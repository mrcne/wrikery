package cli

import "testing"

func TestPadRightCountsTerminalCells(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"ab", "ab    "},
		{"\u65e5\u672c", "\u65e5\u672c  "},
		{"toolongvalue", "toolongvalue"},
	} {
		if got := padRight(tc.in, 6); got != tc.want {
			t.Errorf("padRight(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
