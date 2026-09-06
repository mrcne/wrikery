package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var weekdays = map[string]time.Weekday{"mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday, "sun": time.Sunday}

// parseDate accepts what people type in a hurry: an ISO date, today, tomorrow, yesterday, a weekday (the next one),
// +Nd or -Nd, and "12 sep" for a day this year. Empty stays empty so a field can be cleared.
func parseDate(s string, now time.Time) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch s {
	case "":
		return "", nil
	case "today":
		return today.Format("2006-01-02"), nil
	case "tomorrow":
		return today.AddDate(0, 0, 1).Format("2006-01-02"), nil
	case "yesterday":
		return today.AddDate(0, 0, -1).Format("2006-01-02"), nil
	}
	if wd, ok := weekdays[s]; ok {
		days := (int(wd) - int(today.Weekday()) + 7) % 7
		if days == 0 {
			days = 7
		}
		return today.AddDate(0, 0, days).Format("2006-01-02"), nil
	}
	if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") {
		n, err := strconv.Atoi(strings.TrimSuffix(s[1:], "d"))
		if err != nil {
			return "", fmt.Errorf("expected +Nd or -Nd, got %q", s)
		}
		if s[0] == '-' {
			n = -n
		}
		return today.AddDate(0, 0, n).Format("2006-01-02"), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Format("2006-01-02"), nil
	}
	// The layout token is "Jan", matched case insensitively by time.Parse, but a lowercase
	// "jan" in the layout string itself is not a token at all, so the input's month has to be
	// title cased before parsing "12 sep" against the layout "2 Jan".
	if parts := strings.SplitN(s, " ", 2); len(parts) == 2 && parts[1] != "" {
		titled := parts[0] + " " + strings.ToUpper(parts[1][:1]) + parts[1][1:]
		if t, err := time.Parse("2 Jan", titled); err == nil {
			return time.Date(today.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Format("2006-01-02"), nil
		}
	}
	return "", errors.New("could not read the date, try 2026-09-12, fri, +3d or today")
}
