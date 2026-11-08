package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"go.mau.fi/whatsmeow/types"

	"cp-reminders/sender/internal/wa"
)

func runGroups(ctx context.Context, args []string, getenv func(string) string, out io.Writer) (err error) {
	f := newFlags("groups", out, true)
	timeout := f.Duration("timeout", 90*time.Second, "limit for the whole run")
	connectTimeout := f.Duration("connect-timeout", 60*time.Second, "limit for connecting to WhatsApp")
	if err := f.parse(args); err != nil {
		return err
	}
	v, err := vaultFromEnv(getenv)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	c, err := dial(ctx, *f.state, v, f.logger(), *connectTimeout)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, c.Close(), c.Release()) }()

	groups, err := c.cli.GetJoinedGroups(ctx)
	if err != nil {
		return fmt.Errorf("get joined groups: %w", err)
	}
	self := c.cli.SelfJIDs()
	fmt.Fprintf(out, "logged in as %s\n\n", joinJIDs(self))
	return writeGroupTable(out, groups, self)
}

func writeGroupTable(w io.Writer, groups []*types.GroupInfo, self []types.JID) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "JID\tNAME\tPARENT\tDEFAULT_SUB\tLINKED_PARENT\tANNOUNCE\tADMIN")
	var candidates []string
	for _, g := range groups {
		admin := wa.IsAdmin(g, self...)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			g.JID, cell(g.Name), yesNo(g.IsParent), yesNo(g.IsDefaultSubGroup),
			jidOrDash(g.LinkedParentJID), yesNo(g.IsAnnounce), yesNo(admin))
		if g.IsDefaultSubGroup {
			candidates = append(candidates, fmt.Sprintf("ANNOUNCEMENTS CANDIDATE: %s name=%q community=%s admin=%s",
				g.JID, cell(g.Name), jidOrDash(g.LinkedParentJID), yesNo(admin)))
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(candidates) > 0 {
		fmt.Fprintln(w)
	}
	for _, line := range candidates {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

func cell(s string) string {
	if s = strings.Join(strings.Fields(s), " "); s == "" {
		return "-"
	}
	return s
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func jidOrDash(j types.JID) string {
	if j.IsEmpty() {
		return "-"
	}
	return j.String()
}

func joinJIDs(ids []types.JID) string {
	s := make([]string, len(ids))
	for i, j := range ids {
		s[i] = j.String()
	}
	return strings.Join(s, " / ")
}
