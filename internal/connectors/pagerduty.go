package connectors

import "context"

type PagerDutyAdapter struct{}
func NewPagerDutyAdapter() *PagerDutyAdapter { return &PagerDutyAdapter{} }
func (p *PagerDutyAdapter) Source() string    { return "pagerduty" }
func (p *PagerDutyAdapter) Poll(ctx context.Context) ([]Incident, error) { return []Incident{}, nil }
