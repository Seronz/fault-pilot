package approval

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// MaxBodyBytes membatasi ukuran body request (fail closed untuk input besar).
const DefaultMaxBodyBytes = 64 << 10 // 64 KiB

// Handler menyajikan REST API untuk Human Approval Gate.
//
// Identitas pemanggil diambil dari header "X-Actor" (biasanya diisi oleh
// reverse-proxy/SSO). Bila kosong, dipakai field actor pada body (bila ada).
type Handler struct {
	gate *Gate
	max  int64
}

// NewHandler membuat Handler untuk Gate.
func NewHandler(g *Gate) *Handler { return &Handler{gate: g, max: 64 << 10} }

// Handler mengembalikan http.Handler dengan seluruh route approval.
func (h *Handler) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /approvals", h.list)
	mux.HandleFunc("POST /approvals", h.create)
	mux.HandleFunc("GET /approvals/{id}", h.get)
	mux.HandleFunc("POST /approvals/{id}/approve", h.approve)
	mux.HandleFunc("POST /approvals/{id}/reject", h.reject)
	return mux
}

type createReq struct {
	Actor      string `json:"actor,omitempty"`
	IncidentID string `json:"incident_id"`
	Action     string `json:"action"`
	Target     string `json:"target"`
	Risk       string `json:"risk"`
	Reason     string `json:"reason,omitempty"`
}

type decisionReq struct {
	Actor string `json:"actor,omitempty"`
	Note  string `json:"note,omitempty"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var body createReq
	if !h.decode(w, r, &body) {
		return
	}
	actor := actor(r, body.Actor)
	req, err := h.gate.Request(r.Context(), Request{
		IncidentID:  body.IncidentID,
		Action:      body.Action,
		Target:      body.Target,
		Risk:        body.Risk,
		RequestedBy: actor,
		Reason:      body.Reason,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, req)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	if !h.gate.rbac.CanRead(actor(r, "")) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	req, ok, err := h.gate.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if !h.gate.rbac.CanRead(actor(r, "")) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var (
		reqs []Request
		err  error
	)
	if r.URL.Query().Get("status") == string(StatusPending) {
		reqs, err = h.gate.Pending(r.Context())
	} else {
		reqs, err = h.gate.List(r.Context())
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	if reqs == nil {
		reqs = []Request{}
	}
	writeJSON(w, http.StatusOK, reqs)
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) { h.decide(w, r, true) }

func (h *Handler) reject(w http.ResponseWriter, r *http.Request) { h.decide(w, r, false) }

func (h *Handler) decide(w http.ResponseWriter, r *http.Request, approve bool) {
	var body decisionReq
	if !h.decode(w, r, &body) {
		return
	}
	actorName := actor(r, body.Actor)
	var (
		req Request
		err error
	)
	if approve {
		req, err = h.gate.Approve(r.Context(), r.PathValue("id"), actorName, body.Note)
	} else {
		req, err = h.gate.Reject(r.Context(), r.PathValue("id"), actorName, body.Note)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, h.max)
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

// actor mengambil identitas dari header X-Actor, fallback ke body.
func actor(r *http.Request, bodyActor string) string {
	if a := r.Header.Get("X-Actor"); a != "" {
		return a
	}
	return bodyActor
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, ErrInvalid):
		status = http.StatusBadRequest
	case err == ErrNotFound:
		status = http.StatusNotFound
	case err == ErrForbidden, err == ErrSelfApprove:
		status = http.StatusForbidden
	case err == ErrNotPending:
		status = http.StatusConflict
	}
	http.Error(w, err.Error(), status)
}
