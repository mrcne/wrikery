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
