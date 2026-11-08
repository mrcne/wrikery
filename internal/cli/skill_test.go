package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/mrcne/wrikery/skills"
)

func skillFile(t *testing.T, name string) string {
	t.Helper()
	b, err := skills.FS.ReadFile(skills.Name + "/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// frontmatter is the key: value block between the first two --- lines, the skill keeps it flat so no YAML parser is needed here.
func frontmatter(t *testing.T, doc string) (map[string]string, string) {
	t.Helper()
	rest, ok := strings.CutPrefix(doc, "---\n")
	if !ok {
		t.Fatal("SKILL.md does not start with ---")
	}
	head, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		t.Fatal("SKILL.md frontmatter is not closed")
	}
	fields := map[string]string{}
	for _, line := range strings.Split(head, "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			t.Fatalf("frontmatter line without a value: %q", line)
		}
		fields[key] = value
	}
	return fields, body
}

// codeOf is the text inside the fenced blocks and the inline code spans of a markdown document.
func codeOf(doc string) string {
	var b strings.Builder
	fenced := false
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			b.WriteString(line + "\n")
			continue
		}
		parts := strings.Split(line, "`")
		for i := 1; i < len(parts); i += 2 {
			b.WriteString(parts[i] + "\n")
		}
	}
	return b.String()
}

// The skill follows the Agent Skills format, https://agentskills.io/specification: a flat frontmatter with the name
// of its directory and a description within the limit, a body an agent loads whole, and only commands that exist.
func TestSkillFolderIsAValidAgentSkill(t *testing.T) {
	fields, body := frontmatter(t, skillFile(t, "SKILL.md"))
	if fields["name"] != skills.Name {
		t.Errorf("name %q, the directory is %q", fields["name"], skills.Name)
	}
	if n := len(fields["description"]); n == 0 || n > 1024 {
		t.Errorf("description has %d characters, the format allows 1 to 1024", n)
	}
	if strings.ContainsAny(fields["description"], `"'#`) || strings.Contains(fields["description"], ": ") {
		t.Errorf("description needs quoting as YAML: %q", fields["description"])
	}
	if n := strings.Count(body, "\n"); n > 150 {
		t.Errorf("body has %d lines, the design keeps it under 150", n)
	}
	for _, r := range skillFile(t, "SKILL.md") + skillFile(t, "references/json.md") {
		if r > 127 {
			t.Fatalf("the skill is not plain ASCII, it has %q", r)
		}
	}
	known := map[string]bool{"help": true}
	for _, c := range Commands() {
		known[c.Name] = true
	}
	for _, g := range Groups() {
		known[g.Name] = true
	}
	mention := regexp.MustCompile(`wrikery ([a-z]+)(?: ([a-z]+))?`)
	for _, name := range []string{"SKILL.md", "references/json.md"} {
		for _, m := range mention.FindAllStringSubmatch(codeOf(skillFile(t, name)), -1) {
			if !known[m[1]+" "+m[2]] && !known[m[1]] {
				t.Errorf("%s names a command that does not exist: %q", name, m[0])
			}
		}
	}
}

// jsonKeys are the keys a JSON shape prints, nested shapes included, read from the struct tags.
func jsonKeys(typ reflect.Type) []string {
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil
	}
	var keys []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.Anonymous {
			if name, _, _ := strings.Cut(f.Tag.Get("json"), ","); name != "" && name != "-" {
				keys = append(keys, name)
			}
		}
		keys = append(keys, jsonKeys(f.Type)...)
	}
	return keys
}

// Every field a command prints with --json is in the reference, so a new field is documented with the code that adds it.
func TestJSONReferenceListsEveryField(t *testing.T) {
	doc := skillFile(t, "references/json.md")
	for _, shape := range []any{taskDetailJSON{}, changeJSON{}, syncJSON{}, writeJSON{}, fateJSON{}, unchangedJSON{}, commentResultJSON{}, uiJSON{}, installJSON{}} {
		typ := reflect.TypeOf(shape)
		for _, key := range jsonKeys(typ) {
			if !strings.Contains(doc, "`"+key+"`") {
				t.Errorf("references/json.md lacks `%s` of %s", key, typ.Name())
			}
		}
	}
}

func TestSkillPrintsTheSkillFile(t *testing.T) {
	env, out, _ := testEnv(t)
	if code := Run(context.Background(), env, []string{"skill"}); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	if out.String() != skillFile(t, "SKILL.md") {
		t.Errorf("skill printed:\n%s", out.String())
	}
}

func TestSkillJSONCarriesEveryFile(t *testing.T) {
	env, out, _ := testEnv(t)
	if code := Run(context.Background(), env, []string{"skill", "--json"}); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	var files map[string]string
	if err := json.Unmarshal(out.Bytes(), &files); err != nil {
		t.Fatal(err)
	}
	if files["SKILL.md"] != skillFile(t, "SKILL.md") || files["references/json.md"] != skillFile(t, "references/json.md") {
		t.Errorf("files: %v", files)
	}
}

func TestSkillInstallWritesTheTwoUserFolders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	env, out, _ := testEnv(t)
	if code := Run(context.Background(), env, []string{"skill", "install"}); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	claude := filepath.Join(home, ".claude", "skills", "wrikery")
	agents := filepath.Join(home, ".agents", "skills", "wrikery")
	for _, path := range []string{filepath.Join(claude, "SKILL.md"), filepath.Join(agents, "SKILL.md"), filepath.Join(claude, "references", "json.md"), filepath.Join(agents, "references", "json.md")} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != skillFile(t, strings.TrimPrefix(strings.TrimPrefix(path, claude+"/"), agents+"/")) {
			t.Errorf("%s differs from the embedded file", path)
		}
	}
	if want := "wrote " + claude + "\nwrote " + agents + "\n"; out.String() != want {
		t.Errorf("install printed:\n%s", out.String())
	}
}

func TestSkillInstallProjectWritesUnderTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", t.TempDir())
	env, out, _ := testEnv(t)
	if code := Run(context.Background(), env, []string{"skill", "install", "--project"}); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	for _, path := range []string{filepath.Join(dir, ".claude", "skills", "wrikery", "SKILL.md"), filepath.Join(dir, ".agents", "skills", "wrikery", "SKILL.md")} {
		if _, err := os.Stat(path); err != nil {
			t.Error(err)
		}
	}
	if entries, _ := os.ReadDir(os.Getenv("HOME")); len(entries) != 0 {
		t.Errorf("--project wrote under the home directory: %v", entries)
	}
	if !strings.Contains(out.String(), "wrote "+filepath.Join(dir, ".agents", "skills", "wrikery")) {
		t.Errorf("install printed:\n%s", out.String())
	}
}

// --dir writes one folder, and a second install replaces what an older binary or a hand edit left there.
func TestSkillInstallDirWritesOneFolderAndReplacesIt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	target := filepath.Join(dir, "wrikery", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	env, out, _ := testEnv(t)
	if code := Run(context.Background(), env, []string{"skill", "install", "--dir", dir, "--json"}); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	var got installJSON
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Written) != 1 || got.Written[0] != filepath.Join(dir, "wrikery") {
		t.Errorf("written: %v", got.Written)
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != skillFile(t, "SKILL.md") {
		t.Errorf("SKILL.md was not replaced:\n%s", b)
	}
	if entries, _ := os.ReadDir(os.Getenv("HOME")); len(entries) != 0 {
		t.Errorf("--dir wrote under the home directory: %v", entries)
	}
}

func TestSkillTakesInstallOrNothing(t *testing.T) {
	for _, args := range [][]string{{"skill", "print"}, {"skill", "--project"}, {"skill", "--dir", t.TempDir()}, {"skill", "install", "--project", "--dir", t.TempDir()}, {"skill", "install", "now"}} {
		env, _, errOut := testEnv(t)
		if code := Run(context.Background(), env, args); code != exitUsage {
			t.Errorf("%v: exit %d", args, code)
		}
		if !strings.Contains(errOut.String(), "usage: wrikery skill") {
			t.Errorf("%v: stderr:\n%s", args, errOut.String())
		}
	}
}

// The word an agent sets in WRIKERY_SOURCE lands on every row it queues, the way cli does for a person's commands.
func TestSourceWordFromTheEnvironment(t *testing.T) {
	for _, tc := range []struct{ in, want, err string }{
		{"", "cli", ""},
		{"codex", "codex", ""},
		{" Claude-Code ", "claude-code", ""},
		{"agent007", "agent007", ""},
		{"my agent", "", "letters, digits and hyphens"},
		{"a-name-of-twenty-five-chr", "", "letters, digits and hyphens"},
		{"tui", "", "names the interface"},
	} {
		got, err := SourceWord(tc.in)
		if tc.err == "" && (err != nil || got != tc.want) {
			t.Errorf("%q: got %q, %v", tc.in, got, err)
		}
		if tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err) || !strings.Contains(err.Error(), EnvSource)) {
			t.Errorf("%q: got %q, %v", tc.in, got, err)
		}
	}
}
