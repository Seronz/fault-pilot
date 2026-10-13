package connectors

import (
	"context"
	"encoding/json"
)

// Incident adalah representasi insiden dari berbagai sumber observability.
type Incident struct {
	ID         string          `json:"id"`
	Service    string          `json:"service"`
	Signal     string          `json:"signal"`
	Evidence   string          `json:"evidence"`
	Severity   string          `json:"severity"`
	RawPayload json.RawMessage `json:"raw_payload,omitempty"`
}

// IncidentSource adalah interface untuk sumber insiden (lab/prod).
type IncidentSource interface {
	Source() string // "prometheus" | "loki" | "pagerduty" | "cloudwatch" | "lab-faultinjector"
	Poll(ctx context.Context) ([]Incident, error)
}
