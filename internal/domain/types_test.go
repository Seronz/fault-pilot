package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestProposalJSONRoundTrip(t *testing.T) {
	in := Proposal{
		IncidentID:  "inc-1",
		EvidenceIDs: []string{"ev-1", "ev-2"},
		Action:      "restart",
		Target:      "checkout",
		Reason:      "high error rate",
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Proposal
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.IncidentID != in.IncidentID || out.Action != in.Action || len(out.EvidenceIDs) != len(in.EvidenceIDs) {
		t.Fatalf("round trip mismatch: %+v", out)
	}
}

func TestEventJSONRoundTrip(t *testing.T) {
	in := Event{
		ID:        "ev-1",
		Timestamp: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC),
		Type:      "alert",
		Service:   "checkout",
		Message:   "boom",
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Event
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Timestamp.Equal(in.Timestamp) || out.ID != in.ID {
		t.Fatalf("round trip mismatch: %+v", out)
	}
}

func TestIncidentAndActionResultJSONRoundTrip(t *testing.T) {
	inc := Incident{ID: "inc-1", Status: "open", EvidenceIDs: []string{"ev-1"}}
	data, err := json.Marshal(inc)
	if err != nil {
		t.Fatalf("marshal incident: %v", err)
	}
	var gotInc Incident
	if err := json.Unmarshal(data, &gotInc); err != nil {
		t.Fatalf("unmarshal incident: %v", err)
	}
	if gotInc.Status != inc.Status {
		t.Fatalf("incident mismatch: %+v", gotInc)
	}

	res := ActionResult{ActionID: "act-1", Status: "failed", Message: "evaluator unavailable"}
	data, err = json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var gotRes ActionResult
	if err := json.Unmarshal(data, &gotRes); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if gotRes.ActionID != res.ActionID || gotRes.Status != res.Status {
		t.Fatalf("result mismatch: %+v", gotRes)
	}
}
