package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadLedgerMissingFileIsEmpty(t *testing.T) {
	l, err := LoadLedger(filepath.Join(t.TempDir(), "sent.json"))
	if err != nil {
		t.Fatalf("LoadLedger: %v", err)
	}
	if _, ok := l.Lookup("k"); ok {
		t.Error("Lookup on empty ledger found an entry")
	}
}

func TestLoadLedgerRejectsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sent.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLedger(path); err == nil {
		t.Fatal("LoadLedger accepted a corrupt file")
	}
}

func TestLedgerSentSinceCountsEntriesAfterTheTime(t *testing.T) {
	l, err := LoadLedger(filepath.Join(t.TempDir(), "sent.json"))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	for i, sent := range []time.Time{at.Add(-25 * time.Hour), at.Add(-24 * time.Hour), at.Add(-23 * time.Hour), at.Add(-time.Minute)} {
		if err := l.Record(Entry{Key: fmt.Sprintf("k%d", i), SentAt: sent, MessageID: "M"}); err != nil {
			t.Fatal(err)
		}
	}
	if got := l.SentSince(at.Add(-24 * time.Hour)); got != 2 {
		t.Errorf("SentSince: got %d, want 2", got)
	}
}

func TestLedgerRecordPersistsAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sent.json")
	l, err := LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	first := Entry{Key: "1|https://x/c/1|2026-11-07T13:30:00Z", SentAt: at, MessageID: "MSG1"}
	if err := l.Record(first); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := l.Record(Entry{Key: first.Key, SentAt: at.Add(time.Hour), MessageID: "MSG2"}); err != nil {
		t.Fatalf("Record duplicate: %v", err)
	}

	reloaded, err := LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Lookup(first.Key)
	if !ok {
		t.Fatal("entry not persisted")
	}
	if got.MessageID != "MSG1" || !got.SentAt.Equal(at) {
		t.Errorf("Lookup: got %+v, want first entry %+v", got, first)
	}

	var raw []map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("sent.json is not a JSON array: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("entries on disk: got %d, want 1", len(raw))
	}
	for _, field := range []string{"key", "sentAt", "messageId"} {
		if _, ok := raw[0][field]; !ok {
			t.Errorf("sent.json entry missing %q: %v", field, raw[0])
		}
	}
}

func TestLedgerKeepsLast500(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sent.json")
	l, err := LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 510 {
		if err := l.Record(Entry{Key: fmt.Sprint(i), MessageID: "m"}); err != nil {
			t.Fatal(err)
		}
	}
	reloaded, err := LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Lookup("9"); ok {
		t.Error("oldest entries were not dropped")
	}
	if _, ok := reloaded.Lookup("10"); !ok {
		t.Error("entry 10 should be kept")
	}
	if _, ok := reloaded.Lookup("509"); !ok {
		t.Error("newest entry missing")
	}
	if n := reloaded.Len(); n != MaxLedgerEntries {
		t.Errorf("Len: got %d, want %d", n, MaxLedgerEntries)
	}
}
