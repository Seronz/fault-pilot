// Package store menyediakan penyimpanan persisten berbasis SQLite.
//
// Basis data dibuka dengan mode WAL (Write-Ahead Logging) agar pembacaan dan
// penulisan bisa berjalan lebih mulus. Dua tabel utama:
//   - events    : menyimpan event yang diterima collector.
//   - incidents : menyimpan insiden beserta daftar buktinya (JSON).
//
// Catatan: koneksi dibatasi satu (SetMaxOpenConns(1)) untuk menghindari
// error "database is locked" khas SQLite.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/seronz/fault-pilot/internal/domain"
)

// schema berisi DDL untuk membuat tabel bila belum ada.
const schema = `
CREATE TABLE IF NOT EXISTS events (
	id        TEXT PRIMARY KEY,
	timestamp INTEGER NOT NULL,
	type      TEXT NOT NULL,
	service   TEXT NOT NULL,
	message   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS incidents (
	id           TEXT PRIMARY KEY,
	status       TEXT NOT NULL,
	evidence_ids TEXT NOT NULL DEFAULT '[]',
	created_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS journal (
	action_id       TEXT PRIMARY KEY,
	idempotency_key TEXT NOT NULL UNIQUE,
	incident_id     TEXT NOT NULL DEFAULT '',
	action          TEXT NOT NULL DEFAULT '',
	target          TEXT NOT NULL DEFAULT '',
	state           TEXT NOT NULL,
	message         TEXT NOT NULL DEFAULT '',
	updated_at      INTEGER NOT NULL
);
`

// Store adalah pembungkus koneksi SQLite.
type Store struct {
	db *sql.DB
}

// Open membuka (atau membuat) database pada dsn, mengaktifkan WAL, lalu
// menjalankan migrasi skema. Gunakan ":memory:" untuk database sementara.
func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite hanya aman untuk satu penulis; batasi ke satu koneksi.
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout=5000;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set journal_mode: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign_keys: %w", err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// JournalMode mengembalikan mode jurnal SQLite yang sedang aktif
// (diharapkan "wal" untuk database berbasis file).
func (s *Store) JournalMode(ctx context.Context) (string, error) {
	var mode string
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode;").Scan(&mode); err != nil {
		return "", err
	}
	return mode, nil
}

// AppendEvent menyimpan satu event. ID yang sudah ada diabaikan (idempoten).
func (s *Store) AppendEvent(ctx context.Context, e domain.Event) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO events (id, timestamp, type, service, message) VALUES (?, ?, ?, ?, ?)`,
		e.ID, e.Timestamp.UTC().UnixNano(), e.Type, e.Service, e.Message,
	)
	return err
}

// CreateIncident menyimpan satu insiden. Daftar bukti disimpan sebagai JSON.
// ID yang sudah ada diabaikan (idempoten).
func (s *Store) CreateIncident(ctx context.Context, inc domain.Incident) error {
	evidence, err := json.Marshal(inc.EvidenceIDs)
	if err != nil {
		return fmt.Errorf("marshal evidence: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO incidents (id, status, evidence_ids, created_at) VALUES (?, ?, ?, ?)`,
		inc.ID, inc.Status, string(evidence), time.Now().UTC().UnixNano(),
	)
	return err
}

// CountEvents mengembalikan jumlah baris pada tabel events.
func (s *Store) CountEvents(ctx context.Context) (int, error) {
	return s.count(ctx, "events")
}

// CountIncidents mengembalikan jumlah baris pada tabel incidents.
func (s *Store) CountIncidents(ctx context.Context) (int, error) {
	return s.count(ctx, "incidents")
}

// Incident mengambil satu insiden berdasarkan ID. Nilai bool kedua bernilai
// false bila insiden tidak ditemukan.
func (s *Store) Incident(ctx context.Context, id string) (domain.Incident, bool, error) {
	var status, evidenceJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT status, evidence_ids FROM incidents WHERE id = ?`, id,
	).Scan(&status, &evidenceJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Incident{}, false, nil
	}
	if err != nil {
		return domain.Incident{}, false, err
	}

	var evidence []string
	if err := json.Unmarshal([]byte(evidenceJSON), &evidence); err != nil {
		return domain.Incident{}, false, fmt.Errorf("parse evidence: %w", err)
	}
	return domain.Incident{ID: id, Status: status, EvidenceIDs: evidence}, true, nil
}

// EventExists memeriksa apakah sebuah event dengan ID tertentu ada di store.
func (s *Store) EventExists(ctx context.Context, id string) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM events WHERE id = ?`, id).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// SaveJournalEntry menyimpan (upsert) satu baris jurnal berdasarkan action_id.
func (s *Store) SaveJournalEntry(ctx context.Context, e domain.JournalEntry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO journal (action_id, idempotency_key, incident_id, action, target, state, message, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(action_id) DO UPDATE SET
		   state = excluded.state,
		   message = excluded.message,
		   updated_at = excluded.updated_at`,
		e.ActionID, e.IdempotencyKey, e.IncidentID, e.Action, e.Target, string(e.State), e.Message, e.UpdatedAt.UTC().UnixNano(),
	)
	return err
}

// JournalEntry mengambil satu baris jurnal berdasarkan action_id.
func (s *Store) JournalEntry(ctx context.Context, actionID string) (domain.JournalEntry, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT action_id, idempotency_key, incident_id, action, target, state, message, updated_at
		 FROM journal WHERE action_id = ?`, actionID)
	return scanJournal(row)
}

// JournalByKey mengambil satu baris jurnal berdasarkan idempotency_key.
func (s *Store) JournalByKey(ctx context.Context, key string) (domain.JournalEntry, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT action_id, idempotency_key, incident_id, action, target, state, message, updated_at
		 FROM journal WHERE idempotency_key = ?`, key)
	return scanJournal(row)
}

// JournalByState mengembalikan semua baris jurnal dengan state tertentu.
func (s *Store) JournalByState(ctx context.Context, states ...domain.JournalState) ([]domain.JournalEntry, error) {
	if len(states) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(states))
	args := make([]any, len(states))
	for i, st := range states {
		placeholders[i] = "?"
		args[i] = string(st)
	}
	query := `SELECT action_id, idempotency_key, incident_id, action, target, state, message, updated_at
	          FROM journal WHERE state IN (` + strings.Join(placeholders, ",") + `) ORDER BY updated_at`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.JournalEntry
	for rows.Next() {
		e, _, err := scanJournal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// journalScanner menyatukan *sql.Row dan *sql.Rows.
type journalScanner interface {
	Scan(dest ...any) error
}

func scanJournal(sc journalScanner) (domain.JournalEntry, bool, error) {
	var (
		e         domain.JournalEntry
		state     string
		updatedAt int64
	)
	err := sc.Scan(&e.ActionID, &e.IdempotencyKey, &e.IncidentID, &e.Action, &e.Target, &state, &e.Message, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.JournalEntry{}, false, nil
	}
	if err != nil {
		return domain.JournalEntry{}, false, err
	}
	e.State = domain.JournalState(state)
	e.UpdatedAt = time.Unix(0, updatedAt).UTC()
	return e, true, nil
}

// count adalah helper menghitung baris pada tabel (nama tabel dari konstanta
// internal, bukan input pengguna).
func (s *Store) count(ctx context.Context, table string) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// Close menutup koneksi database.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Ping memeriksa apakah koneksi database masih sehat (dipakai health check).
func (s *Store) Ping(ctx context.Context) error {
	if s.db == nil {
		return errors.New("store: database belum dibuka")
	}
	var n int
	return s.db.QueryRowContext(ctx, "SELECT 1").Scan(&n)
}
