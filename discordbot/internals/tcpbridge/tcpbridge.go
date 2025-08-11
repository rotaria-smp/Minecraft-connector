package tcpbridge

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Logger is anything with Printf(string, ...any). stdlib *log.Logger works.
type Logger interface {
	Printf(string, ...any)
}

// Wire protocol (simple, text-based):
//   Heartbeat:  client -> "PING", server -> "PONG"
//   Command:    client -> "CMD <id> <len>\n<payload>"
//               server -> "RES <id> <len>\n<body>"   (or "ERR <id> <len>\n<msg>")
//   Event push: server -> "EVT <topic> <len>\n<body>"   (unsolicited message/broadcast)

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

	// Logging
	Log   Logger // optional; if nil, logging is disabled
	Debug bool   // verbose logs
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

// BreakerState represents the state of the circuit breaker
type BreakerState int

const (
	BreakerClosed BreakerState = iota
	BreakerOpen
	BreakerHalfOpen
)

// Status is a snapshot for /status commands etc.
type Status struct {
	Connected     bool
	BreakerState  BreakerState
	LastHeartbeat time.Time
	QueueLen      int // number of in-flight requests
}

// Event represents an unsolicited message pushed by the server.
type Event struct {
	Topic string
	Body  []byte
}

type Response struct {
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
	pending   map[string]chan Response

	subsMu sync.RWMutex
	subs   map[int64]chan Event
	subSeq atomic.Int64

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
		pending: make(map[string]chan Response),
	}
	c.lastPongNS.Store(time.Now().UnixNano())
	return c
}

func (c *Client) infof(format string, args ...any) {
	if c.opt.Log != nil {
		c.opt.Log.Printf("[tcpbridge] "+format, args...)
	}
}
func (c *Client) debugf(format string, args ...any) {
	if c.opt.Debug && c.opt.Log != nil {
		c.opt.Log.Printf("[tcpbridge] "+format, args...)
	}
}
func (c *Client) warnf(format string, args ...any) {
	if c.opt.Log != nil {
		c.opt.Log.Printf("[tcpbridge][WARN] "+format, args...)
	}
}
func (c *Client) errf(format string, args ...any) {
	if c.opt.Log != nil {
		c.opt.Log.Printf("[tcpbridge][ERROR] "+format, args...)
	}
}

// Start runs the connect/reconnect loop until ctx is done.
func (c *Client) Start(ctx context.Context) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		backoff := time.Second
		for ctx.Err() == nil && !c.closed.Load() {
			c.infof("dialing %s", c.addr)
			conn, err := net.DialTimeout("tcp", c.addr, c.opt.DialTimeout)
			if err != nil {
				sleep := backoff + time.Duration(randUint32()%500)*time.Millisecond
				c.warnf("dial failed: %v; retrying in %v", err, sleep)
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
			c.infof("connected to %s", c.addr)
			backoff = time.Second
			c.setConn(conn)
			if err := c.run(ctx, conn); err != nil {
				c.warnf("run loop ended: %v (will reconnect)", err)
			}
		}
	}()
}

func (c *Client) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	c.infof("close requested")
	c.mu.Lock()
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.mu.Unlock()
	close(c.wq)
	c.failAllPending(errors.New("connection closed"))
	c.wg.Wait()
	c.infof("closed")
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
			// cheap header peek for logs
			if len(buf) >= 3 {
				h := string(buf[:3])
				switch h {
				case "PIN":
					c.debugf("sent PING")
				case "CMD":
					// extract id/len from header up to newline
					if nl := strings.IndexByte(string(buf), '\n'); nl > 0 {
						c.debugf("sent %s", strings.TrimSpace(string(buf[:nl])))
					}
				default:
					// ignore
				}
			}
		}
	}()

	go func() {
		defer c.wg.Done()
		br := bufio.NewReader(conn)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(c.opt.ReadTimeout))
			line, err := br.ReadString('\n')
			if err != nil {
				errs <- err
				return
			}
			line = strings.TrimSpace(line)
			if line == "PONG" {
				c.debugf("recv PONG")
				c.lastPongNS.Store(time.Now().UnixNano())
				continue
			}
			parts := strings.Split(line, " ")
			if len(parts) < 3 {
				errs <- ErrBadFrame
				return
			}
			kind, a, nStr := parts[0], parts[1], parts[2]
			var n int
			_, err = fmt.Sscanf(nStr, "%d", &n)
			if err != nil || n < 0 || n > 16<<20 {
				errs <- ErrBadFrame
				return
			}
			c.debugf("recv header: %s %s %d", kind, a, n)

			payload := make([]byte, n)
			_ = conn.SetReadDeadline(time.Now().Add(c.opt.ReadTimeout))
			// IMPORTANT: read from *br*, not conn
			if _, err := io.ReadFull(br, payload); err != nil {
				errs <- err
				return
			}

			switch kind {
			case "RES":
				c.complete(a, payload, nil)
			case "ERR":
				c.complete(a, nil, errors.New(string(payload)))
			case "EVT":
				c.broadcast(Event{Topic: a, Body: payload})
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
				c.debugf("hb: sending PING")
				c.enqueue([]byte("PING\n"))
				tmr := time.NewTimer(c.opt.HeartbeatTimeout)
				select {
				case <-tmr.C:
					if time.Unix(0, c.lastPongNS.Load()).After(last) {
						continue
					}
					c.warnf("hb: missed PONG within %v, closing conn to trigger reconnect", c.opt.HeartbeatTimeout)
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
	c.infof("connection down: %v", err)
	return err
}

func (c *Client) writeFrame(conn net.Conn, buf []byte) (int, error) {
	_ = conn.SetWriteDeadline(time.Now().Add(c.opt.WriteTimeout))
	return conn.Write(buf)
}

func (c *Client) enqueue(buf []byte) {
	select {
	case c.wq <- buf:
	default:
		// backpressure: block to preserve ordering
		c.warnf("writer queue full (%d), blocking", len(c.wq))
		c.wq <- buf
	}
}

// Send transmits a command payload and waits for a response body.
func (c *Client) Send(ctx context.Context, payload []byte) ([]byte, error) {
	if c.closed.Load() {
		c.warnf("Send: client closed")
		return nil, ErrClosed
	}
	if !c.healthy.Load() {
		c.warnf("Send: unhealthy -> ErrUnavailable")
		c.noteFailure()
		return nil, ErrUnavailable
	}
	if c.isBreakerOpen() {
		c.warnf("Send: breaker open")
		return nil, ErrBreakerOpen
	}
	id := newID()
	respCh := make(chan Response, 1)

	c.pendingMu.Lock()
	c.pending[id] = respCh
	pendingCount := len(c.pending)
	c.pendingMu.Unlock()

	hdr := fmt.Sprintf("CMD %s %d\n", id, len(payload))
	c.debugf("Send: enqueue %s (pending=%d)", strings.TrimSpace(hdr), pendingCount)
	c.enqueue(append([]byte(hdr), payload...))

	tmo := c.opt.CommandTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if d := time.Until(deadline); d < tmo {
			tmo = d
		}
	}
	tmr := time.NewTimer(tmo)
	defer tmr.Stop()
	var res Response
	select {
	case res = <-respCh:
	case <-tmr.C:
		c.removePending(id)
		c.noteFailure()
		c.warnf("Send: timeout after %v (id=%s)", tmo, id)
		return nil, ErrTimeout
	case <-ctx.Done():
		c.removePending(id)
		c.noteFailure()
		c.warnf("Send: ctx done: %v (id=%s)", ctx.Err(), id)
		return nil, ctx.Err()
	}
	if res.err != nil {
		c.noteFailure()
		c.warnf("Send: ERR (id=%s): %v", id, res.err)
		return nil, res.err
	}
	c.debugf("Send: RES (id=%s, len=%d)", id, len(res.body))
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
		ch <- Response{body: body, err: err}
	} else {
		c.debugf("complete: unknown id %s (late frame?)", id)
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
		ch <- Response{err: err}
	}
	n := len(c.pending)
	c.pendingMu.Unlock()
	c.warnf("failAllPending: flushed pending (remaining=%d)", n)
}

// --- subscriptions for unsolicited events ---
func (c *Client) Subscribe(buffer int) (id int64, ch <-chan Event, cancel func()) {
	if buffer <= 0 {
		buffer = 64
	}
	cid := c.subSeq.Add(1)
	eventCh := make(chan Event, buffer)

	c.subsMu.Lock()
	if c.subs == nil {
		c.subs = make(map[int64]chan Event)
	}
	c.subs[cid] = eventCh
	c.subsMu.Unlock()

	c.infof("subscribe: id=%d buffer=%d", cid, buffer)

	cancel = func() {
		c.subsMu.Lock()
		if ch, ok := c.subs[cid]; ok {
			delete(c.subs, cid)
			close(ch)
		}
		c.subsMu.Unlock()
		c.infof("unsubscribe: id=%d", cid)
	}
	return cid, eventCh, cancel
}

func (c *Client) broadcast(evt Event) {
	c.subsMu.RLock()
	for id, ch := range c.subs {
		select {
		case ch <- evt:
		default:
			c.debugf("broadcast: drop to slow sub id=%d topic=%s", id, evt.Topic)
		}
	}
	c.subsMu.RUnlock()
}

// Status returns a snapshot of client health.
func (c *Client) Status() Status {
	st := Status{}
	st.Connected = c.healthy.Load()
	st.LastHeartbeat = time.Unix(0, c.lastPongNS.Load())
	st.QueueLen = len(c.wq)
	st.BreakerState = func() BreakerState {
		c.brMu.Lock()
		defer c.brMu.Unlock()
		if time.Now().Before(c.openUntil) {
			return BreakerOpen
		}
		if c.halfOpenProbeIn {
			return BreakerHalfOpen
		}
		return BreakerClosed
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
		if c.opt.Log != nil {
			c.warnf("breaker: OPEN for %v (failures=%d)", c.opt.BreakerOpenFor, c.consecFailures)
		}
	}
}

func (c *Client) noteSuccess() {
	prev := c.Status().BreakerState
	c.resetBreaker()
	if prev != BreakerClosed {
		c.infof("breaker: CLOSED")
	}
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
		c.infof("breaker: HALF-OPEN (probe)")
		return false
	}
	if c.halfOpenProbeIn {
		return true
	}
	return false
}

// --- utils ---
func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func randUint32() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

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
