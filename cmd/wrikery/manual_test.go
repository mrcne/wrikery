package main

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/mrcne/wrikery/internal/cli"
	"github.com/mrcne/wrikery/internal/ui"
)

var updateManual = flag.Bool("update", false, "rewrite man/wrikery.1 from the code")

const manualPath = "../../man/wrikery.1"

func TestTroffEscapesWhatTroffWouldEat(t *testing.T) {
	for in, want := range map[string]string{
		".hidden": `\&.hidden`,
		"'quoted": `\&'quoted`,
		"a-b":     `a\-b`,
		`x\y`:     `x\ey`,
		"plain":   "plain",
	} {
		if got := troff(in); got != want {
			t.Errorf("troff(%q) = %q, want %q", in, got, want)
		}
	}
}

// The page is committed like a golden file, so a change to a command, a flag or a key shows up as a diff of it.
func TestManualMatchesTheCommittedPage(t *testing.T) {
	fs, _ := rootFlags()
	got := []byte(manualPage(fs))
	if *updateManual {
		if err := os.WriteFile(manualPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(manualPath)
	if err != nil {
		t.Fatalf("%v, run go test ./cmd/wrikery -update to write the page", err)
	}
	if !bytes.Equal(got, want) {
		gotLines, wantLines := strings.Split(string(got), "\n"), strings.Split(string(want), "\n")
		for i := range min(len(gotLines), len(wantLines)) {
			if gotLines[i] != wantLines[i] {
				t.Fatalf("man/wrikery.1 is behind the code at line %d, run go test ./cmd/wrikery -update and commit it:\n%s\n%s", i+1, wantLines[i], gotLines[i])
			}
		}
		t.Fatalf("man/wrikery.1 is behind the code, run go test ./cmd/wrikery -update and commit it")
	}
}

func TestManualCoversTheCommandsFlagsAndKeys(t *testing.T) {
	fs, _ := rootFlags()
	page := manualPage(fs)
	for i := range len(page) {
		if page[i] >= 0x80 {
			t.Fatalf("byte %d of the page is not ASCII", i)
		}
	}
	if !strings.HasPrefix(page, ".TH WRIKERY 1 ") {
		t.Errorf("the page does not start with .TH: %q", page[:min(len(page), 40)])
	}
	last := -1
	for _, name := range []string{"NAME", "SYNOPSIS", "DESCRIPTION", "OPTIONS", "COMMANDS", "KEYS", "EXIT STATUS", "ENVIRONMENT", "FILES", "EXAMPLES", "SEE ALSO"} {
		i := strings.Index(page, "\n.SH "+name+"\n")
		if i < 0 || i < last {
			t.Errorf("section %s is missing or out of order", name)
		}
		last = i
	}
	wantFlags := func(where string, set *flag.FlagSet) {
		set.VisitAll(func(f *flag.Flag) {
			if !strings.Contains(page, `\-\-`+troff(f.Name)) {
				t.Errorf("the page lacks --%s of %s", f.Name, where)
			}
		})
	}
	wantFlags("the root", fs)
	for _, c := range cli.Commands() {
		if !strings.Contains(page, "\n.SS "+c.Name+"\n") {
			t.Errorf("the page lacks a heading for %s", c.Name)
		}
		wantFlags(c.Name, c.Flags())
	}
	for _, g := range ui.KeyGroups() {
		if !strings.Contains(page, "\n.SS "+g.Title+"\n") {
			t.Errorf("the page lacks the key group %s", g.Title)
		}
	}
	allowed := map[string]bool{".TH": true, ".SH": true, ".SS": true, ".TP": true, ".PP": true, ".B": true, ".I": true, ".BI": true, ".RI": true, ".nf": true, ".fi": true, ".br": true, ".RS": true, ".RE": true}
	for n, line := range strings.Split(page, "\n") {
		if strings.HasPrefix(line, ".") || strings.HasPrefix(line, "'") {
			macro, _, _ := strings.Cut(line, " ")
			if !allowed[macro] {
				t.Errorf("line %d would be read as a request: %q", n+1, line)
			}
		}
		if line == "" && n+1 < len(strings.Split(page, "\n")) {
			t.Errorf("line %d is empty, troff prints a blank line for it", n+1)
		}
	}
}

// mandoc is the man of macOS, where the cask installs the page. The Linux check is man -l and groff by hand.
func TestManualPassesMandocLint(t *testing.T) {
	mandoc, err := exec.LookPath("mandoc")
	if err != nil {
		t.Skip("mandoc is not on PATH, the lint runs where it is")
	}
	out, err := exec.Command(mandoc, "-T", "lint", "-W", "warning", manualPath).CombinedOutput()
	if err != nil || len(out) > 0 {
		t.Errorf("mandoc -T lint: %v\n%s", err, out)
	}
}
