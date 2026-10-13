// Package gemini berisi adapter untuk menghasilkan Proposal dari sebuah
// insiden (mis. lewat model LLM). Implementasi nyata belum ditambahkan; untuk
// saat ini tersedia FakeAdapter yang mengembalikan proposal valid tetap.
package gemini

import (
	"context"

	"github.com/seronz/fault-pilot/internal/domain"
)

// Proposer menghasilkan usulan (Proposal) dari sebuah insiden.
type Proposer interface {
	Propose(ctx context.Context, inc domain.Incident) (domain.Proposal, error)
}
