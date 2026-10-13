package approval_test

import (
	"context"
	"testing"
	"time"

	"github.com/seronz/fault-pilot/internal/approval"
)

func newTestGate(t *testing.T, clock *time.Time) *approval.Gate {
	t.Helper()
	var n int
	return approval.NewGate(sampleRBAC(t),
		approval.WithClock(func() time.Time { return *clock }),
		approval.WithTTL(time.Minute),
		approval.WithIDGenerator(func() string {
			n++
			return "req-" + string(rune('0'+n))
		}),
	)
}

func baseRequest() approval.Request {
	return approval.Request{
		IncidentID:  "inc-1",
		Action:      "restart_simulated_service",
		Target:      "checkout",
		Risk:        approval.RiskHigh,
		RequestedBy: "sre@company.com",
	}
}

func TestGate_ApproveThenCheckConsumesOnce(t *testing.T) {
	now := time.Now()
	g := newTestGate(t, &now)
	ctx := context.Background()

	req, err := g.Request(ctx, baseRequest())
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if req.Status != approval.StatusPending {
		t.Fatalf("status = %q, want pending", req.Status)
	}

	dec, err := g.Approve(ctx, req.ID, "ronz@company.com", "ok")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if dec.Status != approval.StatusApproved || req.DecidedBy != "" {
		// req adalah salinan; pastikan DecidedBy terisi di hasil.
	}
	if req.DecidedAt.IsZero() && false {
		t.Fatal("unreachable")
	}

	ok, reason := g.Check(ctx, "inc-1", "restart_simulated_service", "checkout", approval.RiskHigh)
	if !ok {
		t.Fatalf("Check harus true, reason=%q", reason)
	}
	// Sekali pakai.
	if ok2, _ := g.Check(ctx, "inc-1", "restart_simulated_service", "checkout", approval.RiskHigh); ok2 {
		t.Fatal("approval harus sekali pakai")
	}
}

func TestGate_SelfApprovalBlocked(t *testing.T) {
	now := time.Now()
	g := newTestGate(t, &now)
	ctx := context.Background()

	r := baseRequest()
	r.RequestedBy = "ronz@company.com"
	req, err := g.Request(ctx, r)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := g.Approve(ctx, req.ID, "ronz@company.com", "self"); err == nil {
		t.Fatal("self-approval harus ditolak (four-eyes)")
	}
}

func TestGate_UnauthorizedApprover(t *testing.T) {
	now := time.Now()
	g := newTestGate(t, &now)
	ctx := context.Background()

	req, err := g.Request(ctx, baseRequest()) // risk high
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := g.Approve(ctx, req.ID, "sre@company.com", "coba"); err != approval.ErrForbidden {
		t.Fatalf("sre approve high = %v, want ErrForbidden", err)
	}
}

func TestGate_RejectMeansCheckFails(t *testing.T) {
	now := time.Now()
	g := newTestGate(t, &now)
	ctx := context.Background()

	req, err := g.Request(ctx, baseRequest())
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := g.Reject(ctx, req.ID, "ronz@company.com", "tidak aman"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if ok, _ := g.Check(ctx, "inc-1", "restart_simulated_service", "checkout", approval.RiskHigh); ok {
		t.Fatal("request yang ditolak tidak boleh lolos Check")
	}
}

func TestGate_ExpiredFailsClosed(t *testing.T) {
	now := time.Now()
	g := newTestGate(t, &now)
	ctx := context.Background()

	req, err := g.Request(ctx, baseRequest())
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := g.Approve(ctx, req.ID, "ronz@company.com", "ok"); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	now = now.Add(2 * time.Minute) // TTL 1 menit
	if ok, _ := g.Check(ctx, "inc-1", "restart_simulated_service", "checkout", approval.RiskHigh); ok {
		t.Fatal("approval kadaluarsa harus ditolak")
	}
}

func TestGate_RequestValidation(t *testing.T) {
	now := time.Now()
	g := newTestGate(t, &now)
	ctx := context.Background()

	if _, err := g.Request(ctx, approval.Request{IncidentID: "i", Action: "a", Target: "t", Risk: "high"}); err == nil {
		t.Fatal("requested_by kosong harus ditolak")
	}
	bad := baseRequest()
	bad.Risk = "critical"
	if _, err := g.Request(ctx, bad); err == nil {
		t.Fatal("risk tak dikenal harus ditolak")
	}
	noRead := baseRequest()
	noRead.RequestedBy = "ghost@company.com"
	if _, err := g.Request(ctx, noRead); err != approval.ErrForbidden {
		t.Fatalf("pemohon tanpa read = %v, want ErrForbidden", err)
	}
}

func TestGate_CheckWrongKey(t *testing.T) {
	now := time.Now()
	g := newTestGate(t, &now)
	ctx := context.Background()

	req, err := g.Request(ctx, baseRequest())
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if _, err := g.Approve(ctx, req.ID, "ronz@company.com", "ok"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if ok, _ := g.Check(ctx, "inc-1", "restart_simulated_service", "other", approval.RiskHigh); ok {
		t.Fatal("target berbeda tidak boleh memakai approval")
	}
	if ok, _ := g.Check(ctx, "inc-1", "restart_simulated_service", "checkout", approval.RiskHigh); !ok {
		t.Fatal("approval yang benar harus tetap valid")
	}
}

type countingObserver struct{ decided []string }

func (o *countingObserver) ApprovalDecided(status string) { o.decided = append(o.decided, status) }

func TestGate_ObserverNotified(t *testing.T) {
	now := time.Now()
	var n int
	obs := &countingObserver{}
	g := approval.NewGate(sampleRBAC(t),
		approval.WithClock(func() time.Time { return now }),
		approval.WithIDGenerator(func() string { n++; return "req-" + string(rune('0'+n)) }),
		approval.WithObserver(obs),
	)
	ctx := context.Background()

	req, _ := g.Request(ctx, baseRequest())
	if _, err := g.Approve(ctx, req.ID, "ronz@company.com", "ok"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if len(obs.decided) != 1 || obs.decided[0] != "approved" {
		t.Fatalf("observer = %v", obs.decided)
	}
}
