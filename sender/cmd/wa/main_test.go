package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	signallog "go.mau.fi/libsignal/logger"
	"go.mau.fi/whatsmeow/types"

	"cp-reminders/sender/internal/drain"
	"cp-reminders/sender/internal/outbox"
	"cp-reminders/sender/internal/state"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func testIdentity(t *testing.T) string {
	t.Helper()
	id, _, err := state.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRunRejectsUnknownOrMissingCommand(t *testing.T) {
	for _, args := range [][]string{nil, {"nope"}} {
		var out strings.Builder
		err := run(context.Background(), args, envMap(nil), &out)
		if !errors.Is(err, errUsage) {
			t.Errorf("run(%v): got %v, want errUsage", args, err)
		}
	}
}

func TestKeygenPrintsUsableIdentityAndRecipient(t *testing.T) {
	var out strings.Builder
	if err := run(context.Background(), []string{"keygen"}, envMap(nil), &out); err != nil {
		t.Fatalf("keygen: %v", err)
	}
	var identity, recipient string
	for _, line := range strings.Split(out.String(), "\n") {
		if r, ok := strings.CutPrefix(line, "# public key: "); ok {
			recipient = r
		}
		if strings.HasPrefix(line, "AGE-SECRET-KEY-1") {
			identity = line
		}
	}
	v, err := state.NewVault(identity)
	if err != nil {
		t.Fatalf("printed identity does not parse: %v\n%s", err, out.String())
	}
	if v.Recipient() != recipient {
		t.Errorf("recipient %q does not match identity (%q)", recipient, v.Recipient())
	}
}

type fakeWorker struct {
	t      *testing.T
	items  []outbox.Item
	mu     sync.Mutex
	claims int
	acks   []outbox.AckRequest
	srv    *httptest.Server
}

func newFakeWorker(t *testing.T, items ...outbox.Item) *fakeWorker {
	f := &fakeWorker{t: t, items: items}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/outbox/claim":
			f.claims++
			json.NewEncoder(w).Encode(outbox.ClaimResponse{Items: f.items})
		case "/outbox/ack":
			var req outbox.AckRequest
			json.NewDecoder(r.Body).Decode(&req)
			f.acks = append(f.acks, req)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeWorker) env(t *testing.T, extra map[string]string) func(string) string {
	m := map[string]string{
		envWorkerURL:   f.srv.URL,
		envSenderToken: "tok",
		envIdentity:    testIdentity(t),
		envAnnounceJID: "120363000000000001@g.us",
	}
	for k, v := range extra {
		m[k] = v
	}
	return envMap(m)
}

func TestDrainRequiresEnvironment(t *testing.T) {
	err := run(context.Background(), []string{"drain", "--state", t.TempDir()}, envMap(nil), io.Discard)
	if err == nil {
		t.Fatal("drain succeeded without environment")
	}
	for _, name := range []string{envWorkerURL, envSenderToken, envIdentity, envAnnounceJID} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not mention %s: %v", name, err)
		}
	}
}

func TestDrainRequiresStateFlag(t *testing.T) {
	f := newFakeWorker(t)
	err := run(context.Background(), []string{"drain"}, f.env(t, nil), io.Discard)
	if !errors.Is(err, errUsage) {
		t.Fatalf("got %v, want errUsage", err)
	}
	if f.claims != 0 {
		t.Error("claimed without --state")
	}
}

func TestDrainRejectsBadConfigBeforeClaim(t *testing.T) {
	for name, extra := range map[string]map[string]string{
		"bad jid":      {envAnnounceJID: "919999999999@s.whatsapp.net"},
		"bad identity": {envIdentity: "AGE-SECRET-KEY-1BOGUS"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeWorker(t)
			err := run(context.Background(), []string{"drain", "--state", t.TempDir()}, f.env(t, extra), io.Discard)
			if err == nil {
				t.Fatal("drain succeeded")
			}
			if strings.Contains(err.Error(), "BOGUS") {
				t.Errorf("error leaks identity: %v", err)
			}
			if f.claims != 0 {
				t.Error("claimed despite bad config")
			}
		})
	}
}

func TestDrainEmptyOutboxDoesNotTouchState(t *testing.T) {
	f := newFakeWorker(t)
	dir := t.TempDir()
	var out strings.Builder
	if err := run(context.Background(), []string{"drain", "--state", dir}, f.env(t, nil), &out); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if f.claims != 1 || len(f.acks) != 0 {
		t.Errorf("claims=%d acks=%d", f.claims, len(f.acks))
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("state dir modified: %v", entries)
	}
	if !strings.Contains(out.String(), "outbox empty") {
		t.Errorf("output: %s", out.String())
	}
}

func TestDrainWithoutSessionAcksFailedAndWritesSummary(t *testing.T) {
	f := newFakeWorker(t, outbox.Item{Key: "k1", Text: drain.Header + "\n\nCodeforces Round starts in 75 min", StartUTC: "2026-11-07T14:35:00Z", DeadlineUTC: "2026-11-07T14:25:00Z"})
	summaryPath := filepath.Join(t.TempDir(), "summary.md")
	env := f.env(t, map[string]string{envStepSummary: summaryPath})
	err := run(context.Background(), []string{"drain", "--state", t.TempDir(), "--jitter", "0s"}, env, io.Discard)
	if !errors.Is(err, state.ErrNoSession) {
		t.Fatalf("drain: got %v, want ErrNoSession", err)
	}
	if len(f.acks) != 1 || len(f.acks[0].Results) != 1 || f.acks[0].Results[0].Status != outbox.StatusFailed {
		t.Fatalf("acks: %+v", f.acks)
	}
	data, rerr := os.ReadFile(summaryPath)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "Codeforces Round starts in 75 min") {
		t.Errorf("summary:\n%s", data)
	}
}

type groupOnlySession struct {
	group drain.Group
	sent  []string
}

func (s *groupOnlySession) Group(context.Context, string) (drain.Group, error) { return s.group, nil }

func (s *groupOnlySession) SendText(_ context.Context, _, text string) (string, error) {
	s.sent = append(s.sent, text)
	return "MSG1", nil
}

func (s *groupOnlySession) Close() error { return nil }

func TestDrainRefusesNonAnnouncementsTarget(t *testing.T) {
	f := newFakeWorker(t, outbox.Item{Key: "k1", Text: drain.Header + "\n\nhi", StartUTC: "2026-11-07T14:35:00Z", DeadlineUTC: "2026-11-07T14:25:00Z"})
	d, _, err := newDrainer([]string{"--state", t.TempDir(), "--jitter", "0s"}, f.env(t, nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	sess := &groupOnlySession{group: drain.Group{IsAdmin: true}}
	d.Dial = func(context.Context) (drain.Session, error) { return sess, nil }
	if err := d.Run(context.Background()); !errors.Is(err, drain.ErrNotAnnouncements) {
		t.Fatalf("Run: got %v, want ErrNotAnnouncements", err)
	}
	if len(sess.sent) != 0 {
		t.Errorf("sent to a non-announcements group: %v", sess.sent)
	}
}

type releasingSession struct {
	groupOnlySession
	worker        *fakeWorker
	released      bool
	acksAtRelease int
}

func (s *releasingSession) Release() error {
	s.worker.mu.Lock()
	defer s.worker.mu.Unlock()
	s.released = true
	s.acksAtRelease = len(s.worker.acks)
	return nil
}

func TestDrainReleasesTheSessionAfterTheAck(t *testing.T) {
	f := newFakeWorker(t, outbox.Item{Key: "k1", Text: "hi", StartUTC: "2026-11-07T14:35:00Z", DeadlineUTC: "2026-11-07T14:25:00Z"})
	d, _, err := newDrainer([]string{"--state", t.TempDir(), "--jitter", "0s"}, f.env(t, nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	sess := &releasingSession{groupOnlySession: groupOnlySession{group: drain.Group{IsAnnouncements: true, IsAdmin: true}}, worker: f}
	d.Dial = func(context.Context) (drain.Session, error) { return sess, nil }
	dial := reflect.ValueOf(d.Dial).Pointer()
	if err := runDrainer(context.Background(), d, time.Minute); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if !sess.released || sess.acksAtRelease != 1 {
		t.Errorf("released=%v with %d ack(s) received, want released after the ack", sess.released, sess.acksAtRelease)
	}
	if reflect.ValueOf(d.Dial).Pointer() != dial {
		t.Error("runDrainer replaced the caller's Dial")
	}
}

func TestDrainReleasesTheSessionWhenTheRunFails(t *testing.T) {
	f := newFakeWorker(t, outbox.Item{Key: "k1", Text: "hi", StartUTC: "2026-11-07T14:35:00Z", DeadlineUTC: "2026-11-07T14:25:00Z"})
	d, _, err := newDrainer([]string{"--state", t.TempDir(), "--jitter", "0s"}, f.env(t, nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	sess := &releasingSession{groupOnlySession: groupOnlySession{group: drain.Group{IsAdmin: true}}, worker: f}
	d.Dial = func(context.Context) (drain.Session, error) { return sess, nil }
	if err := runDrainer(context.Background(), d, time.Minute); !errors.Is(err, drain.ErrNotAnnouncements) {
		t.Fatalf("drain: got %v, want ErrNotAnnouncements", err)
	}
	if !sess.released {
		t.Error("session not released after a failed run")
	}
}

func TestPairRefusesExistingSessionWithoutForce(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, state.SessionFile), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := envMap(map[string]string{envIdentity: testIdentity(t)})
	err := run(context.Background(), []string{"pair", "--state", dir}, env, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("pair: got %v", err)
	}
}

func TestErrorLineRedactsWhatsAppIDs(t *testing.T) {
	got := errorLine(fmt.Errorf("connect: %w", errors.New("stream replaced for 919876543210:5@s.whatsapp.net")))
	want := "wa: connect: stream replaced for <id>@s.whatsapp.net"
	if got != want {
		t.Errorf("errorLine: got %q, want %q", got, want)
	}
}

func TestLoggerRedactsWhatsAppIDs(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = stdout })

	f := newFlags("drain", io.Discard, true)
	if err := f.parse([]string{"--state", t.TempDir(), "--log-level", "WARN"}); err != nil {
		t.Fatal(err)
	}
	f.logger().Sub("Client").Warnf("retry receipt from %s", "919876543210@s.whatsapp.net")
	signallog.Warning("no session found for user 123456789012345_1:5")
	signallog.Info("below the log level")
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"[wa/Client WARN] retry receipt from <id>@s.whatsapp.net",
		"[wa/Signal WARN] main_test.go:",
		": no session found for user <number>:5",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output %q does not contain %q", out, want)
		}
	}
	for _, leak := range []string{"919876543210", "123456789012345", "below the log level"} {
		if strings.Contains(string(out), leak) {
			t.Errorf("output %q contains %q", out, leak)
		}
	}
}

func TestLoggerNeverPrintsSignalDebug(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = stdout })

	f := newFlags("drain", io.Discard, true)
	if err := f.parse([]string{"--state", t.TempDir(), "--log-level", "DEBUG"}); err != nil {
		t.Fatal(err)
	}
	f.logger().Debugf("whatsmeow debug line")
	signallog.Debug("Using cipherKey: [1 2 3 250]")
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "[wa DEBUG] whatsmeow debug line") {
		t.Errorf("output %q lacks the whatsmeow debug line", out)
	}
	if strings.Contains(string(out), "cipherKey") {
		t.Errorf("output %q contains a libsignal debug line", out)
	}
}

func TestCommandsRequireIdentity(t *testing.T) {
	for _, cmd := range []string{"pair", "groups"} {
		err := run(context.Background(), []string{cmd, "--state", t.TempDir()}, envMap(nil), io.Discard)
		if err == nil || !strings.Contains(err.Error(), envIdentity) {
			t.Errorf("%s: got %v", cmd, err)
		}
	}
}

func TestGroupsWithoutSession(t *testing.T) {
	env := envMap(map[string]string{envIdentity: testIdentity(t)})
	err := run(context.Background(), []string{"groups", "--state", t.TempDir()}, env, io.Discard)
	if !errors.Is(err, state.ErrNoSession) {
		t.Fatalf("groups: got %v, want ErrNoSession", err)
	}
}

func TestWriteGroupTable(t *testing.T) {
	self := types.NewADJID("919999999999", 0, 5)
	community := types.NewJID("120363000000000000", types.GroupServer)
	announce := types.NewJID("120363000000000001", types.GroupServer)
	other := types.NewJID("120363000000000002", types.GroupServer)

	parent := &types.GroupInfo{JID: community}
	parent.Name = "CP Community"
	parent.IsParent = true

	ann := &types.GroupInfo{JID: announce, Participants: []types.GroupParticipant{{JID: self.ToNonAD(), IsAdmin: true}}}
	ann.Name = "CP Community"
	ann.IsDefaultSubGroup = true
	ann.LinkedParentJID = community
	ann.IsAnnounce = true

	sub := &types.GroupInfo{JID: other, Participants: []types.GroupParticipant{{JID: self.ToNonAD()}}}
	sub.Name = "General"
	sub.LinkedParentJID = community

	var b strings.Builder
	if err := writeGroupTable(&b, []*types.GroupInfo{parent, ann, sub}, []types.JID{self}); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{"JID", "NAME", "PARENT", "DEFAULT_SUB", "LINKED_PARENT", "ANNOUNCE", "ADMIN", "General"} {
		if !strings.Contains(got, want) {
			t.Errorf("table missing %q:\n%s", want, got)
		}
	}
	candidates := 0
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "ANNOUNCEMENTS CANDIDATE:") {
			candidates++
			if !strings.Contains(line, announce.String()) || !strings.Contains(line, "admin=yes") {
				t.Errorf("candidate line: %q", line)
			}
		}
	}
	if candidates != 1 {
		t.Errorf("candidate lines: got %d, want 1\n%s", candidates, got)
	}
	annRow := rowFor(got, announce.String())
	if !strings.Contains(annRow, "yes") || !strings.Contains(annRow, community.String()) {
		t.Errorf("announcements row: %q", annRow)
	}
	if subRow := rowFor(got, other.String()); strings.Count(subRow, "yes") != 0 {
		t.Errorf("sub-group row should have no yes flags: %q", subRow)
	}
}

func rowFor(table, jid string) string {
	for _, line := range strings.Split(table, "\n") {
		if strings.HasPrefix(line, jid) {
			return line
		}
	}
	return ""
}
