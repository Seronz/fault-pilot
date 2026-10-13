package gemini

import (
	"context"

	"github.com/seronz/fault-pilot/internal/domain"
)

// FakeAdapter adalah Proposer palsu yang selalu mengembalikan proposal valid
// tetap (aksi restart_simulated_service pada target checkout). Dipakai untuk
// pengujian alur policy/eksekusi tanpa memanggil LLM.
type FakeAdapter struct{}

// Propose mengembalikan proposal valid dengan bukti pertama dari insiden.
func (FakeAdapter) Propose(ctx context.Context, inc domain.Incident) (domain.Proposal, error) {
	var evidence string
	if len(inc.EvidenceIDs) > 0 {
		evidence = inc.EvidenceIDs[0]
	}
	return domain.Proposal{
		IncidentID:  inc.ID,
		EvidenceIDs: []string{evidence},
		Action:      "restart_simulated_service",
		Target:      "checkout",
		Reason:      "fake adapter: fixed valid proposal",
	}, nil
}
