package drain

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"cp-reminders/sender/internal/outbox"
	"cp-reminders/sender/internal/state"
)

const target = "120363000000000001@g.us"

var errLoggedOut = errors.New("whatsapp: logged out")

type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf(format, args...))
}

func (r *recorder) index(event string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Index(r.events, event)
}

type fakeOutbox struct {
	rec       *recorder
	items     []outbox.Item
	claimErr  error
	ackErr    error
	acks      []outbox.AckRequest
	ackCtxErr error
}

func (f *fakeOutbox) Claim(ctx context.Context, claimID string) ([]outbox.Item, error) {
	f.rec.add("claim %s", claimID)
	return f.items, f.claimErr
}

func (f *fakeOutbox) Ack(ctx context.Context, req outbox.AckRequest) error {
	f.rec.add("ack")
	f.ackCtxErr = ctx.Err()
	f.acks = append(f.acks, req)
	return f.ackErr
}

type fakeSession struct {
	rec      *recorder
	group    Group
	groupErr error
	sendErr  map[string]error
	sent     []string
	closeErr error
	nextID   int
}

func (s *fakeSession) Group(ctx context.Context, jid string) (Group, error) {
	s.rec.add("group %s", jid)
	return s.group, s.groupErr
}

func (s *fakeSession) SendText(ctx context.Context, jid, text string) (string, error) {
	body := contestName(text)
	s.rec.add("send %s %s", jid, body)
	if err := s.sendErr[body]; err != nil {
		return "", err
	}
	s.sent = append(s.sent, body)
	s.nextID++
	return fmt.Sprintf("MSG%d", s.nextID), nil
}

func (s *fakeSession) Close() error {
	s.rec.add("close")
	return s.closeErr
}

type fakeLedger struct {
	rec       *recorder
	entries   map[string]state.Entry
	recordErr error
}

func (l *fakeLedger) Lookup(key string) (state.Entry, bool) {
	e, ok := l.entries[key]
	return e, ok
}

func (l *fakeLedger) SentSince(t time.Time) int {
	n := 0
	for _, e := range l.entries {
		if e.SentAt.After(t) {
			n++
		}
	}
	return n
}

func (l *fakeLedger) Record(e state.Entry) error {
	l.rec.add("record %s", e.Key)
	if l.recordErr != nil {
		return l.recordErr
	}
	l.entries[e.Key] = e
	return nil
}

type harness struct {
	rec     *recorder
	ob      *fakeOutbox
	sess    *fakeSession
	ledger  *fakeLedger
	dialErr error
	out     strings.Builder
	summary strings.Builder
	d       *Drainer
}

var fixedNow = time.Date(2026, 11, 7, 13, 20, 0, 0, time.UTC)

func newHarness(items ...outbox.Item) *harness {
	rec := &recorder{}
	h := &harness{
		rec:    rec,
		ob:     &fakeOutbox{rec: rec, items: items},
		sess:   &fakeSession{rec: rec, group: Group{IsAnnouncements: true, IsAdmin: true}},
		ledger: &fakeLedger{rec: rec, entries: map[string]state.Entry{}},
	}
	h.d = &Drainer{
		Outbox: h.ob,
		Dial: func(ctx context.Context) (Session, error) {
			rec.add("dial")
			if h.dialErr != nil {
				return nil, h.dialErr
			}
			return h.sess, nil
		},
		Ledger:       h.ledger,
		Target:       target,
		Jitter:       90 * time.Second,
		Out:          &h.out,
		Summary:      &h.summary,
		Now:          func() time.Time { return fixedNow },
		Sleep:        func(ctx context.Context, d time.Duration) error { rec.add("sleep %v", d); return nil },
		RandDuration: func(max time.Duration) time.Duration { return max / 3 },
		NewClaimID:   func() string { return "claim-1" },
	}
	return h
}

func (h *harness) ack(t *testing.T) outbox.AckRequest {
	t.Helper()
	if len(h.ob.acks) != 1 {
		t.Fatalf("acks: got %d, want 1", len(h.ob.acks))
	}
	return h.ob.acks[0]
}

func wantResults(t *testing.T, got []outbox.Result, want ...outbox.Result) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("results: got %+v, want %+v", got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Key != w.Key || g.Status != w.Status || g.MessageID != w.MessageID {
			t.Errorf("result %d: got %+v, want %+v", i, g, w)
		}
		if w.Error != "" && !strings.Contains(g.Error, w.Error) {
			t.Errorf("result %d error: got %q, want it to contain %q", i, g.Error, w.Error)
		}
		if g.Status == outbox.StatusSent && g.Error != "" {
			t.Errorf("result %d: sent with error %q", i, g.Error)
		}
	}
}

func TestEmptyClaimDoesNotDial(t *testing.T) {
	h := newHarness()
	if err := h.d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !slices.Equal(h.rec.events, []string{"claim claim-1"}) {
		t.Errorf("events: %v", h.rec.events)
	}
	if h.summary.Len() != 0 {
		t.Errorf("summary written on success: %s", h.summary.String())
	}
}

func TestInvalidTargetFailsBeforeClaim(t *testing.T) {
	h := newHarness(item("k", "hi"))
	h.d.Target = "919999999999@s.whatsapp.net"
	err := h.d.Run(context.Background())
	if !errors.Is(err, ErrInvalidTarget) {
		t.Fatalf("Run: got %v, want ErrInvalidTarget", err)
	}
	if len(h.rec.events) != 0 {
		t.Errorf("events: %v", h.rec.events)
	}
}

func TestClaimFailureWritesSummary(t *testing.T) {
	h := newHarness()
	h.ob.claimErr = errors.New("outbox claim: HTTP 503: down")
	if err := h.d.Run(context.Background()); err == nil {
		t.Fatal("Run succeeded")
	}
	if !strings.Contains(h.summary.String(), "HTTP 503") {
		t.Errorf("summary: %s", h.summary.String())
	}
	if h.rec.index("dial") >= 0 {
		t.Error("dialled after claim failure")
	}
}

func TestSendsPendingItemsThenClosesThenAcks(t *testing.T) {
	h := newHarness(item("k1", "first"), item("k2", "second"))
	if err := h.d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	order := []string{"claim claim-1", "sleep 30s", "dial", "group " + target,
		"send " + target + " first", "record k1", "send " + target + " second", "record k2", "close", "ack"}
	if !slices.Equal(h.rec.events, order) {
		t.Errorf("events:\n got %v\nwant %v", h.rec.events, order)
	}
	ack := h.ack(t)
	if ack.ClaimID != "claim-1" {
		t.Errorf("ack claimId: %q", ack.ClaimID)
	}
	wantResults(t, ack.Results,
		outbox.Result{Key: "k1", Status: outbox.StatusSent, MessageID: "MSG1"},
		outbox.Result{Key: "k2", Status: outbox.StatusSent, MessageID: "MSG2"})
	if e := h.ledger.entries["k2"]; e.MessageID != "MSG2" || !e.SentAt.Equal(fixedNow) {
		t.Errorf("ledger entry: %+v", e)
	}
	if strings.Contains(h.out.String(), "first") {
		t.Errorf("message text printed to log:\n%s", h.out.String())
	}
	if h.summary.Len() != 0 {
		t.Errorf("summary written on success: %s", h.summary.String())
	}
}

func TestAllItemsAlreadySentSkipsWhatsApp(t *testing.T) {
	h := newHarness(item("k1", "first"))
	h.ledger.entries["k1"] = state.Entry{Key: "k1", MessageID: "PREV1"}
	if err := h.d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if h.rec.index("dial") >= 0 || h.rec.index("sleep 30s") >= 0 {
		t.Errorf("connected although nothing was pending: %v", h.rec.events)
	}
	wantResults(t, h.ack(t).Results, outbox.Result{Key: "k1", Status: outbox.StatusSent, MessageID: "PREV1"})
}

func TestLedgerHitIsNotResent(t *testing.T) {
	h := newHarness(item("k1", "first"), item("k2", "second"))
	h.ledger.entries["k1"] = state.Entry{Key: "k1", MessageID: "PREV1"}
	if err := h.d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !slices.Equal(h.sess.sent, []string{"second"}) {
		t.Errorf("sent: %v", h.sess.sent)
	}
	wantResults(t, h.ack(t).Results,
		outbox.Result{Key: "k1", Status: outbox.StatusSent, MessageID: "PREV1"},
		outbox.Result{Key: "k2", Status: outbox.StatusSent, MessageID: "MSG1"})
}

func TestInvalidItemsAreFailedOthersSent(t *testing.T) {
	h := newHarness(rawItem("k1", ""), item("k2", "ok"))
	err := h.d.Run(context.Background())
	if err == nil {
		t.Fatal("Run succeeded with an invalid item")
	}
	if !slices.Equal(h.sess.sent, []string{"ok"}) {
		t.Errorf("sent: %v", h.sess.sent)
	}
	wantResults(t, h.ack(t).Results,
		outbox.Result{Key: "k1", Status: outbox.StatusFailed, Error: "empty text"},
		outbox.Result{Key: "k2", Status: outbox.StatusSent, MessageID: "MSG1"})
}

func TestTooManyItemsSendsNothing(t *testing.T) {
	var items []outbox.Item
	for i := range MaxItems + 1 {
		items = append(items, item(fmt.Sprint(i), "x"))
	}
	h := newHarness(items...)
	if err := h.d.Run(context.Background()); err == nil {
		t.Fatal("Run succeeded")
	}
	if h.rec.index("dial") >= 0 {
		t.Error("dialled for an oversized claim")
	}
	for _, r := range h.ack(t).Results {
		if r.Status != outbox.StatusFailed {
			t.Errorf("result %+v", r)
		}
	}
}

func TestDialFailureAcksFailedAndWritesSummary(t *testing.T) {
	h := newHarness(item("k1", "first text"), item("k2", "second text"))
	h.dialErr = fmt.Errorf("connect: %w", errLoggedOut)
	err := h.d.Run(context.Background())
	if !errors.Is(err, errLoggedOut) {
		t.Fatalf("Run: got %v, want errLoggedOut", err)
	}
	if h.rec.index("close") >= 0 {
		t.Error("closed a session that was never opened")
	}
	wantResults(t, h.ack(t).Results,
		outbox.Result{Key: "k1", Status: outbox.StatusFailed, Error: "logged out"},
		outbox.Result{Key: "k2", Status: outbox.StatusFailed, Error: "logged out"})
	s := h.summary.String()
	for _, want := range []string{"first text", "second text", "logged out"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary missing %q:\n%s", want, s)
		}
	}
}

func TestRefusesNonAnnouncementGroup(t *testing.T) {
	h := newHarness(item("k1", "first"))
	h.sess.group.IsAnnouncements = false
	err := h.d.Run(context.Background())
	if !errors.Is(err, ErrNotAnnouncements) {
		t.Fatalf("Run: got %v, want ErrNotAnnouncements", err)
	}
	if len(h.sess.sent) != 0 {
		t.Errorf("sent: %v", h.sess.sent)
	}
	if h.rec.index("close") < 0 {
		t.Error("session not closed")
	}
	wantResults(t, h.ack(t).Results, outbox.Result{Key: "k1", Status: outbox.StatusFailed, Error: "announcement"})
}

func TestRefusesWhenNotAdmin(t *testing.T) {
	h := newHarness(item("k1", "first"))
	h.sess.group.IsAdmin = false
	if err := h.d.Run(context.Background()); !errors.Is(err, ErrNotAdmin) {
		t.Fatalf("Run: got %v, want ErrNotAdmin", err)
	}
	if len(h.sess.sent) != 0 {
		t.Errorf("sent: %v", h.sess.sent)
	}
}

func TestGroupLookupFailure(t *testing.T) {
	h := newHarness(item("k1", "first"))
	h.sess.groupErr = errors.New("item-not-found")
	if err := h.d.Run(context.Background()); err == nil {
		t.Fatal("Run succeeded")
	}
	if h.rec.index("close") < 0 {
		t.Error("session not closed")
	}
	wantResults(t, h.ack(t).Results, outbox.Result{Key: "k1", Status: outbox.StatusFailed, Error: "item-not-found"})
}

func TestSendErrorStopsAndReportsPartialSuccess(t *testing.T) {
	h := newHarness(item("k1", "first"), item("k2", "second"), item("k3", "third"))
	h.sess.sendErr = map[string]error{"second": fmt.Errorf("send: %w", errLoggedOut)}
	err := h.d.Run(context.Background())
	if !errors.Is(err, errLoggedOut) {
		t.Fatalf("Run: got %v, want errLoggedOut", err)
	}
	if h.rec.index("send "+target+" third") >= 0 {
		t.Error("kept sending after a failure")
	}
	if _, ok := h.ledger.entries["k2"]; ok {
		t.Error("failed item recorded in ledger")
	}
	if h.rec.index("close") > h.rec.index("ack") {
		t.Errorf("ack before close: %v", h.rec.events)
	}
	wantResults(t, h.ack(t).Results,
		outbox.Result{Key: "k1", Status: outbox.StatusSent, MessageID: "MSG1"},
		outbox.Result{Key: "k2", Status: outbox.StatusFailed, Error: "logged out"},
		outbox.Result{Key: "k3", Status: outbox.StatusFailed, Error: "not attempted"})
	s := h.summary.String()
	if !strings.Contains(s, "second") || !strings.Contains(s, "third") || strings.Contains(s, "```\nfirst\n```") {
		t.Errorf("summary:\n%s", s)
	}
}

func TestLedgerWriteFailureStopsFurtherSends(t *testing.T) {
	h := newHarness(item("k1", "first"), item("k2", "second"))
	h.ledger.recordErr = errors.New("disk full")
	err := h.d.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Run: got %v", err)
	}
	if !slices.Equal(h.sess.sent, []string{"first"}) {
		t.Errorf("sent: %v", h.sess.sent)
	}
	wantResults(t, h.ack(t).Results,
		outbox.Result{Key: "k1", Status: outbox.StatusSent, MessageID: "MSG1"},
		outbox.Result{Key: "k2", Status: outbox.StatusFailed, Error: "not attempted"})
}

func TestAckFailureFailsRun(t *testing.T) {
	h := newHarness(item("k1", "first"))
	h.ob.ackErr = errors.New("outbox ack: HTTP 502")
	err := h.d.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("Run: got %v", err)
	}
	if _, ok := h.ledger.entries["k1"]; !ok {
		t.Error("delivered item missing from ledger")
	}
	if s := h.summary.String(); !strings.Contains(s, "not acknowledged") || !strings.Contains(s, "k1") {
		t.Errorf("summary:\n%s", s)
	}
}

func TestCloseFailureFailsRun(t *testing.T) {
	h := newHarness(item("k1", "first"))
	h.sess.closeErr = errors.New("commit session: seal: disk full")
	err := h.d.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "seal") {
		t.Fatalf("Run: got %v", err)
	}
	wantResults(t, h.ack(t).Results, outbox.Result{Key: "k1", Status: outbox.StatusSent, MessageID: "MSG1"})
}

func TestAckRunsEvenWhenRunContextIsDone(t *testing.T) {
	h := newHarness(item("k1", "first"))
	ctx, cancel := context.WithCancel(context.Background())
	h.d.Dial = func(context.Context) (Session, error) {
		cancel()
		return nil, context.Canceled
	}
	if err := h.d.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: got %v", err)
	}
	h.ack(t)
	if h.ob.ackCtxErr != nil {
		t.Errorf("ack context already done: %v", h.ob.ackCtxErr)
	}
}

func TestJitterFailureAcksFailed(t *testing.T) {
	h := newHarness(item("k1", "first"))
	h.d.Sleep = func(context.Context, time.Duration) error { return context.DeadlineExceeded }
	if err := h.d.Run(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run: got %v", err)
	}
	if h.rec.index("dial") >= 0 {
		t.Error("dialled after jitter failed")
	}
	wantResults(t, h.ack(t).Results, outbox.Result{Key: "k1", Status: outbox.StatusFailed})
}

func TestDryRunConnectsAndVerifiesButDoesNotSendRecordOrAck(t *testing.T) {
	h := newHarness(item("k1", "first text"))
	h.d.DryRun = true
	if err := h.d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if h.rec.index("group "+target) < 0 || h.rec.index("close") < 0 {
		t.Errorf("events: %v", h.rec.events)
	}
	if len(h.sess.sent) != 0 || len(h.ledger.entries) != 0 || len(h.ob.acks) != 0 {
		t.Errorf("dry run had side effects: sent=%v ledger=%v acks=%v", h.sess.sent, h.ledger.entries, h.ob.acks)
	}
	if !strings.Contains(h.out.String(), "first text") {
		t.Errorf("dry run did not print text:\n%s", h.out.String())
	}
}

func TestDryRunStillRefusesNonAnnouncementGroup(t *testing.T) {
	h := newHarness(item("k1", "first"))
	h.d.DryRun = true
	h.sess.group.IsAnnouncements = false
	if err := h.d.Run(context.Background()); !errors.Is(err, ErrNotAnnouncements) {
		t.Fatalf("Run: got %v", err)
	}
	if len(h.ob.acks) != 0 {
		t.Error("dry run acked")
	}
}

func TestZeroJitterDoesNotSleep(t *testing.T) {
	h := newHarness(item("k1", "first"))
	h.d.Jitter = 0
	if err := h.d.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, e := range h.rec.events {
		if strings.HasPrefix(e, "sleep") {
			t.Errorf("slept with zero jitter: %v", h.rec.events)
		}
	}
}

func TestNilSummaryWriterIsAllowed(t *testing.T) {
	h := newHarness(item("k1", "first"))
	h.d.Summary = nil
	h.dialErr = errLoggedOut
	if err := h.d.Run(context.Background()); !errors.Is(err, errLoggedOut) {
		t.Fatalf("Run: got %v", err)
	}
}
