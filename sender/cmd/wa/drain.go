package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"cp-reminders/sender/internal/drain"
	"cp-reminders/sender/internal/outbox"
	"cp-reminders/sender/internal/state"
)

func runDrain(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	d, timeout, err := newDrainer(args, getenv, out)
	if err != nil {
		return err
	}
	return runDrainer(ctx, d, timeout)
}

type releaser interface {
	Release() error
}

func runDrainer(ctx context.Context, d *drain.Drainer, timeout time.Duration) (err error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var opened []releaser
	defer func() {
		for _, r := range opened {
			err = errors.Join(err, r.Release())
		}
	}()
	run := *d
	run.Dial = func(ctx context.Context) (drain.Session, error) {
		s, err := d.Dial(ctx)
		if r, ok := s.(releaser); ok {
			opened = append(opened, r)
		}
		return s, err
	}
	return run.Run(ctx)
}

func newDrainer(args []string, getenv func(string) string, out io.Writer) (*drain.Drainer, time.Duration, error) {
	f := newFlags("drain", out, true)
	jitter := f.Duration("jitter", 90*time.Second, "random delay before connecting, up to this long")
	dryRun := f.Bool("dry-run", false, "connect and verify the target, print texts, but do not send, record or ack")
	timeout := f.Duration("timeout", 4*time.Minute, "limit for the whole run")
	connectTimeout := f.Duration("connect-timeout", 60*time.Second, "limit for connecting to WhatsApp")
	if err := f.parse(args); err != nil {
		return nil, 0, err
	}
	env, err := requireEnv(getenv, envWorkerURL, envSenderToken, envIdentity, envAnnounceJID)
	if err != nil {
		return nil, 0, err
	}
	workerURL, token, identity, target := env[0], env[1], env[2], env[3]
	v, err := newVault(identity)
	if err != nil {
		return nil, 0, err
	}
	ledger, err := state.LoadLedger(filepath.Join(*f.state, state.LedgerFile))
	if err != nil {
		return nil, 0, err
	}

	log := f.logger()
	d := &drain.Drainer{
		Outbox: outbox.New(workerURL, token),
		Dial: func(ctx context.Context) (drain.Session, error) {
			c, err := dial(ctx, *f.state, v, log, *connectTimeout)
			if err != nil {
				return nil, err
			}
			return c, nil
		},
		Ledger: ledger,
		Target: target,
		DryRun: *dryRun,
		Jitter: *jitter,
		Out:    out,
	}
	if path := getenv(envStepSummary); path != "" {
		d.Summary = appendFile(path)
	}
	return d, *timeout, nil
}

type appendFile string

func (p appendFile) Write(b []byte) (int, error) {
	f, err := os.OpenFile(string(p), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	n, err := f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return n, err
}
