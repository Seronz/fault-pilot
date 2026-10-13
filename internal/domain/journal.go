package domain

import "time"

// JournalState adalah status siklus hidup sebuah aksi.
//
// Alur normal: pending -> executing -> succeeded/failed.
// Bila proses mati saat aksi berjalan, state executing dipulihkan menjadi
// unknown (hasilnya tidak diketahui) agar tidak dieksekusi dua kali.
type JournalState string

const (
	JournalPending   JournalState = "pending"
	JournalExecuting JournalState = "executing"
	JournalSucceeded JournalState = "succeeded"
	JournalFailed    JournalState = "failed"
	JournalUnknown   JournalState = "unknown"
)

// JournalEntry adalah satu baris jurnal aksi. Setiap perubahan state ditulis
// ke SQLite dan ke audit trail JSONL.
type JournalEntry struct {
	ActionID       string       `json:"action_id"`       // UUID aksi
	IdempotencyKey string       `json:"idempotency_key"` // kunci anti-eksekusi ganda
	IncidentID     string       `json:"incident_id,omitempty"`
	Action         string       `json:"action,omitempty"`
	Target         string       `json:"target,omitempty"`
	State          JournalState `json:"state"`
	Message        string       `json:"message,omitempty"`
	UpdatedAt      time.Time    `json:"updated_at"`
}
