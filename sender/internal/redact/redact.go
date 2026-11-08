package redact

import (
	"fmt"
	"regexp"

	signallog "go.mau.fi/libsignal/logger"
	waLog "go.mau.fi/whatsmeow/util/log"
)

var (
	jid    = regexp.MustCompile(`\b[0-9][0-9.:\-]*@(hosted\.lid|s\.whatsapp\.net|c\.us|g\.us|lid|hosted|newsletter|broadcast|msgr|interop|bot)\b`)
	number = regexp.MustCompile(`(^|[^0-9A-Za-z_.])[0-9]{10,}(?:_[0-9]{1,3})?\b`)
)

func String(s string) string {
	s = jid.ReplaceAllString(s, "<id>@$1")
	return number.ReplaceAllString(s, "${1}<number>")
}

func Logger(next waLog.Logger) waLog.Logger {
	return logger{next: next}
}

type logger struct {
	next waLog.Logger
}

func (l logger) Errorf(msg string, args ...any) {
	l.next.Errorf("%s", String(fmt.Sprintf(msg, args...)))
}

func (l logger) Warnf(msg string, args ...any) {
	l.next.Warnf("%s", String(fmt.Sprintf(msg, args...)))
}

func (l logger) Infof(msg string, args ...any) {
	l.next.Infof("%s", String(fmt.Sprintf(msg, args...)))
}

func (l logger) Debugf(msg string, args ...any) {
	l.next.Debugf("%s", String(fmt.Sprintf(msg, args...)))
}

func (l logger) Sub(module string) waLog.Logger {
	return logger{next: l.next.Sub(module)}
}

func Signal(next waLog.Logger) signallog.Loggable {
	return signal{next: Logger(next)}
}

type signal struct {
	next waLog.Logger
}

func (s signal) Error(caller, msg string)   { s.next.Errorf("%s: %s", caller, msg) }
func (s signal) Warning(caller, msg string) { s.next.Warnf("%s: %s", caller, msg) }
func (s signal) Info(caller, msg string)    { s.next.Infof("%s: %s", caller, msg) }
func (s signal) Debug(string, string)       {}
func (s signal) Configure(string)           {}
