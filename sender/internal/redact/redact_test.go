package redact

import (
	"fmt"
	"testing"

	waLog "go.mau.fi/whatsmeow/util/log"
)

func TestString(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"user", "from 919876543210@s.whatsapp.net", "from <id>@s.whatsapp.net"},
		{"user device", "device 919876543210:12@s.whatsapp.net", "device <id>@s.whatsapp.net"},
		{"user agent and device", "agent 919876543210.0:3@s.whatsapp.net", "agent <id>@s.whatsapp.net"},
		{"group", "group 120363012345678901@g.us", "group <id>@g.us"},
		{"legacy group", "group 919876543210-1600000000@g.us", "group <id>@g.us"},
		{"lid", "lid 123456789012345@lid", "lid <id>@lid"},
		{"hosted lid", "lid 123456789012345:7@hosted.lid", "lid <id>@hosted.lid"},
		{"newsletter", "channel 120363000000000001@newsletter", "channel <id>@newsletter"},
		{"several", "a 911111111111@s.whatsapp.net b 120363000000000002@g.us", "a <id>@s.whatsapp.net b <id>@g.us"},
		{"phone number", "call +919876543210 now", "call +<number> now"},
		{"long number", "ts=1762594754", "ts=<number>"},
		{"phone signal address", "no signal session established with 919876543210:5", "no signal session established with <number>:5"},
		{"lid signal address", "no session found for user 123456789012345_1:5", "no session found for user <number>:5"},
		{"hosted signal address", "no signal session established with 123456789012345_128:2", "no signal session established with <number>:2"},
		{"hosted lid signal address", "with 123456789012345_129:2.", "with <number>:2."},
		{
			"group decrypt failure",
			"Error decrypting message 3EB0DD5652A4740D961284 from 120363012345678901@g.us: failed to decrypt group message: no sender key for 123456789012345_1:5 in 120363012345678901@g.us",
			"Error decrypting message 3EB0DD5652A4740D961284 from <id>@g.us: failed to decrypt group message: no sender key for <number>:5 in <id>@g.us",
		},
		{"client version", "Client outdated (405) connect failure (client version: 2.3000.1052556849)", "Client outdated (405) connect failure (client version: 2.3000.1052556849)"},
		{"struct field", "{User:919876543210 Server:s.whatsapp.net}", "{User:<number> Server:s.whatsapp.net}"},
		{"message id", "message 3EB0DD5652A4740D961284 sent", "message 3EB0DD5652A4740D961284 sent"},
		{"contest link", "https://codeforces.com/contests/2267", "https://codeforces.com/contests/2267"},
		{"reminder key", "key 102|https://leetcode.com/contest/weekly-contest-527/", "key 102|https://leetcode.com/contest/weekly-contest-527/"},
		{"timestamp", "at 2026-11-08T09:19:14Z", "at 2026-11-08T09:19:14Z"},
		{"short number", "acked 5 result(s) in 123456789 ns", "acked 5 result(s) in 123456789 ns"},
		{"status broadcast", "status@broadcast", "status@broadcast"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := String(tt.in); got != tt.want {
				t.Errorf("String(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

type capture struct {
	lines *[]string
	mod   string
}

func (c capture) add(level, msg string, args []any) {
	*c.lines = append(*c.lines, level+" "+c.mod+" "+fmt.Sprintf(msg, args...))
}

func (c capture) Errorf(msg string, args ...any) { c.add("ERROR", msg, args) }
func (c capture) Warnf(msg string, args ...any)  { c.add("WARN", msg, args) }
func (c capture) Infof(msg string, args ...any)  { c.add("INFO", msg, args) }
func (c capture) Debugf(msg string, args ...any) { c.add("DEBUG", msg, args) }
func (c capture) Sub(mod string) waLog.Logger    { return capture{lines: c.lines, mod: c.mod + "/" + mod} }

func TestLoggerRedactsEveryLevelAndSubLogger(t *testing.T) {
	var lines []string
	log := Logger(capture{lines: &lines, mod: "wa"})

	log.Errorf("decrypt %s from %s", "3EB0", "919876543210@s.whatsapp.net")
	log.Sub("Client").Warnf("retry receipt for %s", "120363012345678901@g.us")
	log.Sub("Client").Sub("Recv").Infof("%d%% done for %s", 50, "123456789012345@lid")
	log.Debugf("phone %s", "919876543210")

	wantLines(t, lines,
		"ERROR wa decrypt 3EB0 from <id>@s.whatsapp.net",
		"WARN wa/Client retry receipt for <id>@g.us",
		"INFO wa/Client/Recv 50% done for <id>@lid",
		"DEBUG wa phone <number>",
	)
}

func TestSignalLoggerRedactsAndForwardsEveryLevel(t *testing.T) {
	var lines []string
	log := Signal(capture{lines: &lines, mod: "wa/Signal"})

	log.Error("SessionCipher.go:300", "no session found for user 123456789012345_1:5")
	log.Warning("SessionCipher.go:263", "invalid message from 919876543210:3")
	log.Info("GroupCipher.go:86", "100% of 3")
	log.Debug("SessionBuilder.go:50", "processing 123456789012345_128:2")
	log.Configure("all")

	wantLines(t, lines,
		"ERROR wa/Signal SessionCipher.go:300: no session found for user <number>:5",
		"WARN wa/Signal SessionCipher.go:263: invalid message from <number>:3",
		"INFO wa/Signal GroupCipher.go:86: 100% of 3",
		"DEBUG wa/Signal SessionBuilder.go:50: processing <number>:2",
	)
}

func wantLines(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("lines: got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
}
