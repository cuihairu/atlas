package audit

import (
	"context"
	"fmt"
	"os"
)

// JSONLSink appends one JSON object per line (Entry.Marshal). It is the
// store-less fallback for deployments that do not run a database: grep/rg
// over the file is the query interface, and rotation plus expiry are the
// operator's job — it deliberately does not implement Retainer, so a
// retention window configured for this backend is ignored and logged.
//
// Each line is fsynced before Write returns, which is what makes the write
// "confirmed" in the compliance sense; Admin volume is low, so the cost is
// invisible. Without fsync the entries would sit in the OS cache and a
// power loss would quietly eat the tail of the archive.
type JSONLSink struct {
	f *os.File
}

func NewJSONLSink(path string) (*JSONLSink, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("audit sink: open %s: %w", path, err)
	}
	return &JSONLSink{f: f}, nil
}

func (s *JSONLSink) Write(ctx context.Context, e Entry) error {
	b, err := e.Marshal()
	if err != nil {
		return fmt.Errorf("audit sink: marshal: %w", err)
	}
	if _, err := s.f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("audit sink: append: %w", err)
	}
	if err := s.f.Sync(); err != nil {
		return fmt.Errorf("audit sink: fsync: %w", err)
	}
	return nil
}

func (s *JSONLSink) Close() error { return s.f.Close() }
