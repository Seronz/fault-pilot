package connectors

import (
	"context"
)

// PrometheusAdapter sumber insiden dari alert Prometheus.
type PrometheusAdapter struct{}

func NewPrometheusAdapter() *PrometheusAdapter { return &PrometheusAdapter{} }
func (p *PrometheusAdapter) Source() string    { return "prometheus" }
func (p *PrometheusAdapter) Poll(ctx context.Context) ([]Incident, error) {
	return []Incident{}, nil
}
