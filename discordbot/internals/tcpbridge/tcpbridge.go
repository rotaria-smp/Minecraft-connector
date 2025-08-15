package tcpbridge

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"limpan/rotaria-bot/entities"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// NDJSON protocol (one JSON object per line):
// {"type":"PING"}
// {"type":"PONG"}
// {"type":"CMD","id":"<id>","body":"<utf8>"}
// {"type":"RES","id":"<id>","body":"<utf8>"}
// {"type":"ERR","id":"<id>","msg":"<utf8>"}
// {"type":"EVT","topic":"<topic>","body":"<utf8>"}

var (
	ErrUnavailable = errors.New("tcpbridge: connection unavailable")
	ErrTimeout     = errors.New("tcpbridge: timeout waiting for response")
	ErrBreakerOpen = errors.New("tcpbridge: circuit breaker open")
	ErrClosed      = errors.New("tcpbridge: client closed")
	ErrBadFrame    = errors.New("tcpbridge: bad frame")
)

type Options struct {
	DialTimeout         time.Duration
	ReadTimeout         time.Duration
	WriteTimeout        time.Duration
	HeartbeatInterval   time.Duration
	HeartbeatTimeout    time.Duration
	ReconnectMaxBackoff time.Duration
	CommandTimeout      time.Duration

	BreakerFailures int
	BreakerOpenFor  time.Duration
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
		o.HeartbeatTimeout = 5 * time.Second
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

type BreakerState int

const (
	BreakerClosed BreakerState = iota
	BreakerOpen
	BreakerHalfOpen
)

type Status struct {
	Connected     bool
	BreakerState  BreakerState
	LastHeartbeat time.Time
	QueueLen      int
}

type Event struct {
	Topic entities.Topic
	Body  []byte
}

type response struct {
	body []byte
	err  error
}

type message struct {
	Type  string         `json:"type"`
	ID    string         `json:"id,omitempty"`
	Body  string         `json:"body,omitempty"`
	Topic entities.Topic `json:"topic,omitempty"`
	Msg   string         `json:"msg,omitempty"`
}

type Client struct {
	addr string
	opt  Options

	mu   sync.RWMutex
	conn net.Conn
	wq   chan []byte

	pendingMu sync.Mutex
	pending   map[string]chan response

	subsMu sync.RWMutex
	subs   map[int64]chan Event
	subSeq atomic.Int64

	healthy    atomic.Bool
	lastPongNS atomic.Int64

	brMu            sync.Mutex
	consecFailures  int
	openUntil       time.Time
	halfOpenProbeIn bool

	closed  atomic.Bool
	started atomic.Bool
	wg      sync.WaitGroup
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

func (c *Client) Start(ctx context.Context) {
	if !c.started.CompareAndSwap(false, true) {
		return // guard against double start
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		backoff := time.Second
		for ctx.Err() == nil && !c.closed.Load() {
			d := &net.Dialer{
				Timeout:   c.opt.DialTimeout,
				KeepAlive: 30 * time.Second, // important: shorter than common NAT idle timeouts
			}
			conn, err := d.DialContext(ctx, "tcp", c.addr)
			if err != nil {
				// dial failed: standard backoff with jitter
				sleepWithJitter(&backoff, c.opt.ReconnectMaxBackoff, ctx)
				continue
			}
			c.setConn(conn)
			uptimeStart := time.Now()
			err = c.run(ctx, conn)

			if err != nil {
				log.Println("tcpbridge: connection error, ", err)
			}

			// If it flapped quickly (dropped connection immediately), back off before the next dial to avoid thrash
			if time.Since(uptimeStart) < time.Second*2 {
				sleepWithJitter(&backoff, c.opt.ReconnectMaxBackoff, ctx)
			} else {
				backoff = time.Second // reset after a stable run
			}
		}
	}()
}

func sleepWithJitter(backoff *time.Duration, max time.Duration, ctx context.Context) {
	sleep := *backoff + time.Duration(randUint32()%500)*time.Millisecond
	if *backoff < max {
		*backoff *= 2
		if *backoff > max {
			*backoff = max
		}
	}
	t := time.NewTimer(sleep)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
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
			if c.opt.ReadTimeout > 0 {
				_ = conn.SetReadDeadline(time.Now().Add(c.opt.ReadTimeout))
			}
			line, err := br.ReadBytes('\n')
			if err != nil {
				// If it's just a timeout, continue waiting for data
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					log.Printf("tcpbridge: read timeout — continuing")
					continue
				}
				errs <- err
				return
			}
			str := strings.TrimSpace(string(line))
			if str == "" {
				continue
			}
			var m message
			if err := json.Unmarshal([]byte(str), &m); err != nil {
				log.Printf("tcpbridge: bad frame (ignored): %q err=%v", str, err)
				continue
			}
			switch m.Type {
			case "PONG":
				c.lastPongNS.Store(time.Now().UnixNano())
			case "RES":
				c.complete(m.ID, []byte(m.Body), nil)
			case "ERR":
				c.complete(m.ID, nil, errors.New(m.Msg))
			case "EVT":
				c.broadcast(Event{Topic: m.Topic, Body: []byte(m.Body)})
			default:
				// ignore unknown
			}
		}
	}()

	// heartbeat monitor (require 2 consecutive misses before closing)
	go func() {
		defer c.wg.Done()
		t := time.NewTicker(c.opt.HeartbeatInterval)
		defer t.Stop()
		misses := 0
		for {
			select {
			case <-t.C:
				last := time.Unix(0, c.lastPongNS.Load())
				c.enqueueJSON(message{Type: "PING"})
				tmr := time.NewTimer(c.opt.HeartbeatTimeout)
				select {
				case <-tmr.C:
					if time.Unix(0, c.lastPongNS.Load()).After(last) {
						misses = 0
						continue
					}
					misses++
					if misses < 2 { // be forgiving; try another round
						continue
					}
					_ = conn.Close() // force reconnect
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

	var err error
	select {
	case err = <-errs:
	case <-ctx.Done():
		err = ctx.Err()
	}

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
	if c.closed.Load() {
		return
	}
	select {
	case c.wq <- buf:
	default:
		c.wq <- buf
	}
}

func (c *Client) enqueueJSON(m message) {
	if c.closed.Load() {
		return
	}
	b, _ := json.Marshal(m)
	b = append(b, '\n')
	select {
	case c.wq <- b:
	default:
		// queue full — drop PINGs; for CMD/critical you might still block
		if m.Type != "PING" {
			c.wq <- b // allow blocking for non-heartbeat frames
		}
	}
}

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

	id := newID()
	respCh := make(chan response, 1)

	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	c.enqueueJSON(message{Type: "CMD", ID: id, Body: string(payload)})

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

func (c *Client) Subscribe(buffer int) (id int64, ch <-chan Event, cancel func()) {
	if buffer <= 0 {
		buffer = 2048 // larger default to tolerate bursts
	}
	cid := c.subSeq.Add(1)
	eventCh := make(chan Event, buffer)

	c.subsMu.Lock()
	if c.subs == nil {
		c.subs = make(map[int64]chan Event)
	}
	c.subs[cid] = eventCh
	c.subsMu.Unlock()

	cancel = func() {
		c.subsMu.Lock()
		if ch, ok := c.subs[cid]; ok {
			delete(c.subs, cid)
			close(ch)
		}
		c.subsMu.Unlock()
	}
	return cid, eventCh, cancel
}

func (c *Client) broadcast(evt Event) {
	c.subsMu.RLock()
	for _, ch := range c.subs {
		select {
		case ch <- evt:
		default:
			log.Printf("tcpbridge: slow subscriber; dropping evt")
		}
	}
	c.subsMu.RUnlock()
}

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

func (c *Client) noteFailure() {
	c.brMu.Lock()
	defer c.brMu.Unlock()
	c.consecFailures++
	if c.consecFailures >= c.opt.BreakerFailures && time.Now().After(c.openUntil) {
		c.openUntil = time.Now().Add(c.opt.BreakerOpenFor)
		c.halfOpenProbeIn = false
	}
}

func (c *Client) noteSuccess() { c.resetBreaker() }

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
	if !c.halfOpenProbeIn && !c.openUntil.IsZero() && now.After(c.openUntil) && c.consecFailures >= c.opt.BreakerFailures {
		c.halfOpenProbeIn = true
		return false
	}
	if c.halfOpenProbeIn {
		return true
	}
	return false
}

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
