package ui

import (
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
	for _, bad := range []string{"someday", "2026-13-01", "+x"} {
		if _, err := parseDate(bad, now); err == nil {
			t.Errorf("parseDate(%q) accepted", bad)
		}
	}
}

func TestParseHours(t *testing.T) {
	for in, want := range map[string]float64{"1.5": 1.5, "1,5": 1.5, "1:30": 1.5, "90m": 1.5, "2h": 2, "2h30m": 2.5, "0.25": 0.25} {
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
