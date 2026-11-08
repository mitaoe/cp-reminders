// Command wa posts contest reminders to a WhatsApp community announcements group.
//
// Usage:
//
//	wa keygen
//	wa pair   --state DIR [--phone 91XXXXXXXXXX] [--force]
//	wa groups --state DIR
//	wa drain  --state DIR [--jitter 90s] [--dry-run]
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	signallog "go.mau.fi/libsignal/logger"
	waLog "go.mau.fi/whatsmeow/util/log"

	"cp-reminders/sender/internal/redact"
	"cp-reminders/sender/internal/state"
)

const (
	envIdentity    = "AGE_IDENTITY"
	envAnnounceJID = "ANNOUNCE_JID"
	envWorkerURL   = "WORKER_URL"
	envSenderToken = "SENDER_TOKEN"
	envStepSummary = "GITHUB_STEP_SUMMARY"
)

var errUsage = errors.New("usage")

type command struct {
	name    string
	summary string
	run     func(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error
}

var commands = []command{
	{"keygen", "print a new age identity and its recipient", runKeygen},
	{"pair", "link this sender as a new device (QR or pairing code)", runPair},
	{"groups", "list joined groups and announcements candidates", runGroups},
	{"drain", "claim queued reminders, send them, ack the results", runDrain},
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Getenv, os.Stdout)
	stop()
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
	case errors.Is(err, errUsage):
		fmt.Fprintln(os.Stderr, errorLine(err))
		os.Exit(2)
	default:
		fmt.Fprintln(os.Stderr, errorLine(err))
		os.Exit(1)
	}
}

func errorLine(err error) string {
	return "wa: " + redact.String(err.Error())
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	if len(args) == 0 {
		usage(out)
		return fmt.Errorf("%w: missing command", errUsage)
	}
	for _, c := range commands {
		if c.name == args[0] {
			return c.run(ctx, args[1:], getenv, out)
		}
	}
	switch args[0] {
	case "help", "-h", "--help":
		usage(out)
		return nil
	}
	return fmt.Errorf("%w: unknown command %q", errUsage, args[0])
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: wa <command> [flags]")
	for _, c := range commands {
		fmt.Fprintf(w, "  %-7s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(w, "Run `wa <command> -h` for flags. See docs/sender.md.")
}

type flags struct {
	*flag.FlagSet
	state    *string
	logLevel *string
}

func newFlags(name string, out io.Writer, withState bool) *flags {
	fs := flag.NewFlagSet("wa "+name, flag.ContinueOnError)
	fs.SetOutput(out)
	f := &flags{FlagSet: fs}
	if withState {
		f.state = fs.String("state", "", "state directory holding "+state.SessionFile+" and "+state.LedgerFile+" (required)")
		f.logLevel = fs.String("log-level", "WARN", "whatsmeow and libsignal log level: DEBUG, INFO, WARN or ERROR")
	}
	return f
}

func (f *flags) parse(args []string) error {
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if f.NArg() > 0 {
		return fmt.Errorf("%w: unexpected arguments %q", errUsage, f.Args())
	}
	if f.state != nil && *f.state == "" {
		return fmt.Errorf("%w: --state is required", errUsage)
	}
	return nil
}

func (f *flags) logger() waLog.Logger {
	base := waLog.Stdout("wa", strings.ToUpper(*f.logLevel), false)
	signallog.Logger = redact.Signal(base.Sub("Signal"))
	return redact.Logger(base)
}

func requireEnv(getenv func(string) string, names ...string) ([]string, error) {
	values := make([]string, len(names))
	var missing []string
	for i, n := range names {
		values[i] = strings.TrimSpace(getenv(n))
		if values[i] == "" {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing environment variables: %s", strings.Join(missing, ", "))
	}
	return values, nil
}

func vaultFromEnv(getenv func(string) string) (*state.Vault, error) {
	vals, err := requireEnv(getenv, envIdentity)
	if err != nil {
		return nil, err
	}
	return newVault(vals[0])
}

func newVault(identity string) (*state.Vault, error) {
	v, err := state.NewVault(identity)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envIdentity, err)
	}
	return v, nil
}
