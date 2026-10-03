package cli

import "testing"

func TestPadRightCountsTerminalCells(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"ab", "ab    "},
		{"日本", "日本  "},
		{"toolongvalue", "toolongvalue"},
	} {
		if got := padRight(tc.in, 6); got != tc.want {
			t.Errorf("padRight(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
