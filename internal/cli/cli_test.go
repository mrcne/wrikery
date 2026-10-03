package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrcne/wrikery/internal/config"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/internal/ui"
)

// testEnv is an Env on a fresh store with the output captured, no token and no network.
// Width 0 means a pipe, so the text output is the bare form without header lines.
func testEnv(t *testing.T) (Env, *bytes.Buffer, *bytes.Buffer) {
	// The golden holds comment times, so the zone must not depend on the machine.
	time.Local = time.UTC
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
