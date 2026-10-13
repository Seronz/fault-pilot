package faultinjector

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func doControl(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/control", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func doHealth(h http.Handler) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthHealthyByDefault(t *testing.T) {
	s := New(nil)
	rec := doHealth(s.Handler())
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var resp HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "healthy" {
		t.Fatalf("want healthy, got %q", resp.Status)
	}
}

func TestUnhealthyReturns500(t *testing.T) {
	s := New(nil)
	h := s.Handler()
	if rec := doControl(t, h, `{"mode":"unhealthy"}`); rec.Code != http.StatusOK {
		t.Fatalf("control want 200, got %d", rec.Code)
	}
	rec := doHealth(h)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("health want 500, got %d", rec.Code)
	}
}

func TestLatencyModeDelays(t *testing.T) {
	s := New(nil)
	h := s.Handler()
	doControl(t, h, `{"mode":"latency","latency_ms":20}`)
	start := time.Now()
	rec := doHealth(h)
	elapsed := time.Since(start)
	if rec.Code != http.StatusOK {
		t.Fatalf("health want 200, got %d", rec.Code)
	}
	if elapsed < 20*time.Millisecond {
		t.Fatalf("expected >=20ms delay, got %v", elapsed)
	}
}

func TestErrorModeAlwaysFailsAtFullRate(t *testing.T) {
	s := New(nil)
	h := s.Handler()
	doControl(t, h, `{"mode":"error","error_rate":1}`)
	if rec := doHealth(h); rec.Code != http.StatusInternalServerError {
		t.Fatalf("health want 500, got %d", rec.Code)
	}
}

func TestErrorModeAlwaysOKAtZeroRate(t *testing.T) {
	s := New(nil)
	h := s.Handler()
	doControl(t, h, `{"mode":"error","error_rate":0}`)
	if rec := doHealth(h); rec.Code != http.StatusOK {
		t.Fatalf("health want 200, got %d", rec.Code)
	}
}

func TestInvalidModeRejected(t *testing.T) {
	s := New(nil)
	if rec := doControl(t, s.Handler(), `{"mode":"bogus"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestControlWrongMethod(t *testing.T) {
	s := New(nil)
	req := httptest.NewRequest(http.MethodGet, "/control", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}
