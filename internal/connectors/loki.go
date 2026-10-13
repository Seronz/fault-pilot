package connectors

import "context"

type LokiAdapter struct{}

func NewLokiAdapter() *LokiAdapter                                  { return &LokiAdapter{} }
func (l *LokiAdapter) Source() string                               { return "loki" }
func (l *LokiAdapter) Poll(ctx context.Context) ([]Incident, error) { return []Incident{}, nil }
