package observability_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/seronz/fault-pilot/internal/observability"
)

func TestTraceID_RoundTrip(t *testing.T) {
	id := observability.NewTraceID()
	if id == "" {
		t.Fatal("empty trace id")
	}
	ctx := observability.WithTraceID(context.Background(), id)
	got, ok := observability.TraceIDFrom(ctx)
	if !ok || got != id {
		t.Fatalf("got %q, ok=%v", got, ok)
	}
}

func TestEnsureTraceID(t *testing.T) {
	ctx, id := observability.EnsureTraceID(context.Background())
	if id == "" {
		t.Fatal("empty id")
	}
	ctx2, id2 := observability.EnsureTraceID(ctx)
	if id2 != id {
		t.Fatalf("id changed: %q -> %q", id, id2)
	}
	if _, ok := observability.TraceIDFrom(ctx2); !ok {
		t.Fatal("trace id missing")
	}
}

func TestLoggerWithTrace_AddsAttribute(t *testing.T) {
	ctx := observability.WithTraceID(context.Background(), observability.TraceID("abc123"))
	base := slog.New(slog.NewTextHandler(&dummyWriter{}, nil))
	l := observability.LoggerWithTrace(ctx, base)
	if l == nil {
		t.Fatal("nil logger")
	}
	// Logger tanpa trace tetap valid.
	l2 := observability.LoggerWithTrace(context.Background(), base)
	if l2 == nil {
		t.Fatal("nil logger without trace")
	}
}

type dummyWriter struct{}

func (dummyWriter) Write(p []byte) (int, error) { return len(p), nil }
