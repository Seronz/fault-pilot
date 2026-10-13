// Package investigation merangkum insiden dari bukti (event) yang ada.
package investigation

import (
	"context"
	"fmt"

	"github.com/seronz/fault-pilot/internal/domain"
)

// Investigator menyusun ringkasan insiden dari daftar evidence.
type Investigator struct{}

// New membuat Investigator baru.
func New() *Investigator { return &Investigator{} }

// BuildSummary membangun ringkasan insiden berdasarkan incident dan event.
func (i *Investigator) BuildSummary(_ context.Context, inc domain.Incident, events []domain.Event) string {
	if len(events) == 0 {
		return fmt.Sprintf("Incident %s: tidak ada evidence untuk dirangkum.", inc.ID)
	}
	service := events[0].Service
	return fmt.Sprintf("Incident %s di service %s: %d evidence", inc.ID, service, len(events))
}

// SummaryFromEvidence membangun ringkasan langsung dari daftar event.
func (i *Investigator) SummaryFromEvidence(_ context.Context, incidentID string, events []domain.Event) string {
	if len(events) == 0 {
		return fmt.Sprintf("Incident %s: tidak ada evidence.", incidentID)
	}
	return fmt.Sprintf("Incident %s di service %s: %d evidence", incidentID, events[0].Service, len(events))
}
