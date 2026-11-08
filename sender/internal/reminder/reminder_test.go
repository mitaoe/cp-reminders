package reminder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func ch(r rune) string { return string(r) }

var codepoint = regexp.MustCompile(`<U\+([0-9A-F]{4,6})>`)

func expand(s string) string {
	return codepoint.ReplaceAllStringFunc(s, func(token string) string {
		n, err := strconv.ParseUint(codepoint.FindStringSubmatch(token)[1], 16, 32)
		if err != nil {
			panic(err)
		}
		return string(rune(n))
	})
}

func readTestdata(t *testing.T, name string, v any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
}

func valid() Reminder {
	return Reminder{
		Platform: "Codeforces",
		Name:     "Codeforces Round 1134 (Div. 4)",
		Timing:   "8:05 PM",
		Duration: "2:15 hours",
		Link:     "https://codeforces.com/contests/2290",
		Tip:      "never started cp? scared of cf?",
	}
}

func TestStringAndParseRoundTrip(t *testing.T) {
	withoutTip := valid()
	withoutTip.Tip = ""
	for _, r := range []Reminder{valid(), withoutTip} {
		got, err := Parse(r.String())
		if err != nil {
			t.Fatalf("Parse(%q): %v", r.String(), err)
		}
		if got != r {
			t.Errorf("round trip: got %+v, want %+v", got, r)
		}
	}
}

func TestParseAcceptsEveryPlatformAndFieldForm(t *testing.T) {
	for name, r := range map[string]Reminder{
		"codechef":                 {Platform: "Codechef", Name: "Starters 264", Timing: "8:00 PM", Duration: "2 hours", Link: "https://www.codechef.com/START264"},
		"leetcode morning":         {Platform: "LeetCode", Name: "Weekly Contest 527", Timing: "8:00 AM", Duration: "1:30 hours", Link: "https://leetcode.com/contest/weekly-contest-527/"},
		"atcoder":                  {Platform: "Atcoder", Name: "AtCoder Beginner Contest 476", Timing: "5:30 PM", Duration: "1:40 hours", Link: "https://atcoder.jp/contests/abc476", Tip: "aim for D today, upsolve E after"},
		"atcoder capitals":         {Platform: "Atcoder", Name: "KeioPC 2025", Timing: "1:00 PM", Duration: "5 hours", Link: "https://atcoder.jp/contests/KeioPC2025"},
		"twelve o'clock":           {Platform: "Codeforces", Name: "Round", Timing: "12:35 PM", Duration: "3 hours", Link: "https://codeforces.com/contests/1"},
		"defused domain in a name": {Platform: "Codeforces", Name: "Refact" + ch(0x2024) + "ai Match 1 (Codeforces Round 985)", Timing: "8:05 PM", Duration: "2:15 hours", Link: "https://codeforces.com/contests/2029"},
		"longest name and tip":     {Platform: "Codeforces", Name: strings.Repeat(ch(0x1f525), MaxNameRunes), Timing: "8:05 PM", Duration: "2:15 hours", Link: "https://codeforces.com/contests/1", Tip: strings.Repeat(ch(0x1f525), MaxTipRunes)},
		"longest link":             {Platform: "Codechef", Name: "Starters", Timing: "8:00 PM", Duration: "2 hours", Link: "https://www.codechef.com/" + strings.Repeat("A", MaxLinkLength-len("https://www.codechef.com/"))},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(r.String()); err != nil {
				t.Errorf("Parse: %v\n%s", err, r.String())
			}
		})
	}
}

func TestParseRefusesOtherLayouts(t *testing.T) {
	text := valid().String()
	for name, s := range map[string]string{
		"empty":                  "",
		"only spaces":            " \n\t",
		"no header":              strings.TrimPrefix(text, Header),
		"header not alone":       "[Forwarded] " + text,
		"carriage returns":       strings.ReplaceAll(text, "\n", "\r\n"),
		"extra line":             text + "\njoin us",
		"tip without blank line": strings.Replace(text, "\n\n📌", "\n📌", 1),
		"empty tip":              strings.TrimSuffix(text, valid().Tip),
		"fields out of order":    strings.Replace(text, "🕒 Timing: 8:05 PM\n⏳ Duration: 2:15 hours", "⏳ Duration: 2:15 hours\n🕒 Timing: 8:05 PM", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(s); err == nil {
				t.Errorf("Parse accepted %q", s)
			}
		})
	}
}

func TestParseRefusesInvalidFields(t *testing.T) {
	const link = "https://codeforces.com/contests/1"
	for name, change := range map[string]func(*Reminder){
		"unknown platform":          func(r *Reminder) { r.Platform = "Codeforcez" },
		"link of another platform":  func(r *Reminder) { r.Platform = "LeetCode" },
		"timing not a time":         func(r *Reminder) { r.Timing = "call now" },
		"timing hour 13":            func(r *Reminder) { r.Timing = "13:05 PM" },
		"timing leading zero":       func(r *Reminder) { r.Timing = "08:05 PM" },
		"duration not a duration":   func(r *Reminder) { r.Duration = "visit evil.com" },
		"empty name":                func(r *Reminder) { r.Name = " " },
		"name too long":             func(r *Reminder) { r.Name = strings.Repeat(ch(0x1f525), MaxNameRunes+1) },
		"tip too long":              func(r *Reminder) { r.Tip = strings.Repeat(ch(0x1f525), MaxTipRunes+1) },
		"unsafe name":               func(r *Reminder) { r.Name = "Round at chat.whatsapp.com/abc" },
		"unsafe tip":                func(r *Reminder) { r.Tip = "WhatsApp +91 98765 43210" },
		"http":                      func(r *Reminder) { r.Link = "http://codeforces.com/contests/1" },
		"other host":                func(r *Reminder) { r.Link = "https://evil.example/contests/1" },
		"allowed prefix":            func(r *Reminder) { r.Link = "https://codeforces.com.evil.io/contests/1" },
		"subdomain":                 func(r *Reminder) { r.Link = "https://mirror.codeforces.com/contests/1" },
		"www on codeforces":         func(r *Reminder) { r.Link = "https://www.codeforces.com/contests/1" },
		"user info":                 func(r *Reminder) { r.Link = "https://codeforces.com@evil.io/contests/1" },
		"port":                      func(r *Reminder) { r.Link = "https://codeforces.com:8443/contests/1" },
		"query":                     func(r *Reminder) { r.Link = link + "?next=https://evil.io" },
		"fragment":                  func(r *Reminder) { r.Link = link + "#x" },
		"uppercase host":            func(r *Reminder) { r.Link = "https://CODEFORCES.com/contests/1" },
		"codeforces blog":           func(r *Reminder) { r.Link = "https://codeforces.com/blog/entry/150813" },
		"codeforces singular":       func(r *Reminder) { r.Link = "https://codeforces.com/contest/1" },
		"codeforces trailing slash": func(r *Reminder) { r.Link = link + "/" },
		"codeforces letters":        func(r *Reminder) { r.Link = "https://codeforces.com/contests/abc" },
		"no-break space":            func(r *Reminder) { r.Link = link + ch(0xa0) + "https://evil.com/login" },
		"ideographic space":         func(r *Reminder) { r.Link = link + ch(0x3000) + "https://evil.com/login" },
		"line separator":            func(r *Reminder) { r.Link = link + ch(0x2028) + "https://evil.com/login" },
		"next line":                 func(r *Reminder) { r.Link = link + ch(0x85) + "https://evil.com/login" },
		"quote":                     func(r *Reminder) { r.Link = link + `"https://evil.com` },
		"angle brackets":            func(r *Reminder) { r.Link = link + "<https://evil.com>" },
		"pipe":                      func(r *Reminder) { r.Link = link + "|evil.com" },
		"parenthesis":               func(r *Reminder) { r.Link = link + ")https://evil.com" },
		"comma":                     func(r *Reminder) { r.Link = link + ",https://evil.com" },
		"codechef without www":      func(r *Reminder) { r.Platform, r.Link = "Codechef", "https://codechef.com/START264" },
		"codechef path":             func(r *Reminder) { r.Platform, r.Link = "Codechef", "https://www.codechef.com/START264/rankings" },
		"codechef too long": func(r *Reminder) {
			r.Platform, r.Link = "Codechef", "https://www.codechef.com/"+strings.Repeat("A", MaxLinkLength)
		},
		"atcoder other page":     func(r *Reminder) { r.Platform, r.Link = "Atcoder", "https://atcoder.jp/users/abc" },
		"atcoder subpage":        func(r *Reminder) { r.Platform, r.Link = "Atcoder", "https://atcoder.jp/contests/abc476/tasks" },
		"leetcode without slash": func(r *Reminder) { r.Platform, r.Link = "LeetCode", "https://leetcode.com/contest/weekly-contest-527" },
		"leetcode capitals":      func(r *Reminder) { r.Platform, r.Link = "LeetCode", "https://leetcode.com/contest/Weekly-527/" },
		"leetcode discuss":       func(r *Reminder) { r.Platform, r.Link = "LeetCode", "https://leetcode.com/discuss/weekly-contest-527/" },
	} {
		t.Run(name, func(t *testing.T) {
			r := valid()
			change(&r)
			if _, err := Parse(r.String()); err == nil {
				t.Errorf("Parse accepted:\n%q", r.String())
			}
		})
	}
}

func TestFreeTextRulesMatchTheSharedCases(t *testing.T) {
	var cases struct {
		Safe   []string `json:"safe"`
		Unsafe []string `json:"unsafe"`
	}
	readTestdata(t, "free-text.json", &cases)
	if len(cases.Safe) == 0 || len(cases.Unsafe) == 0 {
		t.Fatal("testdata/free-text.json has no cases")
	}
	for _, s := range cases.Safe {
		if err := checkText("text", expand(s), MaxTipRunes); err != nil {
			t.Errorf("refused safe %q: %v", s, err)
		}
	}
	for _, s := range cases.Unsafe {
		if err := checkText("text", expand(s), MaxTipRunes); err == nil {
			t.Errorf("accepted unsafe %q", s)
		}
	}
}

func TestEveryRenderedReminderParses(t *testing.T) {
	var texts []string
	readTestdata(t, "reminders.json", &texts)
	if len(texts) == 0 {
		t.Fatal("testdata/reminders.json is empty")
	}
	for i, text := range texts {
		if _, err := Parse(text); err != nil {
			t.Errorf("reminder %d refused: %v\n%s", i, err, text)
		}
	}
}
