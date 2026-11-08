package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"time"
)

// Ledger file name and size.
const (
	LedgerFile       = "sent.json"
	MaxLedgerEntries = 500
)

// Entry records one delivered reminder.
type Entry struct {
	Key       string    `json:"key"`
	SentAt    time.Time `json:"sentAt"`
	MessageID string    `json:"messageId"`
}

// Ledger is the sender's local record of delivered reminders (sent.json).
type Ledger struct {
	path    string
	entries []Entry
}

// LoadLedger reads the ledger at path. A missing file is an empty ledger.
func LoadLedger(path string) (*Ledger, error) {
	l := &Ledger{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load ledger: %w", err)
	}
	if err := json.Unmarshal(data, &l.entries); err != nil {
		return nil, fmt.Errorf("load ledger %s: %w", path, err)
	}
	return l, nil
}

// Lookup returns the entry recorded for key.
func (l *Ledger) Lookup(key string) (Entry, bool) {
	i := slices.IndexFunc(l.entries, func(e Entry) bool { return e.Key == key })
	if i < 0 {
		return Entry{}, false
	}
	return l.entries[i], true
}

// SentSince returns the number of entries sent after t.
func (l *Ledger) SentSince(t time.Time) int {
	n := 0
	for _, e := range l.entries {
		if e.SentAt.After(t) {
			n++
		}
	}
	return n
}

// Len returns the number of entries.
func (l *Ledger) Len() int { return len(l.entries) }

// Record appends e and persists the ledger. Keys already present are kept as is.
func (l *Ledger) Record(e Entry) error {
	if _, ok := l.Lookup(e.Key); ok {
		return nil
	}
	l.entries = append(l.entries, e)
	if n := len(l.entries) - MaxLedgerEntries; n > 0 {
		l.entries = slices.Delete(l.entries, 0, n)
	}
	err := writeAtomic(l.path, func(w io.Writer) error {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(l.entries)
	})
	if err != nil {
		return fmt.Errorf("record %s in ledger: %w", e.Key, err)
	}
	return nil
}
