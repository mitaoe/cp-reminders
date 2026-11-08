package drain

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode"

	"cp-reminders/sender/internal/outbox"
	"cp-reminders/sender/internal/redact"
)

const (
	maxCodeRunes    = 200
	maxErrorRunes   = 1000
	maxBacktickRun  = 15
	maxFenceRun     = 63
	escapedBacktick = "\\" + "u0060"
	notValidated    = "not validated"
)

type summary struct {
	Err     error
	Items   []outbox.Item
	Results []outbox.Result
	Invalid []error
	AckErr  error
}

func writeSummary(w io.Writer, s summary) error {
	bw := bufio.NewWriter(w)
	var manual, refused, unacked []int
	for i, r := range s.Results {
		switch {
		case r.Status == outbox.StatusSent:
			if s.AckErr != nil {
				unacked = append(unacked, i)
			}
		case i < len(s.Invalid) && s.Invalid[i] == nil:
			manual = append(manual, i)
		default:
			refused = append(refused, i)
		}
	}

	fmt.Fprintf(bw, "## WhatsApp sender failed: %d reminder(s) not delivered\n\n", len(manual)+len(refused))
	fmt.Fprintf(bw, "Error: %s\n\n", errorCode(errText(s.Err)))
	if len(s.Items) == 0 {
		fmt.Fprint(bw, "No reminders were claimed in this run. Check the Worker's `/health`.\n\n")
	}
	if len(manual) > 0 {
		fmt.Fprint(bw, "Post these manually in the Announcements group:\n\n")
		for _, i := range listed(manual) {
			writeManual(bw, s.Items[i], s.Results[i])
		}
		writeNotListed(bw, len(manual))
	}
	if len(refused) > 0 {
		fmt.Fprint(bw, "Refused by validation; not posted, and their texts are not shown:\n\n")
		for _, i := range listed(refused) {
			reason := notValidated
			if i < len(s.Invalid) {
				reason = errText(s.Invalid[i])
			}
			fmt.Fprintf(bw, "- %s: %s\n", code(s.Items[i].Key), code(reason))
		}
		fmt.Fprintln(bw)
		writeNotListed(bw, len(refused))
	}
	if len(unacked) > 0 {
		fmt.Fprintf(bw, "Delivered but not acknowledged (%s); the next run acks them from sent.json:\n\n", errorCode(errText(s.AckErr)))
		for _, i := range listed(unacked) {
			fmt.Fprintf(bw, "- %s (message %s)\n", code(s.Items[i].Key), code(s.Results[i].MessageID))
		}
		fmt.Fprintln(bw)
		writeNotListed(bw, len(unacked))
	}
	return bw.Flush()
}

func writeManual(w io.Writer, it outbox.Item, r outbox.Result) {
	fmt.Fprintf(w, "### %s\n\n", code(it.Key))
	fmt.Fprintf(w, "Contest start %s, deadline %s.", code(it.StartUTC), code(it.DeadlineUTC))
	if r.Error != "" {
		fmt.Fprintf(w, " Reason: %s", code(redact.String(oneLine(r.Error))))
	}
	backticks, tildes := longestRun(it.Text, '`'), longestRun(it.Text, '~')
	switch {
	case backticks > maxFenceRun:
		fmt.Fprintf(w, "\n\nReminder text not shown: it contains a run of %d backticks.\n\n", backticks)
	case tildes > maxFenceRun:
		fmt.Fprintf(w, "\n\nReminder text not shown: it contains a run of %d tildes.\n\n", tildes)
	default:
		fence := fenceFor(it.Text)
		fmt.Fprintf(w, "\n\n%s\n%s\n%s\n\n", fence, it.Text, fence)
	}
}

func listed(indices []int) []int {
	return indices[:min(len(indices), MaxItems)]
}

func writeNotListed(w io.Writer, n int) {
	if n > MaxItems {
		fmt.Fprintf(w, "%d more not listed.\n\n", n-MaxItems)
	}
}

func fenceFor(text string) string {
	return strings.Repeat("`", max(3, longestRun(text, '`')+1))
}

func code(s string) string {
	span, _ := codeSpan(s, maxCodeRunes)
	return span
}

func errorCode(s string) string {
	span, shortened := codeSpan(s, maxErrorRunes)
	if shortened {
		return span + " (full error in the run log)"
	}
	return span
}

func codeSpan(s string, limit int) (string, bool) {
	s = oneLine(s)
	if s == "" {
		return "` `", false
	}
	shortened := false
	if r := []rune(s); len(r) > limit {
		s = string(r[:limit]) + "…"
		shortened = true
	}
	s = escapeControls(s)
	if longestRun(s, '`') > maxBacktickRun {
		s = strings.ReplaceAll(s, "`", escapedBacktick)
	}
	delim := strings.Repeat("`", longestRun(s, '`')+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		s = " " + s + " "
	}
	return delim + s + delim, shortened
}

func escapeControls(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsGraphic(r):
			b.WriteRune(r)
		case r > 0xFFFF:
			fmt.Fprintf(&b, `\U%08X`, r)
		default:
			fmt.Fprintf(&b, `\u%04X`, r)
		}
	}
	return b.String()
}

func longestRun(s string, c rune) int {
	longest, run := 0, 0
	for _, r := range s {
		if r == c {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return redact.String(oneLine(err.Error()))
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
