// Package domain berisi tipe data inti (domain model) yang dipakai lintas
// komponen Fault Pilot. Paket ini sengaja tidak memiliki dependensi ke
// infrastruktur (database, HTTP, Kubernetes) agar mudah diuji dan dipakai ulang.
package domain

import "time"

// Proposal adalah usulan tindakan perbaikan yang dihasilkan oleh tahap
// investigasi. Proposal belum tentu dijalankan; ia harus lolos policy/evaluasi
// terlebih dahulu.
type Proposal struct {
	IncidentID  string   `json:"incident_id"`  // ID insiden terkait
	EvidenceIDs []string `json:"evidence_ids"` // daftar ID bukti pendukung
	Action      string   `json:"action"`       // jenis aksi, mis. "restart"
	Target      string   `json:"target"`       // sasaran aksi, mis. nama service
	Reason      string   `json:"reason"`       // alasan usulan aksi
}

// Event adalah satu kejadian/telemetri yang dikumpulkan dari sistem.
type Event struct {
	ID        string    `json:"id"`        // ID unik event, mis. "ev-000001"
	Timestamp time.Time `json:"timestamp"` // waktu kejadian
	Type      string    `json:"type"`      // jenis event, mis. "alert"
	Service   string    `json:"service"`   // service asal event
	Message   string    `json:"message"`   // pesan/deskripsi event
}

// Incident adalah sekumpulan bukti yang dikelompokkan menjadi satu masalah
// yang perlu diselidiki.
type Incident struct {
	ID          string   `json:"id"`           // ID unik insiden
	Status      string   `json:"status"`       // status insiden, mis. "open"
	EvidenceIDs []string `json:"evidence_ids"` // daftar ID event/bukti
}

// ActionResult adalah hasil eksekusi sebuah Proposal.
type ActionResult struct {
	ActionID string `json:"action_id"` // ID aksi yang dieksekusi
	Status   string `json:"status"`    // status hasil, mis. "success"/"failed"
	Message  string `json:"message"`   // keterangan tambahan
}
