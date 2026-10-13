package observability_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/seronz/fault-pilot/internal/observability"
)

func TestCounterVec_Exposition(t *testing.T) {
	reg := observability.NewRegistry()
	c := observability.NewCounterVec("faultpilot_incidents_total", "help text")
	reg.Register(c)

	c.Inc(observability.Labels{"source": "lab"})
	c.Add(observability.Labels{"source": "prometheus"}, 3)

	var b bytes.Buffer
	reg.WritePrometheus(&b)
	out := b.String()

	for _, want := range []string{
		"# HELP faultpilot_incidents_total help text",
		"# TYPE faultpilot_incidents_total counter",
		`faultpilot_incidents_total{source="lab"} 1`,
		`faultpilot_incidents_total{source="prometheus"} 3`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if c.Value(observability.Labels{"source": "lab"}) != 1 {
		t.Fatalf("Value = %v", c.Value(observability.Labels{"source": "lab"}))
	}
}

func TestGaugeFunc_ScrapesDynamically(t *testing.T) {
	depth := 5
	g := observability.NewGaugeFunc("faultpilot_worker_queue_depth", "depth", func() float64 {
		return float64(depth)
	})

	var b bytes.Buffer
	g.Write(&b)
	if !strings.Contains(b.String(), "faultpilot_worker_queue_depth 5") {
		t.Fatalf("got %q", b.String())
	}

	depth = 42
	b.Reset()
	g.Write(&b)
	if !strings.Contains(b.String(), "faultpilot_worker_queue_depth 42") {
		t.Fatalf("expected updated gauge value in:\n%s", b.String())
	}
}

func TestHistogramVec_Exposition(t *testing.T) {
	reg := observability.NewRegistry()
	h := observability.NewHistogramVec("faultpilot_executor_duration_seconds", "dur", []float64{0.01, 0.1})
	reg.Register(h)

	h.Observe(observability.Labels{"executor": "kubectl"}, 0.005) // <=0.005,0.01
	h.Observe(observability.Labels{"executor": "kubectl"}, 0.05)  // <=0.1
	h.Observe(observability.Labels{"executor": "kubectl"}, 5)     // +Inf only

	var b bytes.Buffer
	reg.WritePrometheus(&b)
	out := b.String()

	for _, want := range []string{
		`faultpilot_executor_duration_seconds_bucket{executor="kubectl",le="0.01"} 1`,
		`faultpilot_executor_duration_seconds_bucket{executor="kubectl",le="0.1"} 2`,
		`faultpilot_executor_duration_seconds_bucket{executor="kubectl",le="+Inf"} 3`,
		`faultpilot_executor_duration_seconds_count{executor="kubectl"} 3`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRegistry_HandlerContentType(t *testing.T) {
	reg := observability.NewRegistry()
	reg.Register(observability.NewGauge("faultpilot_worker_queue_depth", "d"))

	rr := httptest.NewRecorder()
	reg.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content-type = %q", ct)
	}
}
