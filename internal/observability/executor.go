package observability

import (
	"context"
	"errors"
	"time"

	"github.com/seronz/fault-pilot/internal/executor"
)

// instrumentedExecutor membungkus Executor untuk merekam metric.
type instrumentedExecutor struct {
	inner executor.Executor
	m     *Metrics
}

// InstrumentExecutor membungkus executor agar setiap eksekusi tercatat pada
// metrics (actions_total + executor_duration_seconds). Bila inner atau m nil,
// inner dikembalikan apa adanya.
func InstrumentExecutor(inner executor.Executor, m *Metrics) executor.Executor {
	if inner == nil || m == nil {
		return inner
	}
	return &instrumentedExecutor{inner: inner, m: m}
}

func (e *instrumentedExecutor) Type() string      { return e.inner.Type() }
func (e *instrumentedExecutor) RiskLevel() string { return e.inner.RiskLevel() }

func (e *instrumentedExecutor) Execute(ctx context.Context, a executor.Action) (executor.Result, error) {
	start := time.Now()
	res, err := e.inner.Execute(ctx, a)
	typ := e.inner.Type()
	e.m.ObserveExecutorDuration(typ, time.Since(start))
	e.m.RecordAction(typ, statusFor(err))
	return res, err
}

// statusFor memetakan error ke status label metric.
func statusFor(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, executor.ErrDenied),
		errors.Is(err, executor.ErrServiceNotAllowed),
		errors.Is(err, executor.ErrRunbookNotAllowed):
		return "rejected"
	default:
		return "failed"
	}
}
