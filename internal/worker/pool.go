package worker

import (
	"context"
	"sync"
)

type Pool struct {
	max    int
	slots  chan struct{}
	wg     sync.WaitGroup
	mu     sync.Mutex
	closed bool
}

func New(max int) *Pool {
	if max <= 0 {
		max = 1
	}
	return &Pool{
		max:   max,
		slots: make(chan struct{}, max),
	}
}

func (p *Pool) Max() int { return p.max }

func (p *Pool) Submit(ctx context.Context, job func()) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if job == nil {
		return false
	}

	select {
	case <-ctx.Done():
		return false
	case p.slots <- struct{}{}:
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		<-p.slots
		return false
	}
	p.wg.Add(1)
	p.mu.Unlock()

	go func() {
		defer p.wg.Done()
		defer func() { <-p.slots }()
		job()
	}()
	return true
}

func (p *Pool) Close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.wg.Wait()
}
