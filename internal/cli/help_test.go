package cli

import (
	"context"
	"flag"
	"strings"
	"testing"
)

// runHelp is Run on a fresh env with both outputs captured.
func runHelp(t *testing.T, args ...string) (code int, out, errOut string) {
	t.Helper()
	env, o, e := testEnv(t)
	code = Run(context.Background(), env, args)
	return code, o.String(), e.String()
}

func TestHelpWordsPrintOnePage(t *testing.T) {
	want := HelpPage(nil)
	for _, word := range []string{"help", "-h", "--help"} {
		code, out, errOut := runHelp(t, word)
		if code != exitOK || errOut != "" {
			t.Errorf("%s: code %d, stderr %q", word, code, errOut)
		}
		if out != want {
			t.Errorf("%s printed:\n%s\nwant the page:\n%s", word, out, want)
		}
	}
}

func TestHelpPageListsEveryCommand(t *testing.T) {
	page := HelpPage(nil)
	for _, c := range Commands() {
		for _, want := range []string{"wrikery " + c.Synopsis[0], c.Short} {
			if !strings.Contains(page, want) {
				t.Errorf("page lacks %q", want)
			}
		}
	}
	for _, want := range []string{"Every command takes --json", `"wrikery help <command>"`, `"man wrikery"`} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "Flags:") {
		t.Errorf("a page without a flag set has a Flags section:\n%s", page)
	}
	fs := flag.NewFlagSet("wrikery", flag.ContinueOnError)
	fs.Bool("offline", false, "start without syncing")
	if withFlags := HelpPage(fs); !strings.Contains(withFlags, "Flags:") || !strings.Contains(withFlags, "offline") {
		t.Errorf("page with a flag set lacks the flags:\n%s", withFlags)
	}
}

func TestEveryCommandHasItsOwnHelp(t *testing.T) {
	for _, c := range Commands() {
		code, out, errOut := runHelp(t, append(strings.Fields(c.Name), "--help")...)
		if code != exitOK || errOut != "" {
			t.Errorf("%s --help: code %d, stderr %q", c.Name, code, errOut)
		}
		for _, want := range []string{"usage: wrikery " + c.Synopsis[0], c.Notes, "-json"} {
			if want != "" && !strings.Contains(out, want) {
				t.Errorf("%s --help lacks %q:\n%s", c.Name, want, out)
			}
		}
	}
}

func TestGroupHelpPrintsTheGroupOnStdout(t *testing.T) {
	for _, g := range Groups() {
		for _, word := range []string{"--help", "-h"} {
			code, out, errOut := runHelp(t, g.Name, word)
			if code != exitOK || errOut != "" {
				t.Errorf("%s %s: code %d, stderr %q", g.Name, word, code, errOut)
			}
			for _, c := range Commands() {
				if strings.HasPrefix(c.Name, g.Name+" ") && !strings.Contains(out, "wrikery "+c.Synopsis[0]) {
					t.Errorf("%s %s lacks %q:\n%s", g.Name, word, c.Synopsis[0], out)
				}
			}
			if !strings.Contains(out, g.Notes) {
				t.Errorf("%s %s lacks the group notes:\n%s", g.Name, word, out)
			}
		}
		code, out, errOut := runHelp(t, g.Name)
		if code != exitUsage || out != "" || !strings.Contains(errOut, "usage: wrikery "+g.Name) {
			t.Errorf("%s alone: code %d, stdout %q, stderr %q", g.Name, code, out, errOut)
		}
	}
}

func TestHelpOfACommandIsItsHelpFlag(t *testing.T) {
	pairs := [][2][]string{
		{{"help", "sync"}, {"sync", "--help"}},
		{{"help", "task"}, {"task", "--help"}},
		{{"help", "task", "status"}, {"task", "status", "--help"}},
		{{"help", "ui", "filter"}, {"ui", "filter", "--help"}},
		// The --help must land before the --, or everything after it is a title.
		{{"help", "task", "create", "--", "x"}, {"task", "create", "--help"}},
	}
	for _, p := range pairs {
		code, out, errOut := runHelp(t, p[0]...)
		_, want, _ := runHelp(t, p[1]...)
		if code != exitOK || errOut != "" || out == "" || out != want {
			t.Errorf("%v: code %d, stderr %q, stdout:\n%s\nwant:\n%s", p[0], code, errOut, out, want)
		}
	}
	code, out, errOut := runHelp(t, "help", "frobnicate")
	if code != exitUsage || out != "" || !strings.Contains(errOut, "frobnicate") || !strings.Contains(errOut, "usage: wrikery [flags]") {
		t.Errorf("help frobnicate: code %d, stdout %q, stderr %q", code, out, errOut)
	}
	// help help must print the page, not call itself.
	for _, word := range []string{"help", "-h", "--help"} {
		code, out, errOut := runHelp(t, "help", word)
		if code != exitOK || errOut != "" || out != HelpPage(nil) {
			t.Errorf("help %s: code %d, stderr %q, stdout:\n%s", word, code, errOut, out)
		}
	}
}
