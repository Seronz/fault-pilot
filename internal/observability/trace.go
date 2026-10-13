package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
)

// TraceID adalah pengenal unik satu siklus hidup insiden. Nilai ini diteruskan
// lewat context.Context sehingga setiap tahap (collector -> investigator ->
// policy -> executor -> verifier) bisa mencatat log dengan trace_id yang sama.
type TraceID string

type traceCtxKey struct{}

// NewTraceID membangkitkan TraceID acak (16 byte heksadesimal).
func NewTraceID() TraceID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return TraceID("trace-fallback")
	}
	return TraceID(hex.EncodeToString(b[:]))
}

// WithTraceID menyisipkan traceID ke dalam ctx.
func WithTraceID(ctx context.Context, id TraceID) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, traceCtxKey{}, id)
}

// EnsureTraceID mengembalikan ctx dengan TraceID; bila belum ada, dibuat baru.
func EnsureTraceID(ctx context.Context) (context.Context, TraceID) {
	if id, ok := TraceIDFrom(ctx); ok {
		return ctx, id
	}
	id := NewTraceID()
	return WithTraceID(ctx, id), id
}

// TraceIDFrom mengambil TraceID dari ctx.
func TraceIDFrom(ctx context.Context) (TraceID, bool) {
	if ctx == nil {
		return "", false
	}
	id, ok := ctx.Value(traceCtxKey{}).(TraceID)
	return id, ok && id != ""
}

// LoggerWithTrace mengembalikan logger yang otomatis menambahkan trace_id dari ctx.
// Bila ctx tidak punya trace_id, logger dasar dikembalikan apa adanya.
func LoggerWithTrace(ctx context.Context, base *slog.Logger) *slog.Logger {
	if base == nil {
		base = slog.Default()
	}
	if id, ok := TraceIDFrom(ctx); ok {
		return base.With("trace_id", string(id))
	}
	return base
}
