package ui

import (
	"math"
	"testing"
	"time"
)

func TestParseDate(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) // Thursday
	cases := map[string]string{
		"":               "",
		"2026-09-12":     "2026-09-12",
		"today":          "2026-09-03",
		"tomorrow":       "2026-09-04",
		"yesterday":      "2026-09-02",
		"yday":           "2026-09-02",
		"fri":            "2026-09-04",
		"friday":         "2026-09-04",
		"thu":            "2026-09-10", // the same weekday means next week, today is the word for today
		"thurs":          "2026-09-10",
		"tues":           "2026-09-08",
		"mon":            "2026-09-07",
		"next mon":       "2026-09-07",
		"next tues":      "2026-09-08",
		"last mon":       "2026-08-31",
		"last wednesday": "2026-09-02",
		"last thu":       "2026-08-27", // and last week
		"next thu":       "2026-09-10",
		"Last  Fri":      "2026-08-28",
		"+3d":            "2026-09-06",
		"-1d":            "2026-09-02",
		"12 sep":         "2026-09-12",
		"3 sep":          "2026-09-03",
		"1 sep":          "2027-09-01", // a day and month that has passed is next year
	}
	for in, want := range cases {
		got, err := parseDate(in, now, dateAhead)
		if err != nil || got != want {
			t.Errorf("parseDate(%q, ahead) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{
		"someday", "2026-13-01", "+x", "+3", "+d", "--3d", "+-3d", "++3d", "+3661d", "-3661d", "+3000000d",
		"last", "next", "last someday", "last +3d", "next 12 sep", "fridays", "weds", "mo",
		"29 feb", // neither 2026 nor 2027 has one
	}
	for _, in := range bad {
		if got, err := parseDate(in, now, dateAhead); err == nil {
			t.Errorf("parseDate(%q) accepted as %q", in, got)
		}
	}
}

// Time is logged after the fact, so the time entry box reads a bare weekday or a day and month as the latest one, today included.
// last and next say the same in both boxes.
func TestParseDateReadsBackForTimeEntries(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) // Thursday
	cases := map[string]string{
		"mon":      "2026-08-31",
		"monday":   "2026-08-31",
		"thu":      "2026-09-03", // today, the day's work is logged in the evening
		"fri":      "2026-08-28",
		"last thu": "2026-08-27",
		"last fri": "2026-08-28",
		"next fri": "2026-09-04",
		"today":    "2026-09-03",
		"+3d":      "2026-09-06",
		"3 sep":    "2026-09-03",
		"2 sep":    "2026-09-02",
		"28 dec":   "2025-12-28", // a day and month still ahead is last year
	}
	for in, want := range cases {
		got, err := parseDate(in, now, dateBack)
		if err != nil || got != want {
			t.Errorf("parseDate(%q, back) = %q, %v; want %q", in, got, err, want)
		}
	}
}

// 29 feb exists in the year the direction picks, or not at all.
func TestParseDateLeapDay(t *testing.T) {
	back := time.Date(2028, 3, 5, 12, 0, 0, 0, time.UTC)
	if got, err := parseDate("29 feb", back, dateBack); err != nil || got != "2028-02-29" {
		t.Errorf("29 feb back from March 2028 = %q, %v; want 2028-02-29", got, err)
	}
	ahead := time.Date(2027, 12, 1, 12, 0, 0, 0, time.UTC)
	if got, err := parseDate("29 feb", ahead, dateAhead); err != nil || got != "2028-02-29" {
		t.Errorf("29 feb ahead from December 2027 = %q, %v; want 2028-02-29", got, err)
	}
	if got, err := parseDate("29 feb", back, dateAhead); err == nil {
		t.Errorf("29 feb ahead from March 2028 accepted as %q, 2029 has none", got)
	}
}

func TestParseHours(t *testing.T) {
	for in, want := range map[string]float64{"1.5": 1.5, "1,5": 1.5, "1:30": 1.5, "90m": 1.5, "2h": 2, "2h30m": 2.5, "0.25": 0.25, "1.5h": 1.5, "0,5h": 0.5} {
		got, err := parseHours(in)
		if err != nil || got != want {
			t.Errorf("parseHours(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "0", "-1", "25", "abc", "1:75"} {
		if _, err := parseHours(bad); err == nil {
			t.Errorf("parseHours(%q) accepted", bad)
		}
	}
}

func TestFormatHoursRoundTripsThroughParseHours(t *testing.T) {
	cases := map[float64]string{2: "2", 1.5: "1:30", 1.0 + 20.0/60.0: "1:20"}
	for h, want := range cases {
		got := formatHours(h)
		if got != want {
			t.Errorf("formatHours(%v) = %q, want %q", h, got, want)
		}
		back, err := parseHours(got)
		if err != nil || back != h {
			t.Errorf("parseHours(formatHours(%v)) = %v, %v; want %v", h, back, err, h)
		}
	}
}

// The grid shows hours as h:mm, every other place as 1h 20m, both exact to the minute, so the grid can be checked against what was typed.
func TestHoursTextAndClock(t *testing.T) {
	cases := []struct {
		h           float64
		text, clock string
	}{
		{2, "2h", "2:00"},
		{0.75, "45m", "0:45"},
		{1.0 + 20.0/60.0, "1h 20m", "1:20"},
		{38.75, "38h 45m", "38:45"},
		{1.9999, "2h", "2:00"}, // minutes round and carry, the same as formatHours
		{0.5, "30m", "0:30"},
	}
	for _, c := range cases {
		if got := hoursText(c.h); got != c.text {
			t.Errorf("hoursText(%v) = %q, want %q", c.h, got, c.text)
		}
		if got := hoursClock(c.h); got != c.clock {
			t.Errorf("hoursClock(%v) = %q, want %q", c.h, got, c.clock)
		}
	}
}

// What the app prints as logged time, 1h 20m with the space, reads back in the time entry box.
func TestParseHoursReadsWhatHoursTextPrints(t *testing.T) {
	for _, h := range []float64{2, 0.75, 1 + 20.0/60, 23.5} {
		got, err := parseHours(hoursText(h))
		if err != nil || math.Abs(got-h) > 1e-9 {
			t.Errorf("parseHours(%q) = %v, %v; want %v", hoursText(h), got, err, h)
		}
	}
	for in, want := range map[string]float64{"1h 20m": 1 + 20.0/60, "1 h": 1, "1 : 30": 1.5} {
		if got, err := parseHours(in); err != nil || math.Abs(got-want) > 1e-9 {
			t.Errorf("parseHours(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
}

func TestParseDurationHasNoCap(t *testing.T) {
	for in, want := range map[string]float64{"200h": 200, "1h 20m": 1 + 20.0/60, "0.5": 0.5, "": 0, "   ": 0} {
		got, err := parseDuration(in)
		if err != nil || math.Abs(got-want) > 1e-9 {
			t.Errorf("parseDuration(%q) = %v, %v, want %v", in, got, err, want)
		}
	}
	if _, err := parseDuration("soon"); err == nil {
		t.Error("a word is not a duration")
	}
	// strconv reads these as numbers, a budget or a time entry cannot be one of them.
	for _, in := range []string{"nan", "NaN", "inf", "+Inf", "-inf", "infinity"} {
		if _, err := parseDuration(in); err == nil {
			t.Errorf("parseDuration(%q) accepted it", in)
		}
		if _, err := parseHours(in); err == nil {
			t.Errorf("parseHours(%q) accepted it", in)
		}
	}
	if _, err := parseHours("25h"); err == nil {
		t.Error("a time entry keeps its cap")
	}
}
