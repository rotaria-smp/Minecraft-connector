package tcpbridge

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"limpan/rotaria-bot/internals/utils"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Wire protocol (simple, text-based):
//   Heartbeat:  client -> "PING\n", server -> "PONG\n"
//   Command:    client -> "CMD <id> <len>\n<payload>"
//               server -> "RES <id> <len>\n<body>"   (or "ERR <id> <len>\n<msg>")
// You can adapt encode/decodeCommand to your existing mod protocol if needed.

var (
	ErrUnavailable = errors.New("tcpbridge: connection unavailable")
	ErrTimeout     = errors.New("tcpbridge: timeout waiting for response")
	ErrBreakerOpen = errors.New("tcpbridge: circuit breaker open")
	ErrClosed      = errors.New("tcpbridge: client closed")
	ErrBadFrame    = errors.New("tcpbridge: bad frame")
)

// Options controls client behavior. Reasonable defaults are applied in New().
type Options struct {
	DialTimeout         time.Duration // default 3s
	ReadTimeout         time.Duration // per read; default 10s
	WriteTimeout        time.Duration // per write; default 5s
	HeartbeatInterval   time.Duration // default 5s
	HeartbeatTimeout    time.Duration // expect PONG within; default 2s
	ReconnectMaxBackoff time.Duration // default 30s
	CommandTimeout      time.Duration // default 10s

	// Circuit breaker (simple built-in)
	BreakerFailures int           // failures before open; default 3
	BreakerOpenFor  time.Duration // how long to stay open; default 10s
}

func (o *Options) setDefaults() {
	if o.DialTimeout == 0 {
		o.DialTimeout = 3 * time.Second
	}
	if o.ReadTimeout == 0 {
		o.ReadTimeout = 10 * time.Second
	}
	if o.WriteTimeout == 0 {
		o.WriteTimeout = 5 * time.Second
	}
	if o.HeartbeatInterval == 0 {
		o.HeartbeatInterval = 5 * time.Second
	}
	if o.HeartbeatTimeout == 0 {
		o.HeartbeatTimeout = 2 * time.Second
	}
	if o.ReconnectMaxBackoff == 0 {
		o.ReconnectMaxBackoff = 30 * time.Second
	}
	if o.CommandTimeout == 0 {
		o.CommandTimeout = 10 * time.Second
	}
	if o.BreakerFailures == 0 {
		o.BreakerFailures = 3
	}
	if o.BreakerOpenFor == 0 {
		o.BreakerOpenFor = 10 * time.Second
	}
}

// Status is a snapshot for /status commands etc.
type Status struct {
	Connected     bool
	BreakerState  string // closed|open|half-open
	LastHeartbeat time.Time
	QueueLen      int // number of in-flight requests
}

type response struct {
	body []byte
	err  error
}

type Client struct {
	addr string
	opt  Options

	mu   sync.RWMutex
	conn net.Conn
	wq   chan []byte // writes are serialized via this channel

	pendingMu sync.Mutex
	pending   map[string]chan response

	healthy    atomic.Bool
	lastPongNS atomic.Int64

	// simple circuit breaker
	brMu            sync.Mutex
	consecFailures  int
	openUntil       time.Time
	halfOpenProbeIn bool

	closed atomic.Bool
	wg     sync.WaitGroup
}

func New(addr string, opt Options) *Client {
	opt.setDefaults()
	c := &Client{
		addr:    addr,
		opt:     opt,
		wq:      make(chan []byte, 128),
		pending: make(map[string]chan response),
	}
	c.lastPongNS.Store(time.Now().UnixNano())
	return c
}

// Start runs the connect/reconnect loop until ctx is done.
func (c *Client) Start(ctx context.Context) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		backoff := time.Second
		for ctx.Err() == nil && !c.closed.Load() {
			conn, err := net.DialTimeout("tcp", c.addr, c.opt.DialTimeout)
			if err != nil {
				// backoff with jitter
				sleep := backoff + time.Duration(utils.RandUint32()%500)*time.Millisecond
				if backoff < c.opt.ReconnectMaxBackoff {
					backoff *= 2
					if backoff > c.opt.ReconnectMaxBackoff {
						backoff = c.opt.ReconnectMaxBackoff
					}
				}
				timer := time.NewTimer(sleep)
				select {
				case <-timer.C:
				case <-ctx.Done():
					return
				}
				continue
			}
			backoff = time.Second
			c.setConn(conn)
			if err := c.run(ctx, conn); err != nil {
				// connection ended; loop and reconnect
			}
		}
	}()
}

func (c *Client) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	c.mu.Lock()
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.mu.Unlock()
	close(c.wq)
	c.failAllPending(errors.New("connection closed"))
	c.wg.Wait()
	return nil
}

func (c *Client) setConn(conn net.Conn) {
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	c.healthy.Store(true)
	c.resetBreaker()
}

func (c *Client) run(ctx context.Context, conn net.Conn) error {
	c.wg.Add(3)
	errs := make(chan error, 3)

	// writer
	go func() {
		defer c.wg.Done()
		for buf := range c.wq {
			if _, err := c.writeFrame(conn, buf); err != nil {
				errs <- err
				return
			}
		}
	}()

	// reader/demux
	go func() {
		defer c.wg.Done()
		br := bufio.NewReader(conn)
		for {
			// keep read fresh to detect dead peers
			_ = conn.SetReadDeadline(time.Now().Add(c.opt.ReadTimeout))
			line, err := br.ReadString('\n')
			if err != nil {
				errs <- err
				return
			}
			line = strings.TrimSpace(line)
			if line == "PONG" {
				c.lastPongNS.Store(time.Now().UnixNano())
				continue
			}
			parts := strings.Split(line, " ")
			if len(parts) < 3 {
				errs <- ErrBadFrame
				return
			}
			kind, id, nStr := parts[0], parts[1], parts[2]
			var n int
			_, err = fmt.Sscanf(nStr, "%d", &n)
			if err != nil || n < 0 || n > 16<<20 { // 16MB guard
				errs <- ErrBadFrame
				return
			}
			payload := make([]byte, n)
			if _, err := ioReadFullWithDeadline(conn, payload, c.opt.ReadTimeout); err != nil {
				errs <- err
				return
			}
			switch kind {
			case "RES":
				c.complete(id, payload, nil)
			case "ERR":
				c.complete(id, nil, errors.New(string(payload)))
			default:
				errs <- ErrBadFrame
				return
			}
		}
	}()

	// heartbeat monitor
	go func() {
		defer c.wg.Done()
		t := time.NewTicker(c.opt.HeartbeatInterval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				last := time.Unix(0, c.lastPongNS.Load())
				// send PING
				c.enqueue([]byte("PING\n"))
				// wait HeartbeatTimeout for a newer PONG
				tmr := time.NewTimer(c.opt.HeartbeatTimeout)
				select {
				case <-tmr.C:
					if time.Unix(0, c.lastPongNS.Load()).After(last) {
						// ok
						continue
					}
					// miss -> force close; reconnect loop takes over
					_ = conn.Close()
					return
				case <-ctx.Done():
					tmr.Stop()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// wait for any error or ctx
	var err error
	select {
	case err = <-errs:
	case <-ctx.Done():
		err = ctx.Err()
	}

	// tear down
	c.healthy.Store(false)
	_ = conn.Close()
	c.failAllPending(ErrUnavailable)
	return err
}

func (c *Client) writeFrame(conn net.Conn, buf []byte) (int, error) {
	_ = conn.SetWriteDeadline(time.Now().Add(c.opt.WriteTimeout))
	return conn.Write(buf)
}

func (c *Client) enqueue(buf []byte) {
	// best-effort; drop if closed
	select {
	case c.wq <- buf:
	default:
		// backpressure: block to preserve ordering
		c.wq <- buf
	}
}

// Send transmits a command payload and waits for a response body.
// It returns ErrUnavailable immediately if the connection is down or breaker is open.
func (c *Client) Send(ctx context.Context, payload []byte) ([]byte, error) {
	if c.closed.Load() {
		return nil, ErrClosed
	}
	if !c.healthy.Load() {
		c.noteFailure()
		return nil, ErrUnavailable
	}
	if c.isBreakerOpen() {
		return nil, ErrBreakerOpen
	}
	id := utils.NewID()
	respCh := make(chan response, 1)

	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	// frame: CMD <id> <len>\n<payload>
	hdr := fmt.Sprintf("CMD %s %d\n", id, len(payload))
	c.enqueue(append([]byte(hdr), payload...))

	// wait
	tmo := c.opt.CommandTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if d := time.Until(deadline); d < tmo {
			tmo = d
		}
	}
	tmr := time.NewTimer(tmo)
	defer tmr.Stop()
	var res response
	select {
	case res = <-respCh:
	case <-tmr.C:
		c.removePending(id)
		c.noteFailure()
		return nil, ErrTimeout
	case <-ctx.Done():
		c.removePending(id)
		c.noteFailure()
		return nil, ctx.Err()
	}
	if res.err != nil {
		c.noteFailure()
		return nil, res.err
	}
	c.noteSuccess()
	return res.body, nil
}

func (c *Client) complete(id string, body []byte, err error) {
	c.pendingMu.Lock()
	ch, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
	if ok {
		ch <- response{body: body, err: err}
	}
}

func (c *Client) removePending(id string) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
}

func (c *Client) failAllPending(err error) {
	c.pendingMu.Lock()
	for id, ch := range c.pending {
		delete(c.pending, id)
		ch <- response{err: err}
	}
	c.pendingMu.Unlock()
}

// Status returns a snapshot of client health.
func (c *Client) Status() Status {
	st := Status{}
	st.Connected = c.healthy.Load()
	st.LastHeartbeat = time.Unix(0, c.lastPongNS.Load())
	st.QueueLen = len(c.wq) // pending writes
	st.BreakerState = func() string {
		c.brMu.Lock()
		defer c.brMu.Unlock()
		if time.Now().Before(c.openUntil) {
			return "open"
		}
		if c.halfOpenProbeIn {
			return "half-open"
		}
		return "closed"
	}()
	return st
}

// --- simple circuit breaker helpers ---
func (c *Client) noteFailure() {
	c.brMu.Lock()
	defer c.brMu.Unlock()
	c.consecFailures++
	if c.consecFailures >= c.opt.BreakerFailures && time.Now().After(c.openUntil) {
		c.openUntil = time.Now().Add(c.opt.BreakerOpenFor)
		c.halfOpenProbeIn = false
	}
}

func (c *Client) noteSuccess() {
	c.resetBreaker()
}

func (c *Client) resetBreaker() {
	c.brMu.Lock()
	c.consecFailures = 0
	c.openUntil = time.Time{}
	c.halfOpenProbeIn = false
	c.brMu.Unlock()
}

func (c *Client) isBreakerOpen() bool {
	c.brMu.Lock()
	defer c.brMu.Unlock()
	now := time.Now()
	if now.Before(c.openUntil) {
		return true
	}
	// half-open probe: allow a single caller after open window passes; others see open until success/failure
	if !c.halfOpenProbeIn && !c.openUntil.IsZero() && now.After(c.openUntil) && c.consecFailures >= c.opt.BreakerFailures {
		c.halfOpenProbeIn = true
		return false
	}
	if c.halfOpenProbeIn {
		// one probe in flight; other callers still see open
		return true
	}
	return false
}

// --- utils ---

func ioReadFullWithDeadline(conn net.Conn, buf []byte, d time.Duration) (int, error) {
	var n int
	for n < len(buf) {
		_ = conn.SetReadDeadline(time.Now().Add(d))
		m, err := conn.Read(buf[n:])
		if err != nil {
			return n, err
		}
		n += m
	}
	return n, nil
}

// Example usage (pseudo):
//   c := tcpbridge.New("127.0.0.1:25570", tcpbridge.Options{})
//   c.Start(ctx)
//   body, err := c.Send(ctx, []byte("/say hello"))
//   st := c.Status()
