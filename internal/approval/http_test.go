package approval_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/seronz/fault-pilot/internal/approval"
)

func newHandler(t *testing.T) (*approval.Handler, *approval.Gate) {
	t.Helper()
	g := approval.NewGate(sampleRBAC(t))
	return approval.NewHandler(g), g
}

func do(h http.Handler, method, path, actor, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	}
	if actor != "" {
		r.Header.Set("X-Actor", actor)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

func TestHTTP_CreateAndApprove(t *testing.T) {
	h, g := newHandler(t)
	srv := h.Handler()

	body := `{"incident_id":"inc-1","action":"restart_simulated_service","target":"checkout","risk":"high","reason":"latency"}`
	rr := do(srv, http.MethodPost, "/approvals", "sre@company.com", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rr.Code, rr.Body.String())
	}
	var req approval.Request
	if err := json.Unmarshal(rr.Body.Bytes(), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Status != approval.StatusPending {
		t.Fatalf("status = %q", req.Status)
	}

	rr = do(srv, http.MethodPost, "/approvals/"+req.ID+"/approve", "ronz@company.com", `{"note":"ok"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("approve = %d: %s", rr.Code, rr.Body.String())
	}

	ok, _ := g.Check(context.Background(), "inc-1", "restart_simulated_service", "checkout", "high")
	if !ok {
		t.Fatal("approval seharusnya valid setelah approve")
	}
}

func TestHTTP_CreateForbiddenWithoutActor(t *testing.T) {
	h, _ := newHandler(t)
	body := `{"incident_id":"inc-1","action":"a","target":"checkout","risk":"high"}`
	rr := do(h.Handler(), http.MethodPost, "/approvals", "", body)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
}

func TestHTTP_ApproveUnauthorized(t *testing.T) {
	h, _ := newHandler(t)
	srv := h.Handler()

	body := `{"incident_id":"inc-1","action":"restart_simulated_service","target":"checkout","risk":"high"}`
	rr := do(srv, http.MethodPost, "/approvals", "sre@company.com", body)
	var req approval.Request
	_ = json.Unmarshal(rr.Body.Bytes(), &req)

	rr = do(srv, http.MethodPost, "/approvals/"+req.ID+"/approve", "viewer@company.com", "")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
}

func TestHTTP_RejectAndList(t *testing.T) {
	h, _ := newHandler(t)
	srv := h.Handler()

	body := `{"incident_id":"inc-1","action":"restart_simulated_service","target":"checkout","risk":"high"}`
	rr := do(srv, http.MethodPost, "/approvals", "sre@company.com", body)
	var req approval.Request
	_ = json.Unmarshal(rr.Body.Bytes(), &req)

	if rr := do(srv, http.MethodPost, "/approvals/"+req.ID+"/reject", "ronz@company.com", `{"note":"no"}`); rr.Code != http.StatusOK {
		t.Fatalf("reject = %d", rr.Code)
	}

	rr = do(srv, http.MethodGet, "/approvals", "viewer@company.com", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("list = %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"rejected"`) {
		t.Fatalf("list body = %s", rr.Body.String())
	}

	rr = do(srv, http.MethodGet, "/approvals?status=pending", "viewer@company.com", "")
	if strings.Contains(rr.Body.String(), req.ID) {
		t.Fatalf("request yang sudah rejected tidak boleh muncul di pending: %s", rr.Body.String())
	}
}

func TestHTTP_GetNotFoundAndBadJSON(t *testing.T) {
	h, _ := newHandler(t)
	srv := h.Handler()

	if rr := do(srv, http.MethodGet, "/approvals/missing", "viewer@company.com", ""); rr.Code != http.StatusNotFound {
		t.Fatalf("get missing = %d, want 404", rr.Code)
	}
	rr := do(srv, http.MethodPost, "/approvals", "sre@company.com", `{"unknown":true}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d, want 400", rr.Code)
	}
	rr = do(srv, http.MethodPost, "/approvals", "sre@company.com", `not json`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid body = %d, want 400", rr.Code)
	}
}
