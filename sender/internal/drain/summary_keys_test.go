package drain

import (
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
		t.Errorf("value at the limit: got %d runes, want it unchanged", utf8.RuneCountInString(got))
	}
	if got, want := code(strings.Repeat("é", maxCodeRunes+50)), "`"+strings.Repeat("é", maxCodeRunes)+"…`"; got != want {
		t.Errorf("long value: got %q, want %q", got, want)
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
		value := oneLine(s)
		if value == "" || !utf8.ValidString(s) || utf8.RuneCountInString(value) > maxCodeRunes || strings.IndexFunc(value, needsEscape) >= 0 {
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
	return !unicode.IsGraphic(r) || unicode.Is(unicode.Bidi_Control, r)
}
