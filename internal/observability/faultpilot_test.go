package observability_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/seronz/fault-pilot/internal/observability"
)

func TestMetrics_Exposition(t *testing.T) {
	m := observability.NewMetrics()
	m.RecordIncident("lab")
	m.RecordAction("kubectl", "success")
	m.RecordAction("kubectl", "failed")
	m.RecordRejection("missing evidence")
	m.ObserveExecutorDuration("kubectl", 500*time.Millisecond)

	var b bytes.Buffer
	m.Registry().WritePrometheus(&b)
	out := b.String()

	for _, want := range []string{
		`faultpilot_incidents_total{source="lab"} 1`,
		`faultpilot_actions_total{executor="kubectl",status="success"} 1`,
		`faultpilot_actions_total{executor="kubectl",status="failed"} 1`,
		`faultpilot_policy_rejections_total{reason="missing evidence"} 1`,
		`faultpilot_executor_duration_seconds_count{executor="kubectl"} 1`,
		`faultpilot_worker_queue_depth 0`,
	} {
		if !bytes.Contains(b.Bytes(), []byte(want)) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestMetrics_ErrorRate(t *testing.T) {
	m := observability.NewMetrics()
	if got := m.ErrorRate(); got != 0 {
		t.Fatalf("empty error rate = %v", got)
	}
	m.RecordAction("kubectl", "success")
	m.RecordAction("kubectl", "success")
	m.RecordAction("kubectl", "failed")
	if got := m.ErrorRate(); got < 0.33 || got > 0.34 {
		t.Fatalf("error rate = %v, want ~0.333", got)
	}
}

func TestMetrics_QueueDepthFunc(t *testing.T) {
	depth := 7
	m := observability.NewMetrics(observability.WithQueueDepthFunc(func() int { return depth }))
	depth = 42

	var b bytes.Buffer
	m.Registry().WritePrometheus(&b)
	if !bytes.Contains(b.Bytes(), []byte("faultpilot_worker_queue_depth 42")) {
		t.Fatalf("expected live queue depth in:\n%s", b.String())
	}
}

func TestMetrics_PolicyRejectionInterface(t *testing.T) {
	m := observability.NewMetrics()
	m.PolicyRejection("no evidence")
	if got := m.PolicyRejectionsTotal.Value(observability.Labels{"reason": "no evidence"}); got != 1 {
		t.Fatalf("rejection value = %v, want 1", got)
	}
}

func TestMetrics_ApprovalInterface(t *testing.T) {
	m := observability.NewMetrics()
	m.ApprovalDecided("approved")
	m.RecordApproval("rejected")
	if got := m.ApprovalsTotal.Value(observability.Labels{"status": "approved"}); got != 1 {
		t.Fatalf("approved = %v, want 1", got)
	}
	if got := m.ApprovalsTotal.Value(observability.Labels{"status": "rejected"}); got != 1 {
		t.Fatalf("rejected = %v, want 1", got)
	}
}
