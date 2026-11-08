package cli

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mrcne/wrikery/internal/atomicfile"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/skills"
)

var cmdSkill = Command{
	Name:     "skill",
	Synopsis: []string{"skill", "skill install [--project | --dir DIR]"},
	Short:    "print the agent skill, or install it where the coding agents look",
	Notes: `Prints SKILL.md, the skill that tells an AI agent how to work with Wrike through wrikery, in the Agent Skills format, and --json prints every file of the skill folder.
install writes the folder, the reference file included, under ~/.claude/skills and ~/.agents/skills, the two directories the coding agents read, --project puts it under the working directory instead and --dir DIR under one directory of your choice, and a file an older skill left there goes.
The folder is in the repository as well, so gh skill install mrcne/wrikery and npx skills add mrcne/wrikery install it too.
Neither form reads the config or the cache or talks to Wrike.`,
	Flags: func() *flag.FlagSet { return new(skillOpts).flagSet() },
}

type skillOpts struct {
	json, project bool
	dir           string
}

func (o *skillOpts) flagSet() *flag.FlagSet {
	fs := jsonFlagSet("skill", &o.json)
	fs.BoolVar(&o.project, "project", false, "install under the working directory, for one repository")
	fs.StringVar(&o.dir, "dir", "", "install under the skills directory `DIR` alone")
	return fs
}

type installJSON struct {
	Written []string `json:"written"`
}

func runSkill(_ context.Context, env Env, args []string) int {
	var o skillOpts
	fs := o.flagSet()
	positional, code, done := parse(env, fs, cmdSkill.usage(), args)
	if done {
		return code
	}
	switch {
	case len(positional) == 0 && (o.project || o.dir != ""):
		return usageError(env, fs, cmdSkill.usage(), "--project and --dir go with install")
	case len(positional) == 0:
		return printSkill(env, o.json)
	case len(positional) > 1 || positional[0] != "install":
		return usageError(env, fs, cmdSkill.usage(), "skill takes install or nothing")
	case o.project && o.dir != "":
		return usageError(env, fs, cmdSkill.usage(), "--project and --dir exclude each other")
	}
	targets, err := skillTargets(o)
	if err != nil {
		return fail(env, err)
	}
	files, err := skillFiles()
	if err != nil {
		return fail(env, err)
	}
	written := make([]string, 0, len(targets))
	for _, target := range targets {
		dest := filepath.Join(target, skills.Name)
		if err := writeSkill(dest, files); err != nil {
			return fail(env, err)
		}
		written = append(written, dest)
	}
	if o.json {
		return printJSON(env, installJSON{Written: written})
	}
	for _, dest := range written {
		_, _ = fmt.Fprintln(env.Stdout, "wrote "+dest)
	}
	return exitOK
}

// skillTargets are the skills directories an install writes under.
// Without --dir they are the two the agents agree on: .claude/skills, which Claude Code reads, and .agents/skills,
// which the other coding agents share, see https://cli.github.com/manual/gh_skill_install, under the home directory or with --project the working one.
// Both are written whatever is installed here, the help says so and a folder nobody reads costs nothing, an agent with a directory of its own takes --dir.
func skillTargets(o skillOpts) ([]string, error) {
	if o.dir != "" {
		return []string{o.dir}, nil
	}
	base, err := os.UserHomeDir()
	if o.project {
		base, err = os.Getwd()
	}
	if err != nil {
		return nil, err
	}
	return []string{filepath.Join(base, ".claude", "skills"), filepath.Join(base, ".agents", "skills")}, nil
}

// skillFiles is the embedded folder as path under it to text, SKILL.md and the reference files.
func skillFiles() (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(skills.FS, skills.Name, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := skills.FS.ReadFile(path)
		if err != nil {
			return err
		}
		files[strings.TrimPrefix(path, skills.Name+"/")] = b
		return nil
	})
	return files, err
}

// writeSkill puts the files at dest and removes what an older skill left there,
// a reference file renamed or dropped since would otherwise stay next to the new text and be read with it.
func writeSkill(dest string, files map[string][]byte) error {
	keep := map[string]bool{".": true}
	for rel, b := range files {
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := atomicfile.Write(target, b, 0o644); err != nil {
			return err
		}
		for p := filepath.FromSlash(rel); p != "."; p = filepath.Dir(p) {
			keep[p] = true
		}
	}
	var stale []string
	err := filepath.WalkDir(dest, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if rel, _ := filepath.Rel(dest, path); !keep[rel] {
			stale = append(stale, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Deepest first, so a directory goes after what it held.
	sort.Slice(stale, func(i, j int) bool { return len(stale[i]) > len(stale[j]) })
	for _, path := range stale {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func printSkill(env Env, asJSON bool) int {
	files, err := skillFiles()
	if err != nil {
		return fail(env, err)
	}
	if asJSON {
		texts := make(map[string]string, len(files))
		for path, b := range files {
			texts[path] = string(b)
		}
		return printJSON(env, texts)
	}
	// A stdout that cannot be written is left alone, as printJSON leaves it, the exit code says what the command did.
	_, _ = env.Stdout.Write(files["SKILL.md"])
	return exitOK
}

// EnvSource names the source of a command's writes, the word the changes screen and wrikery changes show next to each one.
// An agent sets it to its own name, so a person tells the agent's writes from their own commands, which stay cli.
const EnvSource = "WRIKERY_SOURCE"

var sourceWord = regexp.MustCompile(`^[a-z0-9-]{1,24}$`)

// SourceWord is the source a command's rows carry: cli when the environment names none, else the word lowercased.
// A value that is not a word is an error and not a silent cli, an agent that sets it wrong should learn at once.
func SourceWord(value string) (string, error) {
	word := strings.ToLower(strings.TrimSpace(value))
	switch {
	case word == "":
		return store.SourceCLI, nil
	case word == store.SourceTUI:
		return "", fmt.Errorf("%s=%q names the interface, pick a word of your own", EnvSource, value)
	case !sourceWord.MatchString(word):
		return "", fmt.Errorf("%s must be a word of up to 24 letters, digits and hyphens, not %q", EnvSource, value)
	}
	return word, nil
}
