package ui

import (
	"math"
	"testing"
	"time"
)

func TestParseDate(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) // Thursday
	cases := map[string]string{
		"":           "",
		"2026-09-12": "2026-09-12",
		"today":      "2026-09-03",
		"tomorrow":   "2026-09-04",
		"yesterday":  "2026-09-02",
		"fri":        "2026-09-04",
		"thu":        "2026-09-10", // the same weekday means next week
		"mon":        "2026-09-07",
		"+3d":        "2026-09-06",
		"-1d":        "2026-09-02",
		"12 sep":     "2026-09-12",
	}
	for in, want := range cases {
		got, err := parseDate(in, now)
		if err != nil || got != want {
			t.Errorf("parseDate(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"someday", "2026-13-01", "+x", "+3"} {
		if _, err := parseDate(bad, now); err == nil {
			t.Errorf("parseDate(%q) accepted", bad)
		}
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
