package ui

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// A bare weekday name points ahead for a due date and back for a time entry, the caller says which.
// Today never counts: "thu" typed on a Thursday is a week ahead or a week back, the same as "next thu" and "last thu".
type weekdayRule int

const (
	nextWeekday weekdayRule = iota
	lastWeekday
)

// parseDate accepts what people type in a hurry: an ISO date, today, tomorrow, yesterday or yday,
// a weekday in full or in three letters, alone or after last or next, +Nd or -Nd, and "12 sep" for a day this year.
// Empty stays empty so a field can be cleared.
func parseDate(s string, now time.Time, bare weekdayRule) (string, error) {
	s = strings.Join(strings.Fields(strings.ToLower(s)), " ")
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch s {
	case "":
		return "", nil
	case "today":
		return today.Format("2006-01-02"), nil
	case "tomorrow":
		return today.AddDate(0, 0, 1).Format("2006-01-02"), nil
	case "yesterday", "yday":
		return today.AddDate(0, 0, -1).Format("2006-01-02"), nil
	}
	if wd, ok := weekdayNamed(s); ok {
		return weekdayFrom(today, wd, bare).Format("2006-01-02"), nil
	}
	if word, name, ok := strings.Cut(s, " "); ok && (word == "last" || word == "next") {
		wd, ok := weekdayNamed(name)
		if !ok {
			return "", fmt.Errorf("expected a weekday after %s, got %q", word, name)
		}
		rule := nextWeekday
		if word == "last" {
			rule = lastWeekday
		}
		return weekdayFrom(today, wd, rule).Format("2006-01-02"), nil
	}
	if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") {
		n, err := strconv.Atoi(strings.TrimSuffix(s[1:], "d"))
		if err != nil || !strings.HasSuffix(s, "d") {
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
	return "", errors.New("could not read the date, try 2026-09-12, today, yday, fri, last fri, next fri, +3d or 12 sep")
}

// weekdayNamed reads a weekday written in full or in its first three letters.
func weekdayNamed(s string) (time.Weekday, bool) {
	for wd := time.Sunday; wd <= time.Saturday; wd++ {
		name := strings.ToLower(wd.String())
		if s == name || s == name[:3] {
			return wd, true
		}
	}
	return 0, false
}

// weekdayFrom is the nearest day of that weekday after today, or before it under lastWeekday.
func weekdayFrom(today time.Time, wd time.Weekday, rule weekdayRule) time.Time {
	diff := int(wd) - int(today.Weekday())
	switch {
	case rule == lastWeekday && diff >= 0:
		diff -= 7
	case rule == nextWeekday && diff <= 0:
		diff += 7
	}
	return today.AddDate(0, 0, diff)
}

// parseHours reads 1.5, 1,5, 1:30, 90m, 2h, 2h30m and 1h 20m, the form the app prints logged time in.
// Wrike stores hours as a decimal, the API rejects zero and negative values.
func parseHours(s string) (float64, error) {
	s = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), ",", "."), " ", ""))
	if s == "" {
		return 0, errors.New("hours are required")
	}
	var hours float64
	switch {
	case strings.Contains(s, ":"):
		parts := strings.SplitN(s, ":", 2)
		h, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || m < 0 || m >= 60 {
			return 0, fmt.Errorf("could not read %q as h:mm", s)
		}
		hours = float64(h) + float64(m)/60
	case strings.HasSuffix(s, "m") || strings.Contains(s, "h"):
		rest := s
		if i := strings.Index(rest, "h"); i >= 0 {
			h, err := strconv.ParseFloat(rest[:i], 64)
			if err != nil {
				return 0, fmt.Errorf("could not read %q", s)
			}
			hours += h
			rest = rest[i+1:]
		}
		if rest != "" {
			m, err := strconv.Atoi(strings.TrimSuffix(rest, "m"))
			if err != nil {
				return 0, fmt.Errorf("could not read %q", s)
			}
			hours += float64(m) / 60
		}
	default:
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("could not read %q as hours", s)
		}
		hours = v
	}
	if hours <= 0 || hours > 24 {
		return 0, errors.New("hours must be between 0 and 24")
	}
	return hours, nil
}

// formatHours prefills the hours field the way a person would type it:
// a plain integer for a whole number of hours, h:mm otherwise.
// Minutes are rounded rather than truncated,
// so the result round trips back through parseHours instead of drifting a minute short on repeated edits.
func formatHours(h float64) string {
	hours, minutes := splitHours(h)
	if minutes == 0 {
		return strconv.Itoa(hours)
	}
	return fmt.Sprintf("%d:%02d", hours, minutes)
}

// splitHours rounds a decimal number of hours to whole hours and minutes, 60 minutes carrying into the hour.
func splitHours(h float64) (hours, minutes int) {
	whole := math.Trunc(h)
	minutes = int(math.Round((h - whole) * 60))
	if minutes == 60 {
		whole++
		minutes = 0
	}
	return int(whole), minutes
}

// hoursText writes logged time the way the time entry box reads it: 2h, 45m or 1h 20m.
// Exact to the minute, so what is shown can be checked against what was typed.
func hoursText(h float64) string {
	hours, minutes := splitHours(h)
	switch {
	case minutes == 0:
		return fmt.Sprintf("%dh", hours)
	case hours == 0:
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

// hoursClock writes logged time as h:mm for the timesheet grid.
// A cell is seven characters wide with no gap of its own, 1h 20m with the pending mark would fill it,
// and in a column the colons line up where 2h, 45m and 1h 20m would not.
func hoursClock(h float64) string {
	hours, minutes := splitHours(h)
	return fmt.Sprintf("%d:%02d", hours, minutes)
}
