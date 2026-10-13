package worker_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/seronz/fault-pilot/internal/worker"
)

func TestPoolLimitsConcurrencyToMax(t *testing.T) {
	const max = 5
	p := worker.New(max)

	var running int32
	var peak int32
	var wg sync.WaitGroup

	const jobs = 60
	wg.Add(jobs)
	for i := 0; i < jobs; i++ {
		if !p.Submit(context.Background(), func() {
			defer wg.Done()
			cur := atomic.AddInt32(&running, 1)
			for {
				old := atomic.LoadInt32(&peak)
				if cur <= old || atomic.CompareAndSwapInt32(&peak, old, cur) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&running, -1)
		}) {
			t.Fatalf("submit %d rejected unexpectedly", i)
		}
	}

	wg.Wait()
	p.Close()

	if peak > max {
		t.Fatalf("observed concurrency %d exceeds max %d", peak, max)
	}
	if peak < 2 {
		t.Fatalf("expected concurrency > 1, observed %d", peak)
	}
}

func TestPoolCloseWaitsForInflight(t *testing.T) {
	p := worker.New(2)
	finished := make(chan struct{})

	if !p.Submit(context.Background(), func() {
		time.Sleep(50 * time.Millisecond)
		close(finished)
	}) {
		t.Fatal("submit rejected")
	}

	p.Close()

	select {
	case <-finished:
	default:
		t.Fatal("Close returned before in-flight job finished")
	}
}

func TestPoolRejectsAfterClose(t *testing.T) {
	p := worker.New(1)
	p.Close()
	if p.Submit(context.Background(), func() {}) {
		t.Fatal("Submit after Close must return false")
	}
}

func TestPoolRejectsWhenContextCancelled(t *testing.T) {
	p := worker.New(1)

	started := make(chan struct{})
	release := make(chan struct{})
	if !p.Submit(context.Background(), func() {
		close(started)
		<-release
	}) {
		t.Fatal("submit rejected")
	}
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if p.Submit(ctx, func() {}) {
		t.Fatal("Submit with cancelled context must return false")
	}

	close(release)
	p.Close()
}
