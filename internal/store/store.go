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
	"fmt"
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
