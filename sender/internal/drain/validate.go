package drain

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"cp-reminders/sender/internal/outbox"
)

// Limits applied to every claim, and to the messages sent in any 24 hours.
const (
	MaxItems      = 5
	MaxTextRunes  = 1000
	MaxSentPerDay = 8
)

// Header is the first line of every reminder.
const Header = "🚨 Contest Reminder 🚨"

const groupServer = "@g.us"

var (
	// ErrInvalidTarget is returned when ANNOUNCE_JID is not a group JID.
	ErrInvalidTarget = errors.New("ANNOUNCE_JID must be a group JID like 120363...@g.us")
	// ErrDailyLimit marks reminders held back because MaxSentPerDay messages were sent in the last 24 hours.
	ErrDailyLimit = fmt.Errorf("daily limit of %d messages in 24 hours reached", MaxSentPerDay)
)

var (
	allowedHosts = map[string]bool{"codeforces.com": true, "codechef.com": true, "leetcode.com": true, "atcoder.jp": true}
	schemeLink   = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://\S*`)
	bareDomain   = regexp.MustCompile(`(?i)[\p{L}\p{N}-]+(?:\.[\p{L}\p{N}-]+)*\.\p{L}{2,}`)
)

// ValidateItems returns one error per item, nil for items that may be sent.
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
		switch {
		case it.Key == "":
			errs[i] = errors.New("empty key")
		case seen[it.Key]:
			errs[i] = errors.New("duplicate key in claim")
		case strings.TrimSpace(it.Text) == "":
			errs[i] = errors.New("empty text")
		case utf8.RuneCountInString(it.Text) > MaxTextRunes:
			errs[i] = fmt.Errorf("text is %d characters, more than the limit of %d", utf8.RuneCountInString(it.Text), MaxTextRunes)
		default:
			errs[i] = validateText(it.Text)
		}
		seen[it.Key] = true
	}
	return errs
}

func validateText(text string) error {
	if first, _, _ := strings.Cut(text, "\n"); first != Header {
		return errors.New("text does not start with the reminder header")
	}
	for _, link := range schemeLink.FindAllString(text, -1) {
		if err := checkLink(link); err != nil {
			return err
		}
	}
	if domain := bareDomain.FindString(schemeLink.ReplaceAllString(text, " ")); domain != "" {
		return fmt.Errorf("text contains a domain outside a link: %q", domain)
	}
	return nil
}

func checkLink(link string) error {
	u, err := url.Parse(link)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.User != nil {
		return fmt.Errorf("link not allowed: %q", link)
	}
	if host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."); !allowedHosts[host] {
		return fmt.Errorf("link to a host outside the allowlist: %q", u.Hostname())
	}
	return nil
}

// ValidateTarget checks that jid is a group JID.
func ValidateTarget(jid string) error {
	user, ok := strings.CutSuffix(jid, groupServer)
	if !ok || user == "" || strings.Trim(user, "0123456789-") != "" {
		return ErrInvalidTarget
	}
	return nil
}
