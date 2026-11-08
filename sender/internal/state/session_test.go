package state

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/types"
)

func TestOpenSessionMissingReturnsErrNoSession(t *testing.T) {
	_, err := OpenSession(context.Background(), t.TempDir(), newTestVault(t), nil)
	if !errors.Is(err, ErrNoSession) {
		t.Fatalf("OpenSession: got %v, want ErrNoSession", err)
	}
}

func TestSessionCommitEncryptsAndReopens(t *testing.T) {
	ctx := context.Background()
	v := newTestVault(t)
	dir := t.TempDir()

	s, err := CreateSession(ctx, dir, v, nil)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	work := s.workDir
	dev := s.Container.NewDevice()
	jid := types.NewJID("919999999999", types.DefaultUserServer)
	jid.Device = 7
	dev.ID = &jid
	dev.Account = &waAdv.ADVSignedDeviceIdentity{
		Details:             []byte{1},
		AccountSignature:    make([]byte, 64),
		AccountSignatureKey: make([]byte, 32),
		DeviceSignature:     make([]byte, 64),
	}
	dev.PushName = "cp-reminders-test"
	if err := s.Container.PutDevice(ctx, dev); err != nil {
		t.Fatalf("PutDevice: %v", err)
	}
	if err := s.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := s.Commit(); err != nil {
		t.Errorf("second Commit should be a no-op: %v", err)
	}
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Errorf("plaintext work dir still exists after Commit: %v", err)
	}
	assertOnlyFiles(t, dir, SessionFile)
	if !SessionExists(dir) {
		t.Error("SessionExists: got false after Commit")
	}

	s2, err := OpenSession(ctx, dir, v, nil)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	defer s2.Discard()
	got, err := s2.PairedDevice(ctx)
	if err != nil {
		t.Fatalf("PairedDevice: %v", err)
	}
	if got.ID.String() != jid.String() || got.PushName != "cp-reminders-test" {
		t.Errorf("device after reopen: got %v %q", got.ID, got.PushName)
	}
}

func TestSessionSaveSealsASnapshotAndKeepsTheStoreOpen(t *testing.T) {
	ctx := context.Background()
	v := newTestVault(t)
	dir := t.TempDir()

	s, err := CreateSession(ctx, dir, v, nil)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	work := s.workDir
	dev := s.Container.NewDevice()
	jid := types.NewJID("919999999999", types.DefaultUserServer)
	jid.Device = 7
	dev.ID = &jid
	dev.Account = &waAdv.ADVSignedDeviceIdentity{
		Details:             []byte{1},
		AccountSignature:    make([]byte, 64),
		AccountSignatureKey: make([]byte, 32),
		DeviceSignature:     make([]byte, 64),
	}
	dev.PushName = "saved"
	if err := s.Container.PutDevice(ctx, dev); err != nil {
		t.Fatalf("PutDevice: %v", err)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	assertOnlyFiles(t, dir, SessionFile)

	dev.PushName = "after save"
	if err := s.Container.PutDevice(ctx, dev); err != nil {
		t.Fatalf("PutDevice after Save: %v", err)
	}
	if err := s.Discard(); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Errorf("plaintext work dir still exists after Discard: %v", err)
	}
	assertOnlyFiles(t, dir, SessionFile)

	s2, err := OpenSession(ctx, dir, v, nil)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	defer s2.Discard()
	got, err := s2.PairedDevice(ctx)
	if err != nil {
		t.Fatalf("PairedDevice: %v", err)
	}
	if got.PushName != "saved" {
		t.Errorf("reopened device: got push name %q, want the saved snapshot's", got.PushName)
	}
}

func TestSessionSaveAfterDiscardFails(t *testing.T) {
	s, err := CreateSession(context.Background(), t.TempDir(), newTestVault(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Discard(); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); !errors.Is(err, errSessionDiscarded) {
		t.Errorf("Save after Discard: got %v, want errSessionDiscarded", err)
	}
}

func TestSessionSaveAfterCommitFails(t *testing.T) {
	s, err := CreateSession(context.Background(), t.TempDir(), newTestVault(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); !errors.Is(err, errSessionCommitted) {
		t.Errorf("Save after Commit: got %v, want errSessionCommitted", err)
	}
}

func TestSessionSaveLeavesNoSnapshotInTheWorkDir(t *testing.T) {
	s, err := CreateSession(context.Background(), t.TempDir(), newTestVault(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Discard()
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	assertOnlyFiles(t, s.workDir, plainDBName)
}

func TestSessionSaveRetriesAfterAFailedSave(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	s, err := CreateSession(context.Background(), dir, newTestVault(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Discard()
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err == nil {
		t.Fatal("Save into a missing state directory should fail")
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save after a failed Save: %v", err)
	}
	assertOnlyFiles(t, dir, SessionFile)
}

func TestSessionReleaseSavesAgainAfterAFailedSave(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	s, err := CreateSession(context.Background(), dir, newTestVault(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	work := s.workDir
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err == nil {
		t.Fatal("Save into a missing state directory should fail")
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	assertOnlyFiles(t, dir, SessionFile)
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Errorf("plaintext work dir still exists after Release: %v", err)
	}
}

func TestSessionReleaseSavesASessionThatWasNeverSaved(t *testing.T) {
	dir := t.TempDir()
	s, err := CreateSession(context.Background(), dir, newTestVault(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	work := s.workDir
	if err := s.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	assertOnlyFiles(t, dir, SessionFile)
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Errorf("plaintext work dir still exists after Release: %v", err)
	}
}

func TestSessionReleaseAfterASuccessfulSaveDoesNotSaveAgain(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := CreateSession(ctx, dir, newTestVault(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, SessionFile)); err != nil {
		t.Fatal(err)
	}
	if err := s.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	assertOnlyFiles(t, dir)
}

func TestSessionSaveSnapshotExcludesAnUncommittedWrite(t *testing.T) {
	ctx := context.Background()
	v := newTestVault(t)
	dir := t.TempDir()
	s, err := CreateSession(ctx, dir, v, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Discard()
	if _, err := s.db.ExecContext(ctx, "CREATE TABLE probe (n INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "INSERT INTO probe (n) VALUES (1)"); err != nil {
		t.Fatal(err)
	}

	writer, err := sql.Open("sqlite", filepath.Join(s.workDir, plainDBName)+sqlitePragmas)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	tx, err := writer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "INSERT INTO probe (n) VALUES (2)"); err != nil {
		t.Fatal(err)
	}

	if err := s.Save(); err != nil {
		t.Fatalf("Save during an open write transaction: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit after Save: %v", err)
	}

	s2, err := OpenSession(ctx, dir, v, nil)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	defer s2.Discard()
	var n int
	if err := s2.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM probe").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("rows in the snapshot: got %d, want 1 (the committed one)", n)
	}
}

func TestSessionDiscardWritesNothing(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := CreateSession(ctx, dir, newTestVault(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	work := s.workDir
	if err := s.Discard(); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Errorf("work dir still exists after Discard: %v", err)
	}
	assertOnlyFiles(t, dir)
	if err := s.Commit(); err == nil {
		t.Error("Commit after Discard should fail")
	}
}

func TestPairedDeviceOnUnpairedSession(t *testing.T) {
	ctx := context.Background()
	s, err := CreateSession(ctx, t.TempDir(), newTestVault(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Discard()
	if _, err := s.PairedDevice(ctx); !errors.Is(err, ErrNotPaired) {
		t.Fatalf("PairedDevice: got %v, want ErrNotPaired", err)
	}
}

func TestOpenSessionWrongIdentityLeavesNoPlaintext(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := CreateSession(ctx, dir, newTestVault(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}
	tmpBefore := countTempWorkDirs(t)
	if _, err := OpenSession(ctx, dir, newTestVault(t), nil); err == nil {
		t.Fatal("OpenSession with wrong identity succeeded")
	}
	if after := countTempWorkDirs(t); after != tmpBefore {
		t.Errorf("work dirs in temp: before %d, after %d", tmpBefore, after)
	}
}

func assertOnlyFiles(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if len(got) != len(want) {
		t.Fatalf("files in %s: got %v, want %v", dir, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("files in %s: got %v, want %v", dir, got, want)
		}
	}
}

func countTempWorkDirs(t *testing.T) int {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(os.TempDir(), workDirPattern))
	if err != nil {
		t.Fatal(err)
	}
	return len(m)
}
