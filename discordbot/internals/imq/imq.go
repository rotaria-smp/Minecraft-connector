package imq

import (
	"context"
	"errors"
	"limpan/rotaria-bot/internals/utils"
	"sync"
	"time"
)

// Message is a simple envelope; extend as needed.
type Message struct {
	ID         string
	Body       []byte
	Attempts   int
	EnqueuedAt time.Time
	Headers    map[string]string
}

// Queue is a minimal in-memory queue with a single FIFO channel.
// It is intentionally simple so you can later swap it for NATS/RabbitMQ/etc.
type Queue struct {
	ch     chan Message
	closed bool
	mu     sync.Mutex
}

func New(capacity int) *Queue {
	if capacity <= 0 {
		capacity = 1024
	}
	return &Queue{ch: make(chan Message, capacity)}
}

var ErrClosed = errors.New("imq: queue closed")

func (q *Queue) Publish(ctx context.Context, m Message) error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return ErrClosed
	}
	q.mu.Unlock()
	if m.ID == "" {
		m.ID = utils.NewID()
	}
	m.EnqueuedAt = time.Now()
	select {
	case q.ch <- m:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Consume returns a receive-only channel that will close when ctx is done or the queue is closed.
func (q *Queue) Consume(ctx context.Context) <-chan Message {
	out := make(chan Message)
	go func() {
		defer close(out)
		for {
			select {
			case m, ok := <-q.ch:
				if !ok {
					return
				}
				select {
				case out <- m:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

func (q *Queue) Close() {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.ch)
	}
	q.mu.Unlock()
}

func (q *Queue) Len() int { return len(q.ch) }

// Worker pulls from Q and calls Handle. On error it retries with backoff, then optionally dead-letters.
type Worker struct {
	Q          *Queue
	Handle     func(context.Context, Message) error
	MaxRetries int                             // default 3
	Backoff    func(attempt int) time.Duration // default: exponential up to 10s
	DLQ        *Queue                          // optional
}

func (w *Worker) setDefaults() {
	if w.MaxRetries == 0 {
		w.MaxRetries = 3
	}
	if w.Backoff == nil {
		w.Backoff = func(attempt int) time.Duration {
			if attempt < 1 {
				attempt = 1
			}
			d := time.Duration(1<<int(min(attempt-1, 4))) * time.Second // 1,2,4,8,16 (cap later)
			if d > 10*time.Second {
				d = 10 * time.Second
			}
			return d
		}
	}
}

func (w *Worker) Start(ctx context.Context) {
	w.setDefaults()
	msgs := w.Q.Consume(ctx)
	go func() {
		for {
			select {
			case m, ok := <-msgs:
				if !ok {
					return
				}
				go w.process(ctx, m)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (w *Worker) process(ctx context.Context, m Message) {
	attempt := m.Attempts
	for {
		cctx, cancel := context.WithCancel(ctx)
		err := w.Handle(cctx, m)
		cancel()
		if err == nil {
			return
		}
		attempt++
		if attempt > w.MaxRetries {
			if w.DLQ != nil {
				_ = w.DLQ.Publish(context.Background(), Message{ID: m.ID, Body: m.Body, Attempts: attempt, Headers: m.Headers})
			}
			return
		}
		// retry after backoff
		t := time.NewTimer(w.Backoff(attempt))
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Example wiring:
//   q := imq.New(0)
//   w := &imq.Worker{Q: q, Handle: func(ctx context.Context, m imq.Message) error {
//       _, err := client.Send(ctx, m.Body)
//       return err
//   }}
//   w.Start(ctx)
//   _ = q.Publish(ctx, imq.Message{Body: []byte("/say hi")})
