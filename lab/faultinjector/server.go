// Package faultinjector adalah layanan lab yang meniru (mensimulasikan)
// gangguan pada sebuah service sehingga sistem Fault Pilot bisa diuji.
//
// Endpoint HTTP:
//   - GET  /health  -> mengembalikan {"status","latency_ms"} dengan kode 200/500
//     sesuai mode yang sedang aktif.
//   - POST /control -> mengubah mode gangguan: healthy|latency|error|unhealthy.
//
// Catatan: state dilindungi RWMutex sehingga aman diakses dari banyak request.
package faultinjector

import (
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"
)

// Mode adalah mode gangguan yang bisa disimulasikan.
type Mode string

const (
	ModeHealthy   Mode = "healthy"   // normal: selalu 200, tanpa latensi/error
	ModeLatency   Mode = "latency"   // menambah latensi sebelum merespons
	ModeError     Mode = "error"     // mengembalikan error secara acak sesuai error_rate
	ModeUnhealthy Mode = "unhealthy" // selalu mengembalikan 500
)

// State adalah konfigurasi gangguan yang sedang aktif.
type State struct {
	Mode      Mode    `json:"mode"`       // mode aktif
	LatencyMS int     `json:"latency_ms"` // latensi tambahan dalam milidetik
	ErrorRate float64 `json:"error_rate"` // probabilitas error (0..1) untuk mode error
}

// ControlRequest adalah payload untuk endpoint POST /control.
type ControlRequest struct {
	Mode      Mode    `json:"mode"`
	LatencyMS int     `json:"latency_ms"`
	ErrorRate float64 `json:"error_rate"`
}

// HealthResponse adalah bentuk respons endpoint GET /health.
type HealthResponse struct {
	Status    string `json:"status"`     // label status, mis. "healthy"/"unhealthy"
	LatencyMS int    `json:"latency_ms"` // latensi yang disimulasikan
}

// Server membungkus state gangguan dan handler HTTP-nya.
type Server struct {
	mu    sync.RWMutex // melindungi state agar aman untuk akses bersamaan
	state State
	log   *slog.Logger
}

// New membuat Server dengan mode awal healthy.
// Bila logger nil, slog.Default() dipakai.
func New(logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		state: State{Mode: ModeHealthy},
		log:   logger,
	}
}

// State mengembalikan salinan state saat ini (read lock).
func (s *Server) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// setState mengganti state saat ini (write lock).
func (s *Server) setState(st State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = st
}

// Handler mengembalikan http.Handler yang memasang semua route.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/control", s.handleControl)
	return mux
}

// validMode memeriksa apakah m termasuk mode yang dikenal.
func validMode(m Mode) bool {
	switch m {
	case ModeHealthy, ModeLatency, ModeError, ModeUnhealthy:
		return true
	default:
		return false
	}
}

// handleControl menangani POST /control: memvalidasi payload lalu menyimpan
// state baru. Payload tidak valid dibalas 400.
func (s *Server) handleControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.log.Warn("control method not allowed", "method", r.Method)
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Batasi ukuran body (1 MiB) dan tolak field yang tidak dikenal.
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

// handleHealth menangani GET /health. Perilakunya bergantung pada mode:
//   - healthy/latency: 200
//   - unhealthy     : 500
//   - error         : 500 dengan probabilitas error_rate, selain itu 200
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	st := s.State()

	// Simulasikan latensi bila mode latency.
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

// writeJSON menulis v sebagai JSON dengan kode status tertentu.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
