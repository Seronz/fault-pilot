package gemini_test

import (
	"context"
	"testing"

	"github.com/seronz/fault-pilot/internal/domain"
	"github.com/seronz/fault-pilot/internal/gemini"
)

func TestFakeAdapterReturnsFixedValidProposal(t *testing.T) {
	inc := domain.Incident{ID: "inc-1", Status: "open", EvidenceIDs: []string{"ev-1", "ev-2"}}

	p, err := gemini.FakeAdapter{}.Propose(context.Background(), inc)
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if p.IncidentID != "inc-1" {
		t.Fatalf("incident id: got %q", p.IncidentID)
	}
	if p.Action != "restart_simulated_service" {
		t.Fatalf("action: got %q", p.Action)
	}
	if p.Target != "checkout" {
		t.Fatalf("target: got %q", p.Target)
	}
	if len(p.EvidenceIDs) != 1 || p.EvidenceIDs[0] != "ev-1" {
		t.Fatalf("evidence: got %v", p.EvidenceIDs)
	}
}
