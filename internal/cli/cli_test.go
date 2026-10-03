package cli

import (
	"bytes"
	"context"
	"flag"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/ui"
)

// testEnv is an Env on a fresh store with the output captured, no token and no network.
// Width 0 means a pipe, so the text output is the bare form without header lines.
func testEnv(t *testing.T) (Env, *bytes.Buffer, *bytes.Buffer) {
	// The golden holds comment times, so the zone must not depend on the machine.
	time.Local = time.UTC
	// Colors would put escape codes into the text the tests compare.
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "wrike.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	st.Now = func() time.Time { return time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC) }
	var out, errOut bytes.Buffer
	cfg := config.Config{}
	cfg.UI = config.UIConfig{Theme: "dark", ASCII: true}
	env := Env{
		Version:  "test",
		Config:   cfg,
		Store:    st,
		Theme:    ui.NewTheme(cfg.UI),
		Deadline: 300 * time.Millisecond,
		Stdout:   &out,
		Stderr:   &errOut,
	}
	return env, &out, &errOut
}

func TestRunUnknownCommandIsAUsageError(t *testing.T) {
	env, out, errOut := testEnv(t)
	if code := Run(context.Background(), env, []string{"frobnicate"}); code != exitUsage {
		t.Errorf("code = %d, want %d", code, exitUsage)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want nothing", out.String())
	}
	if !strings.Contains(errOut.String(), `unknown command "frobnicate"`) || !strings.Contains(errOut.String(), "task list") {
		t.Errorf("stderr = %q, want the error and the usage", errOut.String())
	}
}

func TestRunHelpPrintsTheUsageOnStdout(t *testing.T) {
	env, out, _ := testEnv(t)
	if code := Run(context.Background(), env, []string{"help"}); code != exitOK {
		t.Errorf("code = %d, want 0", code)
	}
	for _, want := range []string{"wrikery sync", "wrikery task list", "wrikery task show", "wrikery task status", "wrikery task create", "--json"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("usage lacks %q:\n%s", want, out.String())
		}
	}
}

func TestRunTaskAloneListsItsSubcommands(t *testing.T) {
	env, _, errOut := testEnv(t)
	if code := Run(context.Background(), env, []string{"task"}); code != exitUsage {
		t.Errorf("code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), "task status") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestRunSubcommandHelpFlagIsNotAnError(t *testing.T) {
	env, out, _ := testEnv(t)
	if code := Run(context.Background(), env, []string{"task", "list", "--help"}); code != exitOK {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "--folder") {
		t.Errorf("stdout = %q, want the list flags", out.String())
	}
}

func TestParseFindsFlagsAnywhereAmongTheArguments(t *testing.T) {
	cases := map[string][]string{
		"before":  {"-x", "a", "b"},
		"after":   {"a", "b", "-x"},
		"between": {"a", "-x", "b"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			env, _, _ := testEnv(t)
			fs := flag.NewFlagSet("t", flag.ContinueOnError)
			x := fs.Bool("x", false, "")
			got, code, done := parse(env, fs, "usage\n", args)
			if done || code != exitOK || !*x || !reflect.DeepEqual(got, []string{"a", "b"}) {
				t.Errorf("got %v code %d done %v x %v", got, code, done, *x)
			}
		})
	}
}

func TestParseTakesEverythingAfterDoubleDashAsArguments(t *testing.T) {
	env, _, _ := testEnv(t)
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	x := fs.Bool("x", false, "")
	got, _, done := parse(env, fs, "usage\n", []string{"a", "--", "-x", "b"})
	if done || *x || !reflect.DeepEqual(got, []string{"a", "-x", "b"}) {
		t.Errorf("got %v done %v x %v", got, done, *x)
	}
}

func TestParseHelpAnywherePrintsTheUsage(t *testing.T) {
	for _, args := range [][]string{{"--help", "a"}, {"a", "--help"}} {
		env, out, _ := testEnv(t)
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.Bool("x", false, "")
		_, code, done := parse(env, fs, "usage: t\n", args)
		if !done || code != exitOK || !strings.HasPrefix(out.String(), "usage: t") {
			t.Errorf("%v: code %d done %v out %q", args, code, done, out.String())
		}
	}
}
