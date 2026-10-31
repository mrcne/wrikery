package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

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

func TestPrintJSONWritesTheIndentedDocumentAndANewline(t *testing.T) {
	var out, errOut bytes.Buffer
	env := Env{Stdout: &out, Stderr: &errOut}
	v := map[string]any{"title": "Fix <b>auth</b> & retry", "rows": []int{1, 2}}
	if code := printJSON(env, v); code != exitOK {
		t.Fatalf("code = %d, stderr %q", code, errOut.String())
	}
	want, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want)+"\n" {
		t.Errorf("printed %q, want %q", out.String(), string(want)+"\n")
	}
}
