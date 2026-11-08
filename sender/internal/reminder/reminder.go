package reminder

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const Header = "🚨 Contest Reminder 🚨"

const (
	MaxNameRunes = 200
	MaxTipRunes  = 500
)

const (
	platformField = "📍 Platform: "
	nameField     = "🏆 Contest Name: "
	timingField   = "🕒 Timing: "
	durationField = "⏳ Duration: "
	linkField     = "🔗 Contest Link: "
	tipField      = "📌 Tip: "
)

var hosts = map[string]string{
	"Codeforces": "codeforces.com",
	"Codechef":   "codechef.com",
	"Atcoder":    "atcoder.jp",
	"LeetCode":   "leetcode.com",
}

type Reminder struct {
	Platform string
	Name     string
	Timing   string
	Duration string
	Link     string
	Tip      string
}

var (
	timingPattern   = regexp.MustCompile(`^(?:1[0-2]|[1-9]):[0-5][0-9] [AP]M$`)
	durationPattern = regexp.MustCompile(`^[0-9]{1,2}(?::[0-5][0-9])? hours$`)
	linkPathPattern = regexp.MustCompile(`^(?:/[A-Za-z0-9/_.-]*)?$`)
	domainPattern   = regexp.MustCompile(`(?i)[\p{L}\p{N}-]+(?:[.\x{3002}\x{ff0e}\x{ff61}][\p{L}\p{N}-]+)*[.\x{3002}\x{ff0e}\x{ff61}]\p{L}{2,}`)
	ipv4Pattern     = regexp.MustCompile(`[0-9]{1,3}(?:[.\x{3002}\x{ff0e}\x{ff61}][0-9]{1,3}){3}`)
	phonePattern    = regexp.MustCompile(`[0-9](?:[ ()\-.]*[0-9]){9,}`)
)

func (r Reminder) String() string {
	lines := []string{
		Header,
		"",
		platformField + r.Platform,
		nameField + r.Name,
		timingField + r.Timing,
		durationField + r.Duration,
		linkField + r.Link,
	}
	if r.Tip != "" {
		lines = append(lines, "", tipField+r.Tip)
	}
	return strings.Join(lines, "\n")
}

func Parse(text string) (Reminder, error) {
	lines := strings.Split(text, "\n")
	if len(lines) != 7 && len(lines) != 9 {
		return Reminder{}, errors.New("text is not in the reminder layout")
	}
	if lines[0] != Header || lines[1] != "" {
		return Reminder{}, errors.New("text does not start with the reminder header and a blank line")
	}
	var r Reminder
	for i, field := range []struct {
		prefix string
		value  *string
	}{
		{platformField, &r.Platform},
		{nameField, &r.Name},
		{timingField, &r.Timing},
		{durationField, &r.Duration},
		{linkField, &r.Link},
	} {
		value, ok := strings.CutPrefix(lines[2+i], field.prefix)
		if !ok {
			return Reminder{}, fmt.Errorf("line %d is not the %q field", 3+i, strings.TrimSpace(field.prefix))
		}
		*field.value = value
	}
	if len(lines) == 9 {
		tip, ok := strings.CutPrefix(lines[8], tipField)
		if lines[7] != "" || !ok {
			return Reminder{}, errors.New("the last two lines are not a blank line and the tip")
		}
		if tip == "" {
			return Reminder{}, errors.New("tip is empty")
		}
		r.Tip = tip
	}
	if err := r.check(); err != nil {
		return Reminder{}, err
	}
	return r, nil
}

func (r Reminder) check() error {
	host, ok := hosts[r.Platform]
	if !ok {
		return errors.New("unknown platform")
	}
	if err := checkText("contest name", r.Name, MaxNameRunes); err != nil {
		return err
	}
	if !timingPattern.MatchString(r.Timing) {
		return errors.New("timing is not a time like 8:05 PM")
	}
	if !durationPattern.MatchString(r.Duration) {
		return errors.New("duration is not like 2:15 hours")
	}
	if err := checkLink(r.Link, host); err != nil {
		return err
	}
	if r.Tip != "" {
		return checkText("tip", r.Tip, MaxTipRunes)
	}
	return nil
}

func checkLink(link, host string) error {
	rest, ok := strings.CutPrefix(link, "https://")
	if !ok {
		return errors.New("contest link is not an https link")
	}
	path, ok := strings.CutPrefix(strings.TrimPrefix(rest, "www."), host)
	if !ok || !linkPathPattern.MatchString(path) {
		return fmt.Errorf("contest link is not a plain link to %s", host)
	}
	return nil
}

func checkText(field, s string, maxRunes int) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("%s is empty", field)
	}
	if n := utf8.RuneCountInString(s); n > maxRunes {
		return fmt.Errorf("%s is %d characters, more than the limit of %d", field, n, maxRunes)
	}
	for _, r := range s {
		if !unicode.IsGraphic(r) {
			return fmt.Errorf("%s contains a control or format character", field)
		}
	}
	switch {
	case strings.Contains(s, "://"):
		return fmt.Errorf("%s contains a link", field)
	case domainPattern.MatchString(s):
		return fmt.Errorf("%s contains a domain", field)
	case ipv4Pattern.MatchString(s):
		return fmt.Errorf("%s contains an IP address", field)
	case phonePattern.MatchString(s):
		return fmt.Errorf("%s contains a phone number", field)
	}
	return nil
}
