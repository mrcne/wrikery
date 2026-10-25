package main

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

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
// The .TH date is read back from the committed page, so a run without -update reproduces it byte for byte,
// and -update stamps a new date only when the text of the page changed.
func TestManualMatchesTheCommittedPage(t *testing.T) {
	fs, _ := rootFlags()
	want, err := os.ReadFile(manualPath)
	if err != nil && !*updateManual {
		t.Fatalf("%v, run go test ./cmd/wrikery -update to write the page", err)
	}
	got := []byte(manualPage(fs, pageDate(string(want))))
	if *updateManual && !bytes.Equal(got, want) {
		got = []byte(manualPage(fs, weekendBefore(time.Now())))
		if err := os.WriteFile(manualPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		want = got
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
	page := manualPage(fs, "2026-10-18")
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
	if !strings.Contains(page, "also holds these actions without a key: budget.") {
		t.Error("the page does not name the budget row of the actions list")
	}
	allowed := map[string]bool{".TH": true, ".SH": true, ".SS": true, ".TP": true, ".PP": true, ".B": true, ".I": true, ".BI": true, ".RI": true, ".nf": true, ".fi": true, ".br": true, ".RS": true, ".RE": true}
	lines := strings.Split(page, "\n")
	for n, line := range lines {
		if strings.HasPrefix(line, ".") || strings.HasPrefix(line, "'") {
			macro, _, _ := strings.Cut(line, " ")
			if !allowed[macro] {
				t.Errorf("line %d would be read as a request: %q", n+1, line)
			}
		}
		if line == "" && n+1 < len(lines) {
			t.Errorf("line %d is empty, troff prints a blank line for it", n+1)
		}
	}
}

func TestManualListsEveryKeyOfABinding(t *testing.T) {
	fs, _ := rootFlags()
	page := manualPage(fs, "2026-10-18")
	for _, want := range []string{`.B "q, ctrl+c"`, `.B "S, ctrl+b"`, `.B "j, down"`, `.B "ctrl+d, pgdown"`, ".B space", `.B "f F"`} {
		if !strings.Contains(page, want+"\n") {
			t.Errorf("the KEYS section lacks %q", want)
		}
	}
}

func TestKeylessTextNamesTheActionsWithoutAKey(t *testing.T) {
	if got := keylessText(nil); got != "" {
		t.Errorf("keylessText(nil) = %q, want nothing", got)
	}
	want := "The actions list on . also holds these actions without a key: budget."
	if got := keylessText([]string{"budget"}); got != want {
		t.Errorf("keylessText = %q, want %q", got, want)
	}
	if got := keylessText([]string{"budget", "follow"}); !strings.Contains(got, "budget, follow.") {
		t.Errorf("keylessText = %q, want the names joined by a comma", got)
	}
}

func TestPageDateIsTheWeekendBeforeAndIsReadBack(t *testing.T) {
	for in, want := range map[string]string{
		"2026-10-22": "2026-10-18", // a Thursday goes back to the Sunday
		"2026-10-19": "2026-10-18", // a Monday too
		"2026-10-24": "2026-10-24", // a Saturday stays
		"2026-10-25": "2026-10-25", // a Sunday stays
	} {
		day, err := time.Parse("2006-01-02", in)
		if err != nil {
			t.Fatal(err)
		}
		if got := weekendBefore(day); got != want {
			t.Errorf("weekendBefore(%s) = %s, want %s", in, got, want)
		}
	}
	if got := pageDate(".TH WRIKERY 1 2026-10-18 wrikery \"User Commands\"\n.SH NAME\n"); got != "2026-10-18" {
		t.Errorf("pageDate = %q, want the .TH date", got)
	}
	if got := pageDate(""); got == "" {
		t.Error("pageDate of no page is empty, want a day to stamp")
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

func TestArgQuotesASpaceAndEscapesADoubleQuote(t *testing.T) {
	for in, want := range map[string]string{
		"plain":     "plain",
		"two words": `"two words"`,
		`say "hi"`:  `"say \(dqhi\(dq"`,
		`a"b`:       `a\(dqb`,
	} {
		if got := arg(in); got != want {
			t.Errorf("arg(%q) = %q, want %q", in, got, want)
		}
	}
}

// The EXAMPLES section is the README's command block, the two must not drift apart.
func TestExamplesAreTheReadmeCommandBlock(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, after, ok := strings.Cut(string(readme), "```sh\n")
	block, _, ok2 := strings.Cut(after, "\n```")
	if !ok || !ok2 {
		t.Fatal("README.md has no sh block")
	}
	if block != examples {
		t.Errorf("the manual's examples differ from README.md:\n%s\n---\n%s", examples, block)
	}
}
