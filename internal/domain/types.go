package domain

import "time"

type Proposal struct {
	IncidentID  string   `json:"incident_id"`
	EvidenceIDs []string `json:"evidence_ids"`
	Action      string   `json:"action"`
	Target      string   `json:"target"`
	Reason      string   `json:"reason"`
}

type Event struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Service   string    `json:"service"`
	Message   string    `json:"message"`
}

type Incident struct {
	ID          string   `json:"id"`
	Status      string   `json:"status"`
	EvidenceIDs []string `json:"evidence_ids"`
}

type ActionResult struct {
	ActionID string `json:"action_id"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}
