package executor_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/seronz/fault-pilot/internal/executor"
)

// T16: dua insiden pada dependency yang sama tidak melebihi batas konkurensi.
func TestT16_TwoIncidentsSameDependencyConcurrencyLimit(t *testing.T) {
	var mu sync.Mutex
	calls := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	exec := executor.New(
		executor.WithHTTPClient(srv.Client()),
		executor.WithFaultInjectorURL(srv.URL),
		executor.WithDeadline(2*time.Second),
		executor.WithMaxConcurrencyPerDependency("checkout", 1),
	)

	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = exec.Execute(ctx, executor.Action{
				IncidentID: "inc-" + string(rune('0'+i)),
				Type:       "restart_simulated_service",
				Target:     "checkout",
			})
		}(i)
	}
	wg.Wait()
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

func TestLabExecutor_TypeAndRisk(t *testing.T) {
	e := executor.New()
	if e.Type() != "lab" {
		t.Fatalf("expected lab, got %s", e.Type())
	}
	if e.RiskLevel() != "low" {
		t.Fatalf("expected low risk, got %s", e.RiskLevel())
	}
}

func TestLabExecutor_PolicyDeniedDoesNotExecute(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	pol := &fakePolicy{allow: false, reason: "target not allowed"}
	exec := executor.New(
		executor.WithHTTPClient(srv.Client()),
		executor.WithFaultInjectorURL(srv.URL),
		executor.WithLabPolicy(pol),
	)

	_, err := exec.Execute(context.Background(), executor.Action{
		IncidentID: "inc-1", Type: "restart_simulated_service", Target: "checkout",
	})
	if !errors.Is(err, executor.ErrDenied) {
		t.Fatalf("expected ErrDenied, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("expected 0 HTTP calls on deny, got %d", calls)
	}
}
