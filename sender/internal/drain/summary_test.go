package drain

import (
	"errors"
	"strings"
	"testing"

	"cp-reminders/sender/internal/outbox"
)

func TestSummaryListsOnlyUnsentItemsWithText(t *testing.T) {
	items := []outbox.Item{item("k1", "first reminder"), item("k2", "second\nreminder"), item("k3", "third")}
	results := []outbox.Result{
		{Key: "k1", Status: outbox.StatusSent, MessageID: "M1"},
		{Key: "k2", Status: outbox.StatusFailed, Error: "send: websocket closed"},
		{Key: "k3", Status: outbox.StatusFailed, Error: "not attempted"},
	}
	var b strings.Builder
	if err := writeSummary(&b, summary{Err: errors.New("send failed"), Items: items, Results: results, Invalid: make([]error, len(items))}); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{
		"2 reminder(s) not delivered",
		"send failed",
		"k2", "send: websocket closed", "```\nsecond\nreminder\n```",
		"k3", "```\nthird\n```",
		"2026-11-07T14:35:00Z",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "first reminder") {
		t.Errorf("summary includes a delivered item:\n%s", got)
	}
}

func TestSummaryFenceIsLongerThanBackticksInText(t *testing.T) {
	text := "use ```code``` and ````more````"
	var b strings.Builder
	writeSummary(&b, summary{
		Err:     errors.New("x"),
		Items:   []outbox.Item{item("k", text)},
		Results: []outbox.Result{{Key: "k", Status: outbox.StatusFailed}},
		Invalid: []error{nil},
	})
	if !strings.Contains(b.String(), "`````\n"+text+"\n`````") {
		t.Errorf("fence not widened:\n%s", b.String())
	}
}

func TestSummaryWithoutItems(t *testing.T) {
	var b strings.Builder
	writeSummary(&b, summary{Err: errors.New("outbox claim: HTTP 503")})
	got := b.String()
	if !strings.Contains(got, "outbox claim: HTTP 503") || !strings.Contains(got, "No reminders were claimed") {
		t.Errorf("summary:\n%s", got)
	}
}

func TestSummaryReportsDeliveredButUnacked(t *testing.T) {
	var b strings.Builder
	writeSummary(&b, summary{
		Err:     errors.New("ack failed"),
		Items:   []outbox.Item{item("k1", "delivered text")},
		Results: []outbox.Result{{Key: "k1", Status: outbox.StatusSent, MessageID: "M1"}},
		AckErr:  errors.New("HTTP 502"),
	})
	got := b.String()
	if !strings.Contains(got, "k1") || !strings.Contains(got, "delivered") || !strings.Contains(got, "HTTP 502") {
		t.Errorf("summary:\n%s", got)
	}
	if strings.Contains(got, "delivered text") {
		t.Errorf("summary includes text of a delivered item:\n%s", got)
	}
}
