package drain

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"cp-reminders/sender/internal/outbox"
	"cp-reminders/sender/internal/reminder"
)

const (
	MaxItems      = 5
	MaxTextRunes  = 1000
	MaxSentPerDay = 8
)

const groupServer = "@g.us"

var (
	ErrInvalidTarget = errors.New("ANNOUNCE_JID must be a group JID like 120363...@g.us")
	ErrDailyLimit    = fmt.Errorf("daily limit of %d messages in 24 hours reached", MaxSentPerDay)
)

func ValidateItems(items []outbox.Item) []error {
	errs := make([]error, len(items))
	if len(items) > MaxItems {
		err := fmt.Errorf("claim returned %d items, more than the limit of %d", len(items), MaxItems)
		for i := range errs {
			errs[i] = err
		}
		return errs
	}
	seen := make(map[string]bool, len(items))
	for i, it := range items {
		errs[i] = validateItem(it, seen[it.Key])
		seen[it.Key] = true
	}
	return errs
}

func validateItem(it outbox.Item, duplicate bool) error {
	switch {
	case it.Key == "":
		return errors.New("empty key")
	case duplicate:
		return errors.New("duplicate key in claim")
	case strings.TrimSpace(it.Text) == "":
		return errors.New("empty text")
	case utf8.RuneCountInString(it.Text) > MaxTextRunes:
		return fmt.Errorf("text is %d characters, more than the limit of %d", utf8.RuneCountInString(it.Text), MaxTextRunes)
	}
	_, err := reminder.Parse(it.Text)
	return err
}

func ValidateTarget(jid string) error {
	user, ok := strings.CutSuffix(jid, groupServer)
	if !ok || user == "" || strings.Trim(user, "0123456789-") != "" {
		return ErrInvalidTarget
	}
	return nil
}
