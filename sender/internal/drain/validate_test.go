package drain

import (
	"strings"
	"testing"
	"unicode/utf8"

	"cp-reminders/sender/internal/outbox"
)

func item(key, body string) outbox.Item {
	return rawItem(key, Header+"\n\n"+body)
}

func rawItem(key, text string) outbox.Item {
	return outbox.Item{Key: key, Text: text, StartUTC: "2026-11-07T14:35:00Z", DeadlineUTC: "2026-11-07T14:25:00Z"}
}

func TestValidateItems(t *testing.T) {
	headerRunes := utf8.RuneCountInString(Header)
	tests := []struct {
		name    string
		items   []outbox.Item
		invalid []bool
	}{
		{"ok", []outbox.Item{item("a", "hi"), item("b", "there")}, []bool{false, false}},
		{"empty text", []outbox.Item{rawItem("a", "")}, []bool{true}},
		{"whitespace text", []outbox.Item{rawItem("a", " \n\t")}, []bool{true}},
		{"1000 multibyte runes ok", []outbox.Item{rawItem("a", Header+"\n"+strings.Repeat("é", MaxTextRunes-headerRunes-1))}, []bool{false}},
		{"1001 runes", []outbox.Item{rawItem("a", Header+"\n"+strings.Repeat("x", MaxTextRunes-headerRunes))}, []bool{true}},
		{"empty key", []outbox.Item{item("", "hi")}, []bool{true}},
		{"duplicate key", []outbox.Item{item("a", "hi"), item("a", "again")}, []bool{false, true}},
		{"too many items", []outbox.Item{item("1", "x"), item("2", "x"), item("3", "x"), item("4", "x"), item("5", "x"), item("6", "x")}, []bool{true, true, true, true, true, true}},
		{"max items ok", []outbox.Item{item("1", "x"), item("2", "x"), item("3", "x"), item("4", "x"), item("5", "x")}, []bool{false, false, false, false, false}},
		{"invalid content", []outbox.Item{item("a", "ok"), rawItem("b", "no header")}, []bool{false, true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateItems(tt.items)
			if len(errs) != len(tt.items) {
				t.Fatalf("got %d results for %d items", len(errs), len(tt.items))
			}
			for i, err := range errs {
				if (err != nil) != tt.invalid[i] {
					t.Errorf("item %d: err=%v, want invalid=%v", i, err, tt.invalid[i])
				}
			}
		})
	}
}

func reminderText(platform, name, link, tip string) string {
	text := Header + "\n\n" +
		"📍 Platform: " + platform + "\n" +
		"🏆 Contest Name: " + name + "\n" +
		"🕒 Timing: 8:00 PM\n" +
		"⏳ Duration: 2:15 hours\n" +
		"🔗 Contest Link: " + link
	if tip != "" {
		text += "\n\n📌 Tip: " + tip
	}
	return text
}

func TestValidateText(t *testing.T) {
	tests := []struct {
		name string
		text string
		ok   bool
	}{
		{"codeforces with tip", reminderText("Codeforces", "Codeforces Round 1134 (Div. 4)", "https://codeforces.com/contests/2290", "never started cp? scared of cf?"), true},
		{"codechef", reminderText("Codechef", "Starters 264", "https://www.codechef.com/START264", ""), true},
		{"leetcode with tip", reminderText("LeetCode", "Weekly Contest 527", "https://leetcode.com/contest/weekly-contest-527/", "wake up, code, then chai"), true},
		{"atcoder with tip", reminderText("Atcoder", "AtCoder Beginner Contest 476", "https://atcoder.jp/contests/abc476", "aim for D today, upsolve E after"), true},
		{"punctuation that is not a domain", reminderText("Codeforces", "Educational Round 190 (Rated for Div. 2)", "https://codeforces.com/contests/2300", "e.g. read all problems first... then A."), true},
		{"uppercase host", reminderText("Codeforces", "Round", "https://CODEFORCES.com/contests/1", ""), true},
		{"no header", strings.TrimPrefix(reminderText("Codeforces", "Round", "https://codeforces.com/contests/1", ""), Header), false},
		{"header not on the first line", "[Forwarded] " + reminderText("Codeforces", "Round", "https://codeforces.com/contests/1", ""), false},
		{"http link", reminderText("Codeforces", "Round", "http://codeforces.com/contests/1", ""), false},
		{"other scheme", reminderText("Codeforces", "Round", "ftp://codeforces.com/contests/1", ""), false},
		{"other host", reminderText("Codeforces", "Round", "https://evil.example/contests/1", ""), false},
		{"host with an allowed prefix", reminderText("Codeforces", "Round", "https://codeforces.com.evil.io/contests/1", ""), false},
		{"subdomain of an allowed host", reminderText("Codeforces", "Round", "https://mirror.codeforces.com/contests/1", ""), false},
		{"userinfo before another host", reminderText("Codeforces", "Round", "https://codeforces.com@evil.io/contests/1", ""), false},
		{"second link to another host", reminderText("Codeforces", "Round", "https://codeforces.com/contests/1", "see https://evil.io/x"), false},
		{"bare domain in the tip", reminderText("Codeforces", "Round", "https://codeforces.com/contests/1", "join evil.com for prizes"), false},
		{"bare uppercase domain", reminderText("Codeforces", "Round", "https://codeforces.com/contests/1", "JOIN EVIL.COM"), false},
		{"bare domain with path in the name", reminderText("Codeforces", "Round at chat.whatsapp.com/abc", "https://codeforces.com/contests/1", ""), false},
		{"bare allowed host without scheme", reminderText("Codeforces", "Round", "https://codeforces.com/contests/1", "codeforces.com/blog"), false},
		{"non-latin domain", reminderText("Codeforces", "Round", "https://codeforces.com/contests/1", "пример.рф"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateText(tt.text)
			if (err == nil) != tt.ok {
				t.Errorf("validateText: err=%v, want ok=%v", err, tt.ok)
			}
		})
	}
}

func TestValidateTarget(t *testing.T) {
	for _, tc := range []struct {
		jid string
		ok  bool
	}{
		{"120363012345678901@g.us", true},
		{"", false},
		{"  ", false},
		{"919999999999@s.whatsapp.net", false},
		{"120363012345678901@g.us ", false},
		{"@g.us", false},
		{"120363012345678901@newsletter", false},
	} {
		if err := ValidateTarget(tc.jid); (err == nil) != tc.ok {
			t.Errorf("ValidateTarget(%q) = %v, want ok=%v", tc.jid, err, tc.ok)
		}
	}
}
