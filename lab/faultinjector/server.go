package faultinjector

import (
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"
)

type Mode string

const (
	ModeHealthy   Mode = "healthy"
	ModeLatency   Mode = "latency"
	ModeError     Mode = "error"
	ModeUnhealthy Mode = "unhealthy"
)

type State struct {
	Mode      Mode    `json:"mode"`
	LatencyMS int     `json:"latency_ms"`
	ErrorRate float64 `json:"error_rate"`
}

type ControlRequest struct {
	Mode      Mode    `json:"mode"`
	LatencyMS int     `json:"latency_ms"`
	ErrorRate float64 `json:"error_rate"`
}

type HealthResponse struct {
	Status    string `json:"status"`
	LatencyMS int    `json:"latency_ms"`
}

type Server struct {
	mu    sync.RWMutex
	state State
	log   *slog.Logger
}

func New(logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		state: State{Mode: ModeHealthy},
		log:   logger,
	}
}

func (s *Server) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *Server) setState(st State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = st
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/control", s.handleControl)
	return mux
}

func validMode(m Mode) bool {
	switch m {
	case ModeHealthy, ModeLatency, ModeError, ModeUnhealthy:
		return true
	default:
		return false
	}
}

func (s *Server) handleControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.log.Warn("control method not allowed", "method", r.Method)
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ControlRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		s.log.Warn("invalid control payload", "err", err)
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	if !validMode(req.Mode) {
		s.log.Warn("invalid mode", "mode", req.Mode)
		http.Error(w, "invalid mode", http.StatusBadRequest)
		return
	}
	if req.LatencyMS < 0 {
		http.Error(w, "latency_ms must be >= 0", http.StatusBadRequest)
		return
	}
	if req.ErrorRate < 0 || req.ErrorRate > 1 {
		http.Error(w, "error_rate must be between 0 and 1", http.StatusBadRequest)
		return
	}

	st := State{Mode: req.Mode, LatencyMS: req.LatencyMS, ErrorRate: req.ErrorRate}
	s.setState(st)
	s.log.Info("control updated", "mode", st.Mode, "latency_ms", st.LatencyMS, "error_rate", st.ErrorRate)

	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	st := s.State()

	if st.Mode == ModeLatency && st.LatencyMS > 0 {
		time.Sleep(time.Duration(st.LatencyMS) * time.Millisecond)
	}

	status := http.StatusOK
	label := string(ModeHealthy)
	reportedLatency := 0

	switch st.Mode {
	case ModeLatency:
		reportedLatency = st.LatencyMS
	case ModeUnhealthy:
		status = http.StatusInternalServerError
		label = string(ModeUnhealthy)
	case ModeError:
		if rand.Float64() < st.ErrorRate {
			status = http.StatusInternalServerError
			label = string(ModeError)
		}
	}

	s.log.Debug("health", "mode", st.Mode, "status", status)
	writeJSON(w, status, HealthResponse{Status: label, LatencyMS: reportedLatency})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
