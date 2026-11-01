package cli

import (
	"bytes"
	"encoding/json"
	"errors"
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

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) {
	return 0, errors.New("write /dev/stdout: broken pipe")
}

// The code of a write command says what happened to the write, an agent retries on a 1 and a create would land twice.
func TestPrintJSONKeepsTheCodeWhenTheOutputCannotBeWritten(t *testing.T) {
	var errOut bytes.Buffer
	if code := printJSON(Env{Stdout: brokenWriter{}, Stderr: &errOut}, map[string]bool{"sent": true}); code != exitOK {
		t.Errorf("code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
}

func TestPrintJSONFailsOnAValueItCannotEncode(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := printJSON(Env{Stdout: &out, Stderr: &errOut}, map[string]any{"f": func() {}}); code != exitError {
		t.Errorf("code = %d, want %d", code, exitError)
	}
}
