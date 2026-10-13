package audit

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"

	"github.com/seronz/fault-pilot/internal/domain"
)

// JSONLWriter menulis audit trail sebagai JSON Lines (satu JSON per baris).
// Aman dipakai dari banyak goroutine.
type JSONLWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// NewJSONLWriter membuat writer yang menulis ke w.
func NewJSONLWriter(w io.Writer) *JSONLWriter {
	return &JSONLWriter{w: w}
}

// Append menulis satu entri sebagai satu baris JSON.
func (l *JSONLWriter) Append(ctx context.Context, e domain.JournalEntry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	_, err = l.w.Write(b)
	return err
}

// JSONLFile adalah audit trail yang menulis ke file (mode append).
type JSONLFile struct {
	*JSONLWriter
	f *os.File
}

// OpenJSONLFile membuka (membuat bila perlu) file audit trail.
func OpenJSONLFile(path string) (*JSONLFile, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &JSONLFile{JSONLWriter: NewJSONLWriter(f), f: f}, nil
}

// Close menutup file audit trail.
func (l *JSONLFile) Close() error {
	if l.f == nil {
		return nil
	}
	return l.f.Close()
}
