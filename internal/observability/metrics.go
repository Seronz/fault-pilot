// Package observability menyediakan metrics (gaya Prometheus, tanpa dependensi
// eksternal), tracing sederhana (TraceID lewat context), serta pemeriksaan
// kesehatan diri sendiri (/healthz, /readyz).
//
// Paket ini bersifat TAMBAHAN: log dan SQLite yang lama tidak diubah.
package observability

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Labels adalah himpunan label (dimensi) sebuah metric.
type Labels map[string]string

// labelKey menghasilkan kunci stabil untuk sebuah himpunan label.
func labelKey(l Labels) string {
	if len(l) == 0 {
		return ""
	}
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('\x00')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(l[k])
	}
	return b.String()
}

// escapeLabel meng-escape nilai label sesuai format teks Prometheus.
func escapeLabel(s string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
	).Replace(s)
}

// writeLabels menulis label dalam format {k="v",...} (urut).
func writeLabels(w io.Writer, l Labels) {
	if len(l) == 0 {
		return
	}
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	io.WriteString(w, "{")
	for i, k := range keys {
		if i > 0 {
			io.WriteString(w, ",")
		}
		io.WriteString(w, k)
		io.WriteString(w, `="`)
		io.WriteString(w, escapeLabel(l[k]))
		io.WriteString(w, `"`)
	}
	io.WriteString(w, "}")
}

// formatFloat memformat angka agar stabil (tanpa notasi ilmiah aneh).
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// Collector adalah metric yang bisa ditulis ke format teks Prometheus.
type Collector interface {
	Name() string
	Help() string
	Type() string
	Write(w io.Writer)
}

// ---------------------------------------------------------------------------
// Counter
// ---------------------------------------------------------------------------

// Counter adalah metric monoton naik.
type Counter struct {
	name, help string
	mu         sync.Mutex
	val        float64
}

// NewCounter membuat Counter tanpa label.
func NewCounter(name, help string) *Counter {
	return &Counter{name: name, help: help}
}

func (c *Counter) Name() string { return c.name }
func (c *Counter) Help() string { return c.help }
func (c *Counter) Type() string { return "counter" }

// Inc menambah 1.
func (c *Counter) Inc() { c.Add(1) }

// Add menambah v.
func (c *Counter) Add(v float64) {
	c.mu.Lock()
	c.val += v
	c.mu.Unlock()
}

// Value mengembalikan nilai saat ini.
func (c *Counter) Value() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.val
}

func (c *Counter) Write(w io.Writer) {
	c.mu.Lock()
	v := c.val
	c.mu.Unlock()
	io.WriteString(w, c.name+" "+formatFloat(v)+"\n")
}

// CounterVec adalah Counter dengan label.
type CounterVec struct {
	name, help string
	mu         sync.Mutex
	vals       map[string]float64
	labels     map[string]Labels
}

// NewCounterVec membuat CounterVec baru.
func NewCounterVec(name, help string) *CounterVec {
	return &CounterVec{name: name, help: help, vals: map[string]float64{}, labels: map[string]Labels{}}
}

func (c *CounterVec) Name() string { return c.name }
func (c *CounterVec) Help() string { return c.help }
func (c *CounterVec) Type() string { return "counter" }

// Inc menambah 1 pada deret label l.
func (c *CounterVec) Inc(l Labels) { c.Add(l, 1) }

// Add menambah v pada deret label l.
func (c *CounterVec) Add(l Labels, v float64) {
	key := labelKey(l)
	c.mu.Lock()
	c.vals[key] += v
	if _, ok := c.labels[key]; !ok {
		c.labels[key] = copyLabels(l)
	}
	c.mu.Unlock()
}

// Value mengembalikan nilai deret label l.
func (c *CounterVec) Value(l Labels) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.vals[labelKey(l)]
}

func (c *CounterVec) Write(w io.Writer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range sortedKeys(c.vals) {
		io.WriteString(w, c.name)
		writeLabels(w, c.labels[key])
		io.WriteString(w, " "+formatFloat(c.vals[key])+"\n")
	}
}

// ---------------------------------------------------------------------------
// Gauge
// ---------------------------------------------------------------------------

// Gauge adalah metric yang bisa naik/turun.
type Gauge struct {
	name, help string
	mu         sync.Mutex
	val        float64
}

// NewGauge membuat Gauge baru.
func NewGauge(name, help string) *Gauge { return &Gauge{name: name, help: help} }

func (g *Gauge) Name() string { return g.name }
func (g *Gauge) Help() string { return g.help }
func (g *Gauge) Type() string { return "gauge" }

// Set menetapkan nilai.
func (g *Gauge) Set(v float64) {
	g.mu.Lock()
	g.val = v
	g.mu.Unlock()
}

// Add menambah v.
func (g *Gauge) Add(v float64) {
	g.mu.Lock()
	g.val += v
	g.mu.Unlock()
}

// Value mengembalikan nilai saat ini.
func (g *Gauge) Value() float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.val
}

func (g *Gauge) Write(w io.Writer) {
	g.mu.Lock()
	v := g.val
	g.mu.Unlock()
	io.WriteString(w, g.name+" "+formatFloat(v)+"\n")
}

// GaugeFunc adalah gauge yang nilainya dihitung saat di-scrape.
type GaugeFunc struct {
	name, help string
	fn         func() float64
}

// NewGaugeFunc membuat gauge berbasis fungsi (nil -> 0).
func NewGaugeFunc(name, help string, fn func() float64) *GaugeFunc {
	if fn == nil {
		fn = func() float64 { return 0 }
	}
	return &GaugeFunc{name: name, help: help, fn: fn}
}

func (g *GaugeFunc) Name() string { return g.name }
func (g *GaugeFunc) Help() string { return g.help }
func (g *GaugeFunc) Type() string { return "gauge" }

func (g *GaugeFunc) Write(w io.Writer) {
	io.WriteString(w, g.name+" "+formatFloat(g.fn())+"\n")
}

// ---------------------------------------------------------------------------
// Histogram
// ---------------------------------------------------------------------------

// DefaultBuckets adalah batas bucket default (detik), sama seperti default Go.
var DefaultBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// HistogramVec adalah histogram dengan label.
type HistogramVec struct {
	name, help string
	buckets    []float64
	mu         sync.Mutex
	series     map[string]*histSeries
	labels     map[string]Labels
}

type histSeries struct {
	counts []uint64 // len = len(buckets)+1, elemen terakhir = +Inf
	sum    float64
	count  uint64
}

// NewHistogramVec membuat HistogramVec. buckets nil -> DefaultBuckets.
func NewHistogramVec(name, help string, buckets []float64) *HistogramVec {
	if len(buckets) == 0 {
		buckets = DefaultBuckets
	}
	b := make([]float64, len(buckets))
	copy(b, buckets)
	sort.Float64s(b)
	return &HistogramVec{name: name, help: help, buckets: b, series: map[string]*histSeries{}, labels: map[string]Labels{}}
}

func (h *HistogramVec) Name() string { return h.name }
func (h *HistogramVec) Help() string { return h.help }
func (h *HistogramVec) Type() string { return "histogram" }

func (h *HistogramVec) seriesFor(l Labels) *histSeries {
	key := labelKey(l)
	s, ok := h.series[key]
	if !ok {
		s = &histSeries{counts: make([]uint64, len(h.buckets)+1)}
		h.series[key] = s
		h.labels[key] = copyLabels(l)
	}
	return s
}

// Observe mencatat satu pengamatan v pada deret label l.
func (h *HistogramVec) Observe(l Labels, v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.seriesFor(l)
	s.sum += v
	s.count++
	// Cari bucket pertama yang batas atasnya >= v; bila tak ada -> +Inf.
	idx := len(h.buckets)
	for i, b := range h.buckets {
		if v <= b {
			idx = i
			break
		}
	}
	s.counts[idx]++
}

func (h *HistogramVec) Write(w io.Writer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	keys := make([]string, 0, len(h.series))
	for k := range h.series {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		s := h.series[key]
		lbl := copyLabels(h.labels[key])
		var cum uint64
		for i, b := range h.buckets {
			cum += s.counts[i]
			io.WriteString(w, h.name+"_bucket")
			writeLabels(w, withLabel(lbl, "le", formatFloat(b)))
			io.WriteString(w, " "+strconv.FormatUint(cum, 10)+"\n")
		}
		io.WriteString(w, h.name+"_bucket")
		writeLabels(w, withLabel(lbl, "le", "+Inf"))
		io.WriteString(w, " "+strconv.FormatUint(s.count, 10)+"\n")

		io.WriteString(w, h.name+"_sum")
		writeLabels(w, lbl)
		io.WriteString(w, " "+formatFloat(s.sum)+"\n")

		io.WriteString(w, h.name+"_count")
		writeLabels(w, lbl)
		io.WriteString(w, " "+strconv.FormatUint(s.count, 10)+"\n")
	}
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

// Registry mengumpulkan banyak Collector dan mengeksposnya.
type Registry struct {
	mu         sync.Mutex
	collectors []Collector
}

// NewRegistry membuat Registry kosong.
func NewRegistry() *Registry { return &Registry{} }

// Register menambahkan collector.
func (r *Registry) Register(c Collector) {
	if c == nil {
		return
	}
	r.mu.Lock()
	r.collectors = append(r.collectors, c)
	r.mu.Unlock()
}

// WritePrometheus menulis seluruh metric dalam format teks Prometheus.
func (r *Registry) WritePrometheus(w io.Writer) {
	r.mu.Lock()
	cs := make([]Collector, len(r.collectors))
	copy(cs, r.collectors)
	r.mu.Unlock()

	for _, c := range cs {
		if c.Help() != "" {
			fmt.Fprintf(w, "# HELP %s %s\n", c.Name(), c.Help())
		}
		fmt.Fprintf(w, "# TYPE %s %s\n", c.Name(), c.Type())
		c.Write(w)
	}
}

// Handler mengembalikan http.Handler untuk GET /metrics.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.WritePrometheus(w)
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func copyLabels(l Labels) Labels {
	if l == nil {
		return nil
	}
	out := make(Labels, len(l))
	for k, v := range l {
		out[k] = v
	}
	return out
}

func withLabel(l Labels, k, v string) Labels {
	out := make(Labels, len(l)+1)
	for key, val := range l {
		out[key] = val
	}
	out[k] = v
	return out
}

func sortedKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
