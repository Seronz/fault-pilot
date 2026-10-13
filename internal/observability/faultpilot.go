package observability

import "time"

// Metrics adalah himpunan metric Fault Pilot beserta registry-nya.
//
// Nama metric mengikuti gaya Prometheus dan sengaja diberi prefiks
// "faultpilot_" agar mudah dikenali.
type Metrics struct {
	reg *Registry

	// IncidentsTotal menghitung insiden per sumber (source="prometheus|lab").
	IncidentsTotal *CounterVec
	// ActionsTotal menghitung aksi per executor dan status.
	ActionsTotal *CounterVec
	// PolicyRejectionsTotal menghitung penolakan policy per alasan.
	PolicyRejectionsTotal *CounterVec
	// ExecutorDurationSeconds adalah histogram durasi eksekusi per executor.
	ExecutorDurationSeconds *HistogramVec
	// WorkerQueueDepth adalah kedalaman antrean worker (fungsi, dihitung saat scrape).
	WorkerQueueDepth *GaugeFunc
	// EventsDroppedTotal menghitung event yang ditolak karena antrean penuh.
	EventsDroppedTotal *CounterVec
	// ApprovalsTotal menghitung keputusan Human Approval Gate per status.
	ApprovalsTotal *CounterVec
}

// MetricsOption mengonfigurasi Metrics.
type MetricsOption func(*metricsConfig)

type metricsConfig struct {
	queueDepth func() int
}

// WithQueueDepthFunc memasang fungsi yang mengembalikan kedalaman antrean.
func WithQueueDepthFunc(fn func() int) MetricsOption {
	return func(c *metricsConfig) { c.queueDepth = fn }
}

// NewMetrics membuat himpunan metric dan mendaftarkannya ke Registry baru.
func NewMetrics(opts ...MetricsOption) *Metrics {
	cfg := metricsConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	m := &Metrics{
		reg:                     NewRegistry(),
		IncidentsTotal:          NewCounterVec("faultpilot_incidents_total", "Total insiden yang diterima, per sumber."),
		ActionsTotal:            NewCounterVec("faultpilot_actions_total", "Total aksi remediation, per executor dan status."),
		PolicyRejectionsTotal:   NewCounterVec("faultpilot_policy_rejections_total", "Total penolakan policy, per alasan."),
		ExecutorDurationSeconds: NewHistogramVec("faultpilot_executor_duration_seconds", "Durasi eksekusi executor.", DefaultBuckets),
		EventsDroppedTotal:      NewCounterVec("faultpilot_events_dropped_total", "Total event ditolak karena antrean penuh."),
		ApprovalsTotal:          NewCounterVec("faultpilot_approvals_total", "Keputusan Human Approval Gate, per status."),
	}
	qd := 0
	if cfg.queueDepth != nil {
		qd = cfg.queueDepth()
	}
	m.WorkerQueueDepth = NewGaugeFunc("faultpilot_worker_queue_depth", "Kedalaman antrean worker saat ini.", func() float64 {
		if cfg.queueDepth != nil {
			return float64(cfg.queueDepth())
		}
		return float64(qd)
	})

	m.reg.Register(m.IncidentsTotal)
	m.reg.Register(m.ActionsTotal)
	m.reg.Register(m.PolicyRejectionsTotal)
	m.reg.Register(m.ExecutorDurationSeconds)
	m.reg.Register(m.WorkerQueueDepth)
	m.reg.Register(m.EventsDroppedTotal)
	m.reg.Register(m.ApprovalsTotal)
	return m
}

// Registry mengembalikan registry metric.
func (m *Metrics) Registry() *Registry { return m.reg }

// RecordIncident mencatat satu insiden dari sumber tertentu.
func (m *Metrics) RecordIncident(source string) {
	m.IncidentsTotal.Inc(Labels{"source": normSource(source)})
}

// RecordAction mencatat satu aksi dengan status success|failed|rejected.
func (m *Metrics) RecordAction(executor, status string) {
	m.ActionsTotal.Inc(Labels{"executor": executor, "status": status})
}

// RecordRejection mencatat penolakan policy beserta alasannya.
func (m *Metrics) RecordRejection(reason string) {
	m.PolicyRejectionsTotal.Inc(Labels{"reason": reason})
}

// ObserveExecutorDuration mencatat durasi eksekusi executor.
func (m *Metrics) ObserveExecutorDuration(executor string, d time.Duration) {
	m.ExecutorDurationSeconds.Observe(Labels{"executor": executor}, d.Seconds())
}

// RecordEventDropped mencatat event yang ditolak.
func (m *Metrics) RecordEventDropped(source string) {
	m.EventsDroppedTotal.Inc(Labels{"source": normSource(source)})
}

// PolicyRejection memenuhi antarmuka policy.Observer.
func (m *Metrics) PolicyRejection(reason string) { m.RecordRejection(reason) }

// RecordApproval mencatat keputusan approval (approved|rejected|expired).
func (m *Metrics) RecordApproval(status string) {
	m.ApprovalsTotal.Inc(Labels{"status": status})
}

// ApprovalDecided memenuhi antarmuka approval.Observer.
func (m *Metrics) ApprovalDecided(status string) { m.RecordApproval(status) }

// ErrorRate menghitung rasio aksi gagal terhadap total (success+failed).
// Mengembalikan 0 bila belum ada aksi.
func (m *Metrics) ErrorRate() float64 {
	success := sumCounter(m.ActionsTotal, "status", "success")
	failed := sumCounter(m.ActionsTotal, "status", "failed")
	total := success + failed
	if total == 0 {
		return 0
	}
	return failed / total
}

// normSource menormalkan nama sumber agar label stabil.
func normSource(s string) string {
	if s == "" {
		return "lab"
	}
	return s
}

// sumCounter menjumlahkan semua deret CounterVec yang punya label value == val.
func sumCounter(c *CounterVec, label, val string) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	var sum float64
	for key, v := range c.vals {
		if c.labels[key][label] == val {
			sum += v
		}
	}
	return sum
}
