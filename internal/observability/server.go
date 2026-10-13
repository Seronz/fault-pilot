package observability

import "net/http"

// Server menggabungkan endpoint observability: /metrics, /healthz, /readyz.
type Server struct {
	registry *Registry
	checker  *Checker
}

// NewServer membuat Server observability.
func NewServer(registry *Registry, checker *Checker) *Server {
	return &Server{registry: registry, checker: checker}
}

// Handler mengembalikan http.Handler dengan semua route observability.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	if s.registry != nil {
		mux.Handle("/metrics", s.registry.Handler())
	}
	if s.checker != nil {
		mux.Handle("/healthz", s.checker.Healthz())
		mux.Handle("/readyz", s.checker.Readyz())
	}
	return mux
}
