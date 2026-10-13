package collector

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/seronz/fault-pilot/internal/domain"
	"github.com/seronz/fault-pilot/internal/worker"
)

const (
	DefaultBufferSize    = 100
	DefaultMaxConcurrent = 5
)

type Processor func(ctx context.Context, e domain.Event) error

type Collector struct {
	events  chan domain.Event
	pool    *worker.Pool
	process Processor
	log     *slog.Logger

	mu   sync.Mutex
	seen map[string]struct{}
	seq  atomic.Uint64

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
	wg     sync.WaitGroup

	accepted atomic.Uint64
	dropped  atomic.Uint64
}

func New(size, maxConcurrent int, process Processor, logger *slog.Logger) *Collector {
	if size <= 0 {
		size = DefaultBufferSize
	}
	if maxConcurrent <= 0 {
		maxConcurrent = DefaultMaxConcurrent
	}
	if process == nil {
		process = func(context.Context, domain.Event) error { return nil }
	}
	if logger == nil {
		logger = slog.Default()
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &Collector{
		events:  make(chan domain.Event, size),
		pool:    worker.New(maxConcurrent),
		process: process,
		log:     logger,
		seen:    make(map[string]struct{}),
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	c.wg.Add(1)
	go c.run()
	return c
}

func (c *Collector) Push(e domain.Event) bool {
	if e.ID == "" {
		e.ID = fmt.Sprintf("ev-%06d", c.seq.Add(1))
	}
	key := dedupKey(e)

	c.mu.Lock()
	if _, ok := c.seen[key]; ok {
		c.mu.Unlock()
		return false
	}
	c.seen[key] = struct{}{}
	c.mu.Unlock()

	select {
	case <-c.done:
		c.forget(key)
		return false
	default:
	}

	select {
	case c.events <- e:
		c.accepted.Add(1)
		return true
	default:
		c.forget(key)
		c.dropped.Add(1)
		return false
	}
}

func (c *Collector) run() {
	defer c.wg.Done()
	for {
		select {
		case <-c.done:
			return
		case e := <-c.events:
			ev := e
			c.pool.Submit(c.ctx, func() {
				if err := c.process(c.ctx, ev); err != nil {
					c.log.Error("process event failed", "event_id", ev.ID, "err", err)
				}
			})
		}
	}
}

func (c *Collector) Close() error {
	c.once.Do(func() {
		c.cancel()
		close(c.done)
	})
	c.wg.Wait()
	c.pool.Close()
	return nil
}

func (c *Collector) Len() int { return len(c.events) }
func (c *Collector) Cap() int { return cap(c.events) }

func (c *Collector) Accepted() uint64 { return c.accepted.Load() }
func (c *Collector) Dropped() uint64  { return c.dropped.Load() }

func (c *Collector) forget(key string) {
	c.mu.Lock()
	delete(c.seen, key)
	c.mu.Unlock()
}

func dedupKey(e domain.Event) string {
	return e.Type + "\x00" + e.Service + "\x00" + e.Message
}
