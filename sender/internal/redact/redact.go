// Package redact removes WhatsApp identifiers and phone numbers from text
// before it is written to output that may be public, such as workflow logs.
package redact

import (
	"fmt"
	"regexp"

	waLog "go.mau.fi/whatsmeow/util/log"
)

var (
	jid    = regexp.MustCompile(`\b[0-9][0-9.:\-]*@(hosted\.lid|s\.whatsapp\.net|c\.us|g\.us|lid|hosted|newsletter|broadcast|msgr|interop|bot)\b`)
	number = regexp.MustCompile(`\b[0-9]{10,}\b`)
)

// String replaces the user part of every WhatsApp ID with <id>, keeping its
// server, and every standalone run of ten or more digits with <number>.
func String(s string) string {
	s = jid.ReplaceAllString(s, "<id>@$1")
	return number.ReplaceAllString(s, "<number>")
}

// Logger returns a whatsmeow logger that redacts each message with String
// before passing it to next. Sub-loggers redact as well.
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
