package drain

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"cp-reminders/sender/internal/outbox"
	"cp-reminders/sender/internal/redact"
)

type summary struct {
	Err     error
	Items   []outbox.Item
	Results []outbox.Result
	AckErr  error
}

func writeSummary(w io.Writer, s summary) error {
	bw := bufio.NewWriter(w)
	var unsent, unacked []int
	for i, r := range s.Results {
		if r.Status == outbox.StatusSent {
			unacked = append(unacked, i)
		} else {
			unsent = append(unsent, i)
		}
	}
	if s.AckErr == nil {
		unacked = nil
	}

	fmt.Fprintf(bw, "## WhatsApp sender failed: %d reminder(s) not delivered\n\n", len(unsent))
	fmt.Fprintf(bw, "Error: %s\n\n", code(errText(s.Err)))
	if len(s.Items) == 0 {
		fmt.Fprint(bw, "No reminders were claimed in this run. Check the Worker's `/health`.\n\n")
	}
	if len(unsent) > 0 {
		fmt.Fprint(bw, "Post these manually in the Announcements group:\n\n")
	}
	for _, i := range unsent {
		it, r := s.Items[i], s.Results[i]
		fmt.Fprintf(bw, "### %s\n\n", code(it.Key))
		fmt.Fprintf(bw, "Contest start %s, deadline %s.", code(it.StartUTC), code(it.DeadlineUTC))
		if r.Error != "" {
			fmt.Fprintf(bw, " Reason: %s", code(redact.String(oneLine(r.Error))))
		}
		fence := fenceFor(it.Text)
		fmt.Fprintf(bw, "\n\n%s\n%s\n%s\n\n", fence, it.Text, fence)
	}
	if len(unacked) > 0 {
		fmt.Fprintf(bw, "Delivered but not acknowledged (%s); the next run acks them from sent.json:\n\n", code(errText(s.AckErr)))
		for _, i := range unacked {
			fmt.Fprintf(bw, "- %s (message %s)\n", code(s.Items[i].Key), code(s.Results[i].MessageID))
		}
		fmt.Fprintln(bw)
	}
	return bw.Flush()
}

func fenceFor(text string) string {
	return strings.Repeat("`", max(3, longestBacktickRun(text)+1))
}

func code(s string) string {
	s = oneLine(s)
	if s == "" {
		return "` `"
	}
	delim := strings.Repeat("`", longestBacktickRun(s)+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		s = " " + s + " "
	}
	return delim + s + delim
}

func longestBacktickRun(s string) int {
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
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
