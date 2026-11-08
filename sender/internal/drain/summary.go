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
	fmt.Fprintf(bw, "Error: `%s`\n\n", errText(s.Err))
	if len(s.Items) == 0 {
		fmt.Fprint(bw, "No reminders were claimed in this run. Check the Worker's `/health`.\n\n")
	}
	if len(unsent) > 0 {
		fmt.Fprint(bw, "Post these manually in the Announcements group:\n\n")
	}
	for _, i := range unsent {
		it, r := s.Items[i], s.Results[i]
		fmt.Fprintf(bw, "### `%s`\n\n", it.Key)
		fmt.Fprintf(bw, "Contest start %s, deadline %s.", it.StartUTC, it.DeadlineUTC)
		if r.Error != "" {
			fmt.Fprintf(bw, " Reason: %s", redact.String(oneLine(r.Error)))
		}
		fence := fenceFor(it.Text)
		fmt.Fprintf(bw, "\n\n%s\n%s\n%s\n\n", fence, it.Text, fence)
	}
	if len(unacked) > 0 {
		fmt.Fprintf(bw, "Delivered but not acknowledged (%s); the next run acks them from sent.json:\n\n", errText(s.AckErr))
		for _, i := range unacked {
			fmt.Fprintf(bw, "- `%s` (message %s)\n", s.Items[i].Key, s.Results[i].MessageID)
		}
		fmt.Fprintln(bw)
	}
	return bw.Flush()
}

func fenceFor(text string) string {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", max(3, longest+1))
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
