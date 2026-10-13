package executor_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/seronz/fault-pilot/internal/executor"
)

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
			_ = exec.Execute(ctx, "restart_simulated_service", "checkout", "inc-"+string(rune('0'+i)))
		}(i)
	}
	wg.Wait()
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}
