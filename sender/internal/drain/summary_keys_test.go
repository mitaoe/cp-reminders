package drain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"cp-reminders/sender/internal/outbox"
)

const backslash = "\\"

func TestCodeSpanShowsTheValueLiterally(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "k1", "`k1`"},
		{"backtick inside", "a`b", "``a`b``"},
		{"two backticks inside", "a``b", "```a``b```"},
		{"starts with a backtick", "`start", "`` `start ``"},
		{"ends with a backtick", "end`", "`` end` ``"},
		{"starts and ends with a backtick", "`both`", "`` `both` ``"},
		{"only a backtick", "`", "`` ` ``"},
		{"only backticks", "```", "```` ``` ````"},
		{"html", "<img src=x>", "`<img src=x>`"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := code(tt.in); got != tt.want {
				t.Errorf("code(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCodeSpanKeepsTheValueOnOneLine(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"line breaks", "line\nbreak\r\nhere", "`line break here`"},
		{"tab", "a\tb", "`a b`"},
		{"line separator", "a" + string(rune(0x2028)) + "b", "`a b`"},
		{"empty", "", "` `"},
		{"whitespace only", " \t\n ", "` `"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := code(tt.in); got != tt.want {
				t.Errorf("code(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCodeSpanEscapesControlCharacters(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"right-to-left override", "abc" + string(rune(0x202E)) + "fdp.exe", "`abc" + backslash + "u202Efdp.exe`"},
		{"bidi isolate", string(rune(0x2066)) + "x" + string(rune(0x2069)), "`" + backslash + "u2066x" + backslash + "u2069`"},
		{"left-to-right mark", "a" + string(rune(0x200E)) + "b", "`a" + backslash + "u200Eb`"},
		{"nul", "a" + string(rune(0)) + "b", "`a" + backslash + "u0000b`"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := code(tt.in); got != tt.want {
				t.Errorf("code(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCodeSpanShortensLongValues(t *testing.T) {
	if got, want := code(strings.Repeat("x", maxCodeRunes)), "`"+strings.Repeat("x", maxCodeRunes)+"`"; got != want {
		t.Errorf("value at the limit: got %q, want %q", got, want)
	}
	if got, want := code(strings.Repeat("é", maxCodeRunes+50)), "`"+strings.Repeat("é", maxCodeRunes)+"…`"; got != want {
		t.Errorf("long value: got %q, want %q", got, want)
	}
}

func TestCodeSpanEscapesLongBacktickRuns(t *testing.T) {
	link := "[x](https://evil.example)"
	got := code(strings.Repeat("`", 80) + link)
	if want := "`" + strings.Repeat(backslash+"u0060", 80) + link + "`"; got != want {
		t.Errorf("80 backticks: got %q, want %q", got, want)
	}
	if got, want := code("a"+strings.Repeat("`", 15)+"b"), strings.Repeat("`", 16)+"a"+strings.Repeat("`", 15)+"b"+strings.Repeat("`", 16); got != want {
		t.Errorf("15 backticks: got %q, want %q", got, want)
	}
	if got, want := code("a"+strings.Repeat("`", 16)+"b"), "`a"+strings.Repeat(backslash+"u0060", 16)+"b`"; got != want {
		t.Errorf("16 backticks: got %q, want %q", got, want)
	}
}

func TestErrorSpanAllowsLongerValuesAndPointsToTheLog(t *testing.T) {
	if got, want := errorCode(strings.Repeat("x", maxErrorRunes)), "`"+strings.Repeat("x", maxErrorRunes)+"`"; got != want {
		t.Errorf("error at the limit: got %q, want %q", got, want)
	}
	if got, want := errorCode(strings.Repeat("x", maxErrorRunes+1)), "`"+strings.Repeat("x", maxErrorRunes)+"…` (full error in the run log)"; got != want {
		t.Errorf("long error: got %q, want %q", got, want)
	}
}

func TestSummaryShortensLongErrors(t *testing.T) {
	long := strings.Repeat("e", maxErrorRunes+10)
	items := []outbox.Item{{Key: "k1", Text: "reminder"}}
	results := []outbox.Result{{Key: "k1", Status: outbox.StatusSent, MessageID: "M1"}}
	var b strings.Builder
	if err := writeSummary(&b, summary{Err: errors.New(long), Items: items, Results: results, AckErr: errors.New(long)}); err != nil {
		t.Fatal(err)
	}
	shortened := "`" + strings.Repeat("e", maxErrorRunes) + "…` (full error in the run log)"
	for _, want := range []string{"Error: " + shortened + "\n", "Delivered but not acknowledged (" + shortened + ")"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("summary missing %q", want)
		}
	}
}

func TestSummaryWithholdsTheTextOfRefusedItems(t *testing.T) {
	items := []outbox.Item{
		{Key: "good", Text: "valid reminder text"},
		{Key: "bad", Text: "malicious text https://evil.example"},
	}
	results := []outbox.Result{
		{Key: "good", Status: outbox.StatusFailed, Error: "send: websocket closed"},
		{Key: "bad", Status: outbox.StatusFailed, Error: "link to a host outside the allowlist"},
	}
	invalid := []error{nil, errors.New("link to a host outside the allowlist: \"evil.example\"")}
	var b strings.Builder
	if err := writeSummary(&b, summary{Err: errors.New("x"), Items: items, Results: results, Invalid: invalid}); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if strings.Contains(got, "malicious text") {
		t.Errorf("summary shows the text of a refused item:\n%s", got)
	}
	manual, refused, ok := strings.Cut(got, "Refused by the sender's checks")
	if !ok {
		t.Fatalf("summary has no section for refused items:\n%s", got)
	}
	if !strings.Contains(manual, "2 reminder(s) not delivered") || !strings.Contains(manual, "### `good`") || !strings.Contains(manual, "valid reminder text") || strings.Contains(manual, "`bad`") {
		t.Errorf("manual section wrong:\n%s", manual)
	}
	if !strings.Contains(refused, "\n- `bad`: `link to a host outside the allowlist: \"evil.example\"`\n") {
		t.Errorf("refused section wrong:\n%s", refused)
	}
}

func TestSummaryListsAtMostMaxItemsPerSection(t *testing.T) {
	var items []outbox.Item
	var results []outbox.Result
	var invalid []error
	tooMany := errors.New("claim returned 7 items, more than the limit of 5")
	for i := range MaxItems + 2 {
		key := "refused-" + strings.Repeat("r", i+1)
		items = append(items, outbox.Item{Key: key, Text: "text of " + key})
		results = append(results, outbox.Result{Key: key, Status: outbox.StatusFailed, Error: tooMany.Error()})
		invalid = append(invalid, tooMany)
	}
	for i := range MaxItems + 1 {
		key := "unsent-" + strings.Repeat("u", i+1)
		items = append(items, outbox.Item{Key: key, Text: "text of " + key})
		results = append(results, outbox.Result{Key: key, Status: outbox.StatusFailed, Error: "not attempted"})
		invalid = append(invalid, nil)
	}
	var b strings.Builder
	if err := writeSummary(&b, summary{Err: errors.New("x"), Items: items, Results: results, Invalid: invalid}); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if n := strings.Count(got, "\n### "); n != MaxItems {
		t.Errorf("listed %d reminders to post, want %d:\n%s", n, MaxItems, got)
	}
	if n := strings.Count(got, "\n- `refused-"); n != MaxItems {
		t.Errorf("listed %d refused reminders, want %d:\n%s", n, MaxItems, got)
	}
	for _, want := range []string{"1 more not listed.", "2 more not listed."} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "text of refused-") {
		t.Errorf("summary shows the text of a refused item:\n%s", got)
	}
}

func TestSummaryRendersUnsentItemValuesLiterally(t *testing.T) {
	key := "k`](https://evil.example) **bold**\n# heading"
	items := []outbox.Item{{Key: key, Text: "reminder", StartUTC: "2026`x\n## start", DeadlineUTC: "[d](https://evil.example)\n### deadline"}}
	results := []outbox.Result{{Key: key, Status: outbox.StatusFailed, Error: "send: `oops` [x](https://evil.example)"}}
	var b strings.Builder
	if err := writeSummary(&b, summary{Err: errors.New("boom `x`"), Items: items, Results: results}); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	heading := "### ``k`](https://evil.example) **bold** # heading``"
	for _, want := range []string{
		"Error: `` boom `x` ``\n",
		"\n" + heading + "\n",
		"Contest start ``2026`x ## start``, deadline `[d](https://evil.example) ### deadline`.",
		" Reason: ``send: `oops` [x](https://evil.example)``",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "## WhatsApp sender failed: ") && line != heading {
			t.Errorf("summary has an unexpected heading line: %q", line)
		}
	}
}

func TestSummaryRendersUnackedItemValuesLiterally(t *testing.T) {
	key := "k`1"
	items := []outbox.Item{{Key: key, Text: "reminder"}}
	results := []outbox.Result{{Key: key, Status: outbox.StatusSent, MessageID: "M`1\n# id"}}
	var b strings.Builder
	if err := writeSummary(&b, summary{Err: errors.New("ack failed"), Items: items, Results: results, AckErr: errors.New("HTTP `502`")}); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{
		"Delivered but not acknowledged (`` HTTP `502` ``)",
		"\n- ``k`1`` (message ``M`1 # id``)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "## WhatsApp sender failed: ") {
			t.Errorf("summary has an unexpected heading line: %q", line)
		}
	}
}

func TestRunReportsInvalidItemsWithoutTheirText(t *testing.T) {
	refusedText := "REFUSED" + strings.Repeat("x", MaxTextRunes)
	h := newHarness(
		outbox.Item{Key: "too-long", Text: refusedText, StartUTC: "2026-11-07T14:35:00Z", DeadlineUTC: "2026-11-07T14:25:00Z"},
		outbox.Item{Key: "fine", Text: "fine reminder", StartUTC: "2026-11-07T14:35:00Z", DeadlineUTC: "2026-11-07T14:25:00Z"},
	)
	if err := h.d.Run(context.Background()); err == nil {
		t.Fatal("Run succeeded with an invalid item")
	}
	got := h.summary.String()
	if strings.Contains(got, "REFUSED") {
		t.Errorf("summary shows the text of an invalid item:\n%s", got)
	}
	if !strings.Contains(got, "Refused by the sender's checks") || !strings.Contains(got, "\n- `too-long`: `text is ") {
		t.Errorf("summary does not list the invalid item as refused:\n%s", got)
	}
}

func FuzzCode(f *testing.F) {
	for _, seed := range []string{"k1", "a`b", "`x`", "```", "", " ", "line\nbreak", "a" + string(rune(0x202E)) + "b", strings.Repeat("`", 300)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := code(s)
		if strings.ContainsAny(out, "\n\r") {
			t.Fatalf("code(%q) = %q contains a line break", s, out)
		}
		open := len(out) - len(strings.TrimLeft(out, "`"))
		closing := len(out) - len(strings.TrimRight(out, "`"))
		if open == 0 || open > len(out)/2 {
			t.Fatalf("code(%q) = %q has no opening delimiter", s, out)
		}
		inner := out[open : len(out)-open]
		if closing < open || !strings.HasSuffix(out, strings.Repeat("`", open)) || strings.HasPrefix(inner, "`") || strings.HasSuffix(inner, "`") {
			t.Fatalf("code(%q) = %q does not end with its opening delimiter", s, out)
		}
		if longestBacktickRun(inner) >= open {
			t.Fatalf("code(%q) = %q has a backtick run as long as its delimiter", s, out)
		}
		if open > 17 {
			t.Fatalf("code(%q) = %q has a delimiter of %d backticks", s, out, open)
		}
		value := oneLine(s)
		if value == "" || !utf8.ValidString(s) || utf8.RuneCountInString(value) > maxCodeRunes || strings.IndexFunc(value, needsEscape) >= 0 || longestBacktickRun(value) >= 16 {
			return
		}
		shown := inner
		if strings.HasPrefix(shown, " ") && strings.HasSuffix(shown, " ") && strings.Trim(shown, " ") != "" {
			shown = shown[1 : len(shown)-1]
		}
		if shown != value {
			t.Fatalf("code(%q) = %q shows %q, want %q", s, out, shown, value)
		}
	})
}

func needsEscape(r rune) bool {
	return !unicode.IsGraphic(r)
}
