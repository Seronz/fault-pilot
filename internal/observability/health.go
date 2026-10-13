package observability

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Check adalah satu pemeriksaan kesehatan bernama.
type Check struct {
	Name  string
	Probe func(ctx context.Context) error
}

// CheckResult adalah hasil satu pemeriksaan.
type CheckResult struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Checker menjalankan kumpulan pemeriksaan kesehatan (deep check).
type Checker struct {
	mu      sync.Mutex
	checks  []Check
	ready   atomic.Bool
	timeout time.Duration
}

// NewChecker membuat Checker baru (timeout default 2s per pemeriksaan).
func NewChecker() *Checker {
	return &Checker{timeout: 2 * time.Second}
}

// AddCheck mendaftarkan pemeriksaan kesehatan.
func (c *Checker) AddCheck(name string, probe func(ctx context.Context) error) {
	if probe == nil {
		return
	}
	c.mu.Lock()
	c.checks = append(c.checks, Check{Name: name, Probe: probe})
	c.mu.Unlock()
}

// SetReady menandai apakah sistem siap menerima insiden.
func (c *Checker) SetReady(v bool) { c.ready.Store(v) }

// Ready melaporkan status kesiapan.
func (c *Checker) Ready() bool { return c.ready.Load() }

// Run menjalankan seluruh pemeriksaan dan mengembalikan hasilnya.
// Mengembalikan ok=false bila ada satu saja yang gagal.
func (c *Checker) Run(ctx context.Context) (bool, []CheckResult) {
	c.mu.Lock()
	checks := make([]Check, len(c.checks))
	copy(checks, c.checks)
	timeout := c.timeout
	c.mu.Unlock()

	results := make([]CheckResult, 0, len(checks))
	ok := true
	for _, ck := range checks {
		cctx, cancel := context.WithTimeout(ctx, timeout)
		err := ck.Probe(cctx)
		cancel()
		res := CheckResult{Name: ck.Name, OK: err == nil}
		if err != nil {
			res.Error = err.Error()
			ok = false
		}
		results = append(results, res)
	}
	return ok, results
}

type healthzResponse struct {
	Status string        `json:"status"`
	Ready  bool          `json:"ready"`
	Checks []CheckResult `json:"checks,omitempty"`
}

// Healthz adalah handler deep-check (GET /healthz).
//
// Mengembalikan 200 bila semua pemeriksaan lolos, 503 bila ada yang gagal.
func (c *Checker) Healthz() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ok, results := c.Run(r.Context())
		status := http.StatusOK
		label := "ok"
		if !ok {
			status = http.StatusServiceUnavailable
			label = "degraded"
		}
		writeJSON(w, status, healthzResponse{Status: label, Ready: c.Ready(), Checks: results})
	})
}

// Readyz adalah handler kesiapan (GET /readyz).
func (c *Checker) Readyz() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if c.Ready() {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ---------------------------------------------------------------------------
// Heartbeat
// ---------------------------------------------------------------------------

// Heartbeat melacak waktu "denyut" terakhir (mis. polling connector terakhir).
type Heartbeat struct {
	last atomic.Int64 // UnixNano
}

// Mark mencatat waktu sekarang sebagai denyut terakhir.
func (h *Heartbeat) Mark() { h.last.Store(time.Now().UnixNano()) }

// Last mengembalikan waktu denyut terakhir (zero bila belum pernah).
func (h *Heartbeat) Last() time.Time {
	n := h.last.Load()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

// Probe mengembalikan fungsi pemeriksaan: gagal bila belum pernah atau sudah
// lebih lama dari maxAge.
func (h *Heartbeat) Probe(maxAge time.Duration) func(context.Context) error {
	return func(context.Context) error {
		last := h.Last()
		if last.IsZero() {
			return errHeartbeat("belum ada poll")
		}
		if age := time.Since(last); age > maxAge {
			return errHeartbeat("poll terakhir " + age.Round(time.Second).String() + " lalu")
		}
		return nil
	}
}

type errHeartbeat string

func (e errHeartbeat) Error() string { return string(e) }

// ---------------------------------------------------------------------------
// SelfMonitor
// ---------------------------------------------------------------------------

// SelfMonitor memantau kesehatan diri sendiri dan menulis log WARN
// "self-degraded" bila antrean terlalu panjang atau rasio error terlalu tinggi.
type SelfMonitor struct {
	log            *slog.Logger
	metrics        *Metrics
	queueDepth     func() int
	interval       time.Duration
	window         time.Duration
	thresholdQueue int
	thresholdErr   float64

	mu      sync.Mutex
	samples []sample
}

type sample struct {
	at      time.Time
	success float64
	failed  float64
}

// SelfMonitorOption mengonfigurasi SelfMonitor.
type SelfMonitorOption func(*SelfMonitor)

// WithQueueThreshold mengatur ambang antrean (default 90).
func WithQueueThreshold(n int) SelfMonitorOption {
	return func(s *SelfMonitor) {
		if n > 0 {
			s.thresholdQueue = n
		}
	}
}

// WithErrorRateThreshold mengatur ambang rasio error (default 0.5).
func WithErrorRateThreshold(f float64) SelfMonitorOption {
	return func(s *SelfMonitor) {
		if f > 0 {
			s.thresholdErr = f
		}
	}
}

// WithMonitorInterval mengatur interval sampling (default 15s).
func WithMonitorInterval(d time.Duration) SelfMonitorOption {
	return func(s *SelfMonitor) {
		if d > 0 {
			s.interval = d
		}
	}
}

// WithErrorWindow mengatur jendela perhitungan rasio error (default 5m).
func WithErrorWindow(d time.Duration) SelfMonitorOption {
	return func(s *SelfMonitor) {
		if d > 0 {
			s.window = d
		}
	}
}

// NewSelfMonitor membuat SelfMonitor.
func NewSelfMonitor(logger *slog.Logger, metrics *Metrics, queueDepth func() int, opts ...SelfMonitorOption) *SelfMonitor {
	if logger == nil {
		logger = slog.Default()
	}
	s := &SelfMonitor{
		log:            logger,
		metrics:        metrics,
		queueDepth:     queueDepth,
		interval:       15 * time.Second,
		window:         5 * time.Minute,
		thresholdQueue: 90,
		thresholdErr:   0.5,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Sample mencatat snapshot saat ini dan mengembalikan (degraded, alasan).
func (s *SelfMonitor) Sample(now time.Time) (bool, []string) {
	var reasons []string

	if s.queueDepth != nil {
		if depth := s.queueDepth(); depth > s.thresholdQueue {
			reasons = append(reasons, "worker queue depth "+strconv.Itoa(depth)+" > "+strconv.Itoa(s.thresholdQueue))
		}
	}

	if s.metrics != nil {
		success := sumCounter(s.metrics.ActionsTotal, "status", "success")
		failed := sumCounter(s.metrics.ActionsTotal, "status", "failed")

		s.mu.Lock()
		s.samples = append(s.samples, sample{at: now, success: success, failed: failed})
		cutoff := now.Add(-s.window)
		pruned := s.samples[:0]
		for _, sm := range s.samples {
			if !sm.at.Before(cutoff) {
				pruned = append(pruned, sm)
			}
		}
		s.samples = pruned
		oldest := s.samples[0]
		s.mu.Unlock()

		ds := success - oldest.success
		df := failed - oldest.failed
		total := ds + df
		if total > 0 {
			rate := df / total
			if rate > s.thresholdErr {
				reasons = append(reasons, "error rate "+strconv.FormatFloat(rate, 'g', -1, 64)+" > "+strconv.FormatFloat(s.thresholdErr, 'g', -1, 64))
			}
		}
	}

	return len(reasons) > 0, reasons
}

// Run menjalankan sampling berkala sampai ctx selesai.
func (s *SelfMonitor) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if degraded, reasons := s.Sample(now); degraded {
				s.log.Warn("self-degraded", "reasons", reasons)
			}
		}
	}
}
