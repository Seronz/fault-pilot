package observability_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/seronz/fault-pilot/internal/observability"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestChecker_HealthzOK(t *testing.T) {
	c := observability.NewChecker()
	c.AddCheck("ok", func(context.Context) error { return nil })

	rr := httptest.NewRecorder()
	c.Healthz().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestChecker_HealthzDegraded(t *testing.T) {
	c := observability.NewChecker()
	c.AddCheck("bad", func(context.Context) error { return errors.New("boom") })

	rr := httptest.NewRecorder()
	c.Healthz().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}

func TestChecker_Readyz(t *testing.T) {
	c := observability.NewChecker()

	rr := httptest.NewRecorder()
	c.Readyz().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("not ready status = %d, want 503", rr.Code)
	}

	c.SetReady(true)
	rr = httptest.NewRecorder()
	c.Readyz().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("ready status = %d, want 200", rr.Code)
	}
}

func TestHeartbeat_Probe(t *testing.T) {
	var h observability.Heartbeat
	probe := h.Probe(time.Minute)

	if err := probe(context.Background()); err == nil {
		t.Fatal("expected error before first mark")
	}
	h.Mark()
	if err := probe(context.Background()); err != nil {
		t.Fatalf("unexpected error after mark: %v", err)
	}
}

func TestSelfMonitor_QueueDegraded(t *testing.T) {
	m := observability.NewMetrics()
	sm := observability.NewSelfMonitor(discardLogger(), m, func() int { return 100 })

	degraded, reasons := sm.Sample(time.Now())
	if !degraded {
		t.Fatal("expected degraded due to queue depth")
	}
	if len(reasons) == 0 {
		t.Fatal("expected reasons")
	}
}

func TestSelfMonitor_ErrorRateDegraded(t *testing.T) {
	m := observability.NewMetrics()
	sm := observability.NewSelfMonitor(discardLogger(), m, func() int { return 0 },
		observability.WithErrorRateThreshold(0.5))

	t0 := time.Now()
	sm.Sample(t0) // baseline

	m.RecordAction("kubectl", "success")
	m.RecordAction("kubectl", "failed")
	m.RecordAction("kubectl", "failed")

	degraded, reasons := sm.Sample(t0.Add(time.Second))
	if !degraded {
		t.Fatalf("expected degraded due to error rate, reasons=%v", reasons)
	}
}
