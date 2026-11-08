package drain

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand/v2"
	"time"

	"cp-reminders/sender/internal/outbox"
	"cp-reminders/sender/internal/redact"
	"cp-reminders/sender/internal/state"
)

const ackTimeout = time.Minute

var (
	ErrNotAnnouncements = errors.New("target is not a community announcements group")
	ErrNotAdmin         = errors.New("sender is not an admin of the target group")

	errNotAttempted = errors.New("not attempted: an earlier step failed")
)

type Outbox interface {
	Claim(ctx context.Context, claimID string) ([]outbox.Item, error)
	Ack(ctx context.Context, req outbox.AckRequest) error
}

type Group struct {
	IsAnnouncements bool
	IsAdmin         bool
}

type Session interface {
	Group(ctx context.Context, jid string) (Group, error)
	SendText(ctx context.Context, jid, text string) (messageID string, err error)
	Close() error
}

type Dialer func(ctx context.Context) (Session, error)

type Ledger interface {
	Lookup(key string) (state.Entry, bool)
	SentSince(t time.Time) int
	Record(e state.Entry) error
}

type Drainer struct {
	Outbox       Outbox
	Dial         Dialer
	Ledger       Ledger
	Target       string
	DryRun       bool
	Jitter       time.Duration
	Out          io.Writer
	Summary      io.Writer
	Now          func() time.Time
	Sleep        func(ctx context.Context, d time.Duration) error
	RandDuration func(max time.Duration) time.Duration
	NewClaimID   func() string
}

func (d *Drainer) Run(ctx context.Context) error {
	if err := ValidateTarget(d.Target); err != nil {
		return err
	}
	claimID := d.newClaimID()
	items, err := d.Outbox.Claim(ctx, claimID)
	if err != nil {
		err = fmt.Errorf("claim: %w", err)
		d.report(summary{Err: err})
		return err
	}
	if len(items) == 0 {
		d.logf("outbox empty")
		return nil
	}
	d.logf("claimed %d reminder(s) under claim %s", len(items), claimID)

	results := make([]outbox.Result, len(items))
	var pending []int
	var failures []error
	invalid := ValidateItems(items)
	for i, verr := range invalid {
		key := items[i].Key
		if verr != nil {
			results[i] = outbox.Result{Key: key, Status: outbox.StatusRejected, Error: oneLine(verr.Error())}
			failures = append(failures, fmt.Errorf("invalid item %q: %w", key, verr))
			continue
		}
		if e, ok := d.Ledger.Lookup(key); ok {
			results[i] = outbox.Result{Key: key, Status: outbox.StatusSent, MessageID: e.MessageID}
			d.logf("already sent %s (message %s)", key, e.MessageID)
			continue
		}
		pending = append(pending, i)
	}
	budget := max(0, MaxSentPerDay-d.Ledger.SentSince(d.now().Add(-24*time.Hour)))
	if len(pending) > budget {
		for _, i := range pending[budget:] {
			results[i] = failedResult(items[i].Key, ErrDailyLimit)
		}
		failures = append(failures, fmt.Errorf("%d reminder(s) held back: %w", len(pending)-budget, ErrDailyLimit))
		pending = pending[:budget]
	}
	if len(pending) > 0 {
		if err := d.deliver(ctx, items, results, pending); err != nil {
			failures = append(failures, err)
		}
	}
	if d.DryRun {
		d.logf("dry run: not acking; the claim lease expires and the items return to the queue")
		return errors.Join(failures...)
	}

	ackErr := d.ack(ctx, claimID, results)
	if ackErr != nil {
		failures = append(failures, ackErr)
	}
	runErr := errors.Join(failures...)
	if runErr != nil {
		d.report(summary{Err: runErr, Items: items, Results: results, Invalid: invalid, AckErr: ackErr})
	}
	return runErr
}

func (d *Drainer) deliver(ctx context.Context, items []outbox.Item, results []outbox.Result, pending []int) (err error) {
	failFrom := func(n int, cause error) {
		for _, i := range pending[n:] {
			results[i] = failedResult(items[i].Key, cause)
		}
	}
	if err := d.jitter(ctx); err != nil {
		err = fmt.Errorf("jitter: %w", err)
		failFrom(0, err)
		return err
	}
	sess, err := d.Dial(ctx)
	if err != nil {
		err = fmt.Errorf("connect: %w", err)
		failFrom(0, err)
		return err
	}
	defer func() {
		if cerr := sess.Close(); cerr != nil {
			err = errors.Join(err, fmt.Errorf("close session: %w", cerr))
		}
	}()
	if err := d.checkTarget(ctx, sess); err != nil {
		failFrom(0, err)
		return err
	}
	for n, i := range pending {
		it := items[i]
		if d.DryRun {
			d.logf("dry run: would send %s:", it.Key)
			d.printText(it.Text)
			continue
		}
		id, err := sess.SendText(ctx, d.Target, it.Text)
		if err != nil {
			err = fmt.Errorf("send %s: %w", it.Key, err)
			results[i] = failedResult(it.Key, err)
			failFrom(n+1, errNotAttempted)
			return err
		}
		results[i] = outbox.Result{Key: it.Key, Status: outbox.StatusSent, MessageID: id}
		d.logf("sent %s (message %s)", it.Key, id)
		if err := d.Ledger.Record(state.Entry{Key: it.Key, SentAt: d.now().UTC(), MessageID: id}); err != nil {
			failFrom(n+1, errNotAttempted)
			return fmt.Errorf("after sending %s: %w", it.Key, err)
		}
	}
	return nil
}

func (d *Drainer) checkTarget(ctx context.Context, sess Session) error {
	g, err := sess.Group(ctx, d.Target)
	if err != nil {
		return fmt.Errorf("look up target group: %w", err)
	}
	if !g.IsAnnouncements {
		return ErrNotAnnouncements
	}
	if !g.IsAdmin {
		return ErrNotAdmin
	}
	d.logf("target verified: announcements group, sender is admin")
	return nil
}

func (d *Drainer) ack(ctx context.Context, claimID string, results []outbox.Result) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ackTimeout)
	defer cancel()
	if err := d.Outbox.Ack(ctx, outbox.AckRequest{ClaimID: claimID, Results: results}); err != nil {
		return fmt.Errorf("ack: %w", err)
	}
	d.logf("acked %d result(s)", len(results))
	return nil
}

func (d *Drainer) jitter(ctx context.Context) error {
	if d.Jitter <= 0 {
		return nil
	}
	wait := d.randDuration(d.Jitter)
	d.logf("waiting %s before connecting", wait.Round(time.Second))
	return d.sleep(ctx, wait)
}

func (d *Drainer) report(s summary) {
	if d.Summary == nil {
		return
	}
	if err := writeSummary(d.Summary, s); err != nil {
		d.logf("write step summary: %v", err)
	}
}

func (d *Drainer) logf(format string, args ...any) {
	if d.Out != nil {
		fmt.Fprintln(d.Out, redact.String(fmt.Sprintf(format, args...)))
	}
}

func (d *Drainer) printText(text string) {
	if d.Out != nil {
		fmt.Fprintf(d.Out, "%s\n\n", text)
	}
}

func (d *Drainer) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *Drainer) sleep(ctx context.Context, wait time.Duration) error {
	if d.Sleep != nil {
		return d.Sleep(ctx, wait)
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (d *Drainer) randDuration(max time.Duration) time.Duration {
	if d.RandDuration != nil {
		return d.RandDuration(max)
	}
	return mathrand.N(max + 1)
}

func (d *Drainer) newClaimID() string {
	if d.NewClaimID != nil {
		return d.NewClaimID()
	}
	return rand.Text()
}

func failedResult(key string, err error) outbox.Result {
	return outbox.Result{Key: key, Status: outbox.StatusFailed, Error: oneLine(err.Error())}
}
