package drain

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSummaryRedactsWhatsAppIDsInErrors(t *testing.T) {
	h := newHarness(item("k1", "first"))
	h.sess.sendErr = map[string]error{"first": errors.New("no signal session established with 123456789012345_1:5")}
	if err := h.d.Run(context.Background()); err == nil {
		t.Fatal("Run: want an error")
	}
	for name, out := range map[string]string{"log": h.out.String(), "summary": h.summary.String()} {
		if strings.Contains(out, "123456789012345") {
			t.Errorf("%s contains a WhatsApp ID:\n%s", name, out)
		}
	}
	if !strings.Contains(h.summary.String(), "no signal session established with <number>:5") {
		t.Errorf("summary does not show the redacted error:\n%s", h.summary.String())
	}
}

func TestLogfRedactsWhatsAppIDs(t *testing.T) {
	h := newHarness()
	h.d.logf("from %s via %s", "120363012345678901@g.us", "919876543210:5")
	if got, want := h.out.String(), "from <id>@g.us via <number>:5\n"; got != want {
		t.Errorf("logf: got %q, want %q", got, want)
	}
}

func TestTargetCheckLogsTheVerification(t *testing.T) {
	h := newHarness(item("k1", "first"))
	if err := h.d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(h.out.String(), "\ntarget verified: announcements group, sender is admin\n") {
		t.Errorf("missing verification line:\n%s", h.out.String())
	}
}

func TestDryRunPrintsTextsUnchanged(t *testing.T) {
	text := "Call 9876543210 or see 120363012345678901@g.us"
	h := newHarness(item("k1", text))
	h.d.DryRun = true
	if err := h.d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(h.out.String(), "\n"+text+"\n") {
		t.Errorf("dry-run output does not contain the text verbatim:\n%s", h.out.String())
	}
}
