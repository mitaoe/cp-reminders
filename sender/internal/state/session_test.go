package state

import (
	"context"
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
	if err := s.Save(); err == nil {
		t.Error("Save after Discard should fail")
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
