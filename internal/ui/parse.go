package ui

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// A date field reads ahead for a due date and back for a time entry, the caller says which.
// A bare weekday or a day and month is then the coming one or the latest one.
// Today counts when reading back, the day's work is logged in the evening.
// It never counts when reading ahead, "today" is the word for a due date of today.
type dateDirection int

const (
	dateAhead dateDirection = iota
	dateBack
)

var weekdayNames = [...]string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}

// maxDayOffset bounds +Nd and -Nd to about ten years, a bigger number is a typo and an unbounded one runs past the year 9999.
const maxDayOffset = 3660

// parseDate accepts what people type in a hurry: an ISO date, today, tomorrow, yesterday or yday,
// a weekday in full or cut short after three letters, alone or after last or next, +Nd or -Nd, and "12 sep" for a day and month.
// Empty stays empty so a field can be cleared.
func parseDate(s string, now time.Time, dir dateDirection) (string, error) {
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
		return weekdayFrom(today, wd, dir).Format("2006-01-02"), nil
	}
	if word, name, ok := strings.Cut(s, " "); ok && (word == "last" || word == "next") {
		wd, ok := weekdayNamed(name)
		if !ok {
			return "", fmt.Errorf("expected a weekday after %s, got %q", word, name)
		}
		if word == "last" {
			// The previous one, never today: the latest one up to yesterday.
			return weekdayFrom(today.AddDate(0, 0, -1), wd, dateBack).Format("2006-01-02"), nil
		}
		return weekdayFrom(today, wd, dateAhead).Format("2006-01-02"), nil
	}
	if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") {
		// strconv.Atoi takes a sign of its own, the digit check keeps --3d from reading as +3.
		if len(s) < 3 || s[1] < '0' || s[1] > '9' || !strings.HasSuffix(s, "d") {
			return "", fmt.Errorf("expected +Nd or -Nd, got %q", s)
		}
		n, err := strconv.Atoi(s[1 : len(s)-1])
		if err != nil {
			return "", fmt.Errorf("expected +Nd or -Nd, got %q", s)
		}
		if n > maxDayOffset {
			return "", fmt.Errorf("%s is more than ten years away", s)
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
			return dayAndMonth(today, t.Month(), t.Day(), dir)
		}
	}
	return "", errors.New("could not read the date, try 2026-09-12, today, yday, fri, last fri, next fri, +3d or 12 sep")
}

// weekdayNamed reads a weekday in full or cut short after three or more letters, tue, tues and thurs included.
func weekdayNamed(s string) (time.Weekday, bool) {
	if len(s) < 3 {
		return 0, false
	}
	for i, name := range weekdayNames {
		if strings.HasPrefix(name, s) {
			return time.Weekday(i), true
		}
	}
	return 0, false
}

// weekdayFrom is the coming day of that weekday after today, or the latest one up to today when reading back.
func weekdayFrom(today time.Time, wd time.Weekday, dir dateDirection) time.Time {
	diff := int(wd) - int(today.Weekday())
	switch {
	case dir == dateBack && diff > 0:
		diff -= 7
	case dir == dateAhead && diff <= 0:
		diff += 7
	}
	return today.AddDate(0, 0, diff)
}

// dayAndMonth puts a day and month in the year the direction picks: this year,
// next year once it has passed for a due date, last year while it is still ahead for a time entry.
// time.Parse read the day against year 0, a leap year, so 29 feb is checked again in the year picked.
func dayAndMonth(today time.Time, month time.Month, day int, dir dateDirection) (string, error) {
	year := today.Year()
	d := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	switch {
	case dir == dateBack && d.After(today):
		year--
	case dir == dateAhead && d.Before(today):
		year++
	}
	d = time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if d.Month() != month {
		return "", fmt.Errorf("there is no %d %s in %d", day, strings.ToLower(month.String()[:3]), year)
	}
	return d.Format("2006-01-02"), nil
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
