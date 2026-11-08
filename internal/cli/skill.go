package cli

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mrcne/wrikery/internal/atomicfile"
	"github.com/mrcne/wrikery/internal/store"
	"github.com/mrcne/wrikery/skills"
)

var cmdSkill = Command{
	Name:     "skill",
	Synopsis: []string{"skill", "skill install [--project | --dir DIR]"},
	Short:    "print the agent skill, or install it where the coding agents look",
	Notes: `Prints the skill that tells an AI agent how to work with Wrike through wrikery, a file in the Agent Skills format, so wrikery skill > FILE works too.
install writes the whole skill folder under ~/.claude/skills and ~/.agents/skills, the directories Claude Code and the other coding agents read, --project puts it under the working directory instead and --dir DIR under one directory of your choice.
The folder is in the repository as well, so gh skill install mrcne/wrikery and npx skills add mrcne/wrikery install it too.`,
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
	written := make([]string, 0, len(targets))
	for _, target := range targets {
		dest := filepath.Join(target, skills.Name)
		if err := writeSkill(dest); err != nil {
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
// which Codex, Cursor, Copilot, Gemini CLI and the others read, under the home directory or with --project the working one.
// An agent with a directory of its own gets it through --dir, a table of every agent's path would go stale faster than this help.
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

// writeSkill puts the embedded folder at dest, replacing the files an older binary or a hand edit left there.
func writeSkill(dest string) error {
	return fs.WalkDir(skills.FS, skills.Name, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(strings.TrimPrefix(path, skills.Name)))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := skills.FS.ReadFile(path)
		if err != nil {
			return err
		}
		return atomicfile.Write(target, b, 0o644)
	})
}

func printSkill(env Env, asJSON bool) int {
	if !asJSON {
		b, err := skills.FS.ReadFile(skills.Name + "/SKILL.md")
		if err != nil {
			return fail(env, err)
		}
		if _, err := env.Stdout.Write(b); err != nil {
			return fail(env, err)
		}
		return exitOK
	}
	files := map[string]string{}
	err := fs.WalkDir(skills.FS, skills.Name, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := skills.FS.ReadFile(path)
		if err != nil {
			return err
		}
		files[strings.TrimPrefix(path, skills.Name+"/")] = string(b)
		return nil
	})
	if err != nil {
		return fail(env, err)
	}
	return printJSON(env, files)
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
