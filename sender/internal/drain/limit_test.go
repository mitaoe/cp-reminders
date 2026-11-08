package drain

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"cp-reminders/sender/internal/outbox"
	"cp-reminders/sender/internal/state"
)

func (h *harness) sentEarlier(n int, ago time.Duration) {
	for i := range n {
		key := fmt.Sprintf("earlier-%v-%d", ago, i)
		h.ledger.entries[key] = state.Entry{Key: key, SentAt: fixedNow.Add(-ago), MessageID: "OLD"}
	}
}

func TestDailyLimitStopsSendingAtTheLimit(t *testing.T) {
	h := newHarness(item("k1", "first"), item("k2", "second"))
	h.sentEarlier(MaxSentPerDay-1, time.Hour)
	err := h.d.Run(context.Background())
	if !errors.Is(err, ErrDailyLimit) {
		t.Fatalf("Run: got %v, want ErrDailyLimit", err)
	}
	if !slices.Equal(h.sess.sent, []string{"first"}) {
		t.Errorf("sent: %v", h.sess.sent)
	}
	wantResults(t, h.ack(t).Results,
		outbox.Result{Key: "k1", Status: outbox.StatusSent, MessageID: "MSG1"},
		outbox.Result{Key: "k2", Status: outbox.StatusFailed, Error: "daily limit"},
	)
}

func TestDailyLimitReachedDoesNotConnect(t *testing.T) {
	h := newHarness(item("k1", "first"))
	h.sentEarlier(MaxSentPerDay, 23*time.Hour)
	if err := h.d.Run(context.Background()); !errors.Is(err, ErrDailyLimit) {
		t.Fatalf("Run: got %v, want ErrDailyLimit", err)
	}
	if h.rec.index("dial") >= 0 {
		t.Errorf("connected although the limit was reached: %v", h.rec.events)
	}
	wantResults(t, h.ack(t).Results, outbox.Result{Key: "k1", Status: outbox.StatusFailed, Error: "daily limit"})
}

func TestDailyLimitCountsOnlyTheLast24Hours(t *testing.T) {
	h := newHarness(item("k1", "first"), item("k2", "second"))
	h.sentEarlier(MaxSentPerDay, 25*time.Hour)
	if err := h.d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !slices.Equal(h.sess.sent, []string{"first", "second"}) {
		t.Errorf("sent: %v", h.sess.sent)
	}
}

func TestAlreadySentItemsCountOnce(t *testing.T) {
	h := newHarness(item("k1", "first"), item("k2", "second"))
	h.sentEarlier(MaxSentPerDay-1, time.Hour)
	h.ledger.entries["k1"] = state.Entry{Key: "k1", SentAt: fixedNow.Add(-time.Minute), MessageID: "M0"}
	if err := h.d.Run(context.Background()); !errors.Is(err, ErrDailyLimit) {
		t.Fatalf("Run: got %v, want ErrDailyLimit", err)
	}
	if len(h.sess.sent) != 0 {
		t.Errorf("sent: %v", h.sess.sent)
	}
	wantResults(t, h.ack(t).Results,
		outbox.Result{Key: "k1", Status: outbox.StatusSent, MessageID: "M0"},
		outbox.Result{Key: "k2", Status: outbox.StatusFailed, Error: "daily limit"},
	)
}
