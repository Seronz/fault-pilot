package verifier_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/seronz/fault-pilot/internal/verifier"
)

func TestT14_NotRecoveredNoInfiniteRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	v := verifier.New(
		verifier.WithHTTPClient(srv.Client()),
		verifier.WithHealthURL(srv.URL),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	res, err := v.CheckHealth(ctx)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res != verifier.ResultNotRecovered {
		t.Fatalf("expected not_recovered, got %s", res)
	}
	// No retry logic assumed; just verify state is not recovered
}

func TestVerifierBeforeAfter(t *testing.T) {
	v := verifier.New()
	// not recovered remains not recovered
	if got := v.Verify(context.Background(), verifier.ResultNotRecovered, verifier.ResultNotRecovered); got != verifier.ResultNotRecovered {
		t.Fatalf("got %s", got)
	}
	if got := v.Verify(context.Background(), verifier.ResultUnknown, verifier.ResultRecovered); got != verifier.ResultRecovered {
		t.Fatalf("got %s", got)
	}
}
