package drain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"cp-reminders/sender/internal/state"
)

func TestSummaryRedactsWhatsAppIDsInErrors(t *testing.T) {
	h := newHarness(item("k1", "first"))
	h.sess.sendErr = map[string]error{"first": errors.New("no signal session with 919876543210:5@s.whatsapp.net")}
	if err := h.d.Run(context.Background()); err == nil {
		t.Fatal("Run: want an error")
	}
	for name, out := range map[string]string{"log": h.out.String(), "summary": h.summary.String()} {
		if strings.Contains(out, "919876543210") {
			t.Errorf("%s contains a phone number:\n%s", name, out)
		}
	}
	if !strings.Contains(h.summary.String(), "<id>@s.whatsapp.net") {
		t.Errorf("summary does not show the redacted ID:\n%s", h.summary.String())
	}
}

func TestLogRedactsWhatsAppIDs(t *testing.T) {
	h := newHarness(item("k1", "first"))
	h.ledger.entries["k1"] = state.Entry{Key: "k1", MessageID: "120363012345678901@g.us"}
	if err := h.d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := h.out.String()
	if strings.Contains(out, "120363012345678901") || !strings.Contains(out, "<id>@g.us") {
		t.Errorf("log not redacted:\n%s", out)
	}
}

func TestTargetCheckLogsNoGroupDetails(t *testing.T) {
	h := newHarness(item("k1", "first"))
	if err := h.d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := h.out.String()
	if !strings.Contains(out, "target verified: announcements group, sender is admin") {
		t.Errorf("missing verification line:\n%s", out)
	}
	if strings.Contains(out, strings.TrimSuffix(target, "@g.us")) {
		t.Errorf("log contains the target group ID:\n%s", out)
	}
}
