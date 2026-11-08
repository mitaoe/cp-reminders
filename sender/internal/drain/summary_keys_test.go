package drain

import (
	"errors"
	"strings"
	"testing"

	"cp-reminders/sender/internal/outbox"
)

func TestCodeSpanShowsTheValueLiterally(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "k1", "`k1`"},
		{"backtick inside", "a`b", "``a`b``"},
		{"two backticks inside", "a``b", "```a``b```"},
		{"starts with a backtick", "`start", "`` `start ``"},
		{"ends with a backtick", "end`", "`` end` ``"},
		{"only a backtick", "`", "`` ` ``"},
		{"line breaks", "line\nbreak\r\nhere", "`line break here`"},
		{"html", "<img src=x>", "`<img src=x>`"},
		{"empty", "", "` `"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := code(tt.in); got != tt.want {
				t.Errorf("code(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSummaryRendersUnsentItemValuesLiterally(t *testing.T) {
	key := "k`](https://evil.example) **bold**\n# heading"
	items := []outbox.Item{{Key: key, Text: "reminder", StartUTC: "2026`x", DeadlineUTC: "[d](https://evil.example)"}}
	results := []outbox.Result{{Key: key, Status: outbox.StatusFailed, Error: "send: `oops` [x](https://evil.example)"}}
	var b strings.Builder
	if err := writeSummary(&b, summary{Err: errors.New("boom `x`"), Items: items, Results: results}); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{
		"Error: `` boom `x` ``\n",
		"\n### ``k`](https://evil.example) **bold** # heading``\n",
		"Contest start ``2026`x``, deadline `[d](https://evil.example)`.",
		" Reason: ``send: `oops` [x](https://evil.example)``",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "# ") {
			t.Errorf("summary has a line that renders as a top-level heading: %q", line)
		}
	}
}

func TestSummaryRendersUnackedItemValuesLiterally(t *testing.T) {
	key := "k`1"
	items := []outbox.Item{{Key: key, Text: "reminder"}}
	results := []outbox.Result{{Key: key, Status: outbox.StatusSent, MessageID: "M`1"}}
	var b strings.Builder
	if err := writeSummary(&b, summary{Err: errors.New("ack failed"), Items: items, Results: results, AckErr: errors.New("HTTP `502`")}); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{
		"Delivered but not acknowledged (`` HTTP `502` ``)",
		"\n- ``k`1`` (message ``M`1``)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
}
