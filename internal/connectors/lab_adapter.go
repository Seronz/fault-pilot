package connectors

import (
	"context"
)

// LabFaultInjectorAdapter mengadaptasi lab-faultinjector sebagai sumber insiden.
type LabFaultInjectorAdapter struct{}

func NewLabFaultInjectorAdapter() *LabFaultInjectorAdapter {
	return &LabFaultInjectorAdapter{}
}

func (a *LabFaultInjectorAdapter) Source() string {
	return "lab-faultinjector"
}

func (a *LabFaultInjectorAdapter) Poll(ctx context.Context) ([]Incident, error) {
	return []Incident{}, nil
}
