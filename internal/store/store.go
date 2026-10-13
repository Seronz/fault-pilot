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

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
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

func (s *Store) JournalMode(ctx context.Context) (string, error) {
	var mode string
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode;").Scan(&mode); err != nil {
		return "", err
	}
	return mode, nil
}

func (s *Store) AppendEvent(ctx context.Context, e domain.Event) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO events (id, timestamp, type, service, message) VALUES (?, ?, ?, ?, ?)`,
		e.ID, e.Timestamp.UTC().UnixNano(), e.Type, e.Service, e.Message,
	)
	return err
}

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

func (s *Store) CountEvents(ctx context.Context) (int, error) {
	return s.count(ctx, "events")
}

func (s *Store) CountIncidents(ctx context.Context) (int, error) {
	return s.count(ctx, "incidents")
}

func (s *Store) count(ctx context.Context, table string) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}
