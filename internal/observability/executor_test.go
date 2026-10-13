package observability_test

import (
	"context"
	"testing"

	"github.com/seronz/fault-pilot/internal/executor"
	"github.com/seronz/fault-pilot/internal/observability"
)

type fakeExec struct {
	typ string
	err error
}

func (f fakeExec) Type() string      { return f.typ }
func (f fakeExec) RiskLevel() string { return "low" }
func (f fakeExec) Execute(context.Context, executor.Action) (executor.Result, error) {
	return executor.Result{Executor: f.typ}, f.err
}

func TestInstrumentExecutor_Statuses(t *testing.T) {
	m := observability.NewMetrics()
	ctx := context.Background()
	a := executor.Action{Type: "x", Target: "y"}

	// success
	_, _ = observability.InstrumentExecutor(fakeExec{typ: "kubectl"}, m).Execute(ctx, a)
	// denied -> rejected
	_, _ = observability.InstrumentExecutor(fakeExec{typ: "kubectl", err: executor.ErrDenied}, m).Execute(ctx, a)
	// other -> failed
	_, _ = observability.InstrumentExecutor(fakeExec{typ: "kubectl", err: executor.ErrExecutionFailed}, m).Execute(ctx, a)

	cases := map[string]float64{
		"success":  1,
		"rejected": 1,
		"failed":   1,
	}
	for status, want := range cases {
		got := m.ActionsTotal.Value(observability.Labels{"executor": "kubectl", "status": status})
		if got != want {
			t.Fatalf("status %s = %v, want %v", status, got, want)
		}
	}
}

func TestInstrumentExecutor_NilPassthrough(t *testing.T) {
	var e executor.Executor = fakeExec{typ: "lab"}
	if got := observability.InstrumentExecutor(e, nil); got != e {
		t.Fatal("expected passthrough when metrics nil")
	}
	if got := observability.InstrumentExecutor(nil, observability.NewMetrics()); got != nil {
		t.Fatal("expected nil passthrough")
	}
}
