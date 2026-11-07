package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

var errCannotEdit = errors.New("the key is written in a form the app cannot edit")

// Set writes one key to the config file and returns what the file loads afterwards.
// The app never writes the whole file: the TOML encoder would drop the comments a user put there.
// So the key's line is replaced in its table, or added to it, and every other line stays as it is.
// The new text is checked by the loader in a temporary file before it replaces the config file, so a value the next start would refuse never lands.
// key is poll_interval, offline, log_level or ui.<name>, value a string, a bool or a []string.
// The duration of poll_interval comes as the string the file takes, "2m", so the file reads as the user typed it.
func Set(path, key string, value any) (Config, error) {
	table, bare, found := strings.Cut(key, ".")
	if !found {
		table, bare = "", table
	}
	rendered, err := toml.Marshal(map[string]any{bare: value})
	if err != nil {
		return Config{}, fmt.Errorf("writing %s: %w", path, err)
	}
	line := strings.TrimRight(string(rendered), "\n")
	target := path
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		// A config kept in a dotfiles directory is a link, and a rename over the link would replace it with a plain file.
		if target, err = filepath.EvalSymlinks(path); err != nil {
			return Config{}, fmt.Errorf("writing %s: %w", path, err)
		}
	}
	old, err := os.ReadFile(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	text, err := editLines(string(old), table, bare, line)
	if err != nil {
		return Config{}, fmt.Errorf("set %s in %s by hand, %w", key, path, err)
	}
	mode := os.FileMode(0o600) // the file may hold a client secret
	if fi, err := os.Stat(target); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".config-*.toml")
	if err != nil {
		return Config{}, fmt.Errorf("writing %s: %w", path, err)
	}
	_, werr := tmp.WriteString(text)
	if err := errors.Join(werr, tmp.Close(), os.Chmod(tmp.Name(), mode)); err != nil {
		_ = os.Remove(tmp.Name())
		return Config{}, fmt.Errorf("writing %s: %w", path, err)
	}
	cfg, err := load(tmp.Name(), path)
	if err != nil {
		_ = os.Remove(tmp.Name())
		return Config{}, err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		_ = os.Remove(tmp.Name())
		return Config{}, fmt.Errorf("writing %s: %w", path, err)
	}
	return cfg, nil
}

// editLines replaces the key's line in its table, or adds one, and leaves every other line as it is.
// table is empty for the top level, line is the rendered "key = value".
// Headers and keys match the way the loader matches them, ignoring case, a replaced line keeps the spelling the file has.
// A key written twice, as a dotted key, in an inline table, over several lines or under an array of tables is errCannotEdit:
// the file after such an edit would not be what the user meant, so nothing guesses.
func editLines(text, table, key, line string) (string, error) {
	var lines []string
	if text != "" {
		lines = strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	}
	current := "" // the table the walk is in, empty before the first header
	tableAt, lastKey, found := -1, -1, -1
	for i, raw := range lines {
		l := strings.TrimSpace(raw)
		switch {
		case l == "" || strings.HasPrefix(l, "#"):
			continue
		case strings.HasPrefix(l, "[["):
			current = headerName(l[2:])
			if strings.EqualFold(current, table) {
				return "", errCannotEdit
			}
			continue
		case strings.HasPrefix(l, "["):
			current = headerName(l[1:])
			if strings.EqualFold(current, table) {
				tableAt = i
			}
			continue
		}
		k, rest, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		k = unquoteKey(k)
		switch {
		case strings.EqualFold(current, table) && strings.EqualFold(k, key):
			if found >= 0 {
				return "", errCannotEdit
			}
			found, lastKey = i, i
		case strings.EqualFold(current, table):
			lastKey = i
		case current == "" && table != "" && strings.EqualFold(k, table+"."+key):
			return "", errCannotEdit
		case current == "" && table != "" && strings.EqualFold(k, table) && strings.HasPrefix(strings.TrimSpace(rest), "{"):
			return "", errCannotEdit
		}
	}
	switch {
	case found >= 0:
		raw := strings.TrimRight(lines[found], "\r")
		eq := strings.Index(raw, "=")
		v := raw[eq+1:]
		end, complete := valueEnd(v)
		if !complete {
			return "", errCannotEdit
		}
		_, value, _ := strings.Cut(line, " = ")
		lead := v[:len(v)-len(strings.TrimLeft(v, " \t"))]
		edited := raw[:eq+1] + lead + value
		if comment := strings.TrimSpace(v[end:]); comment != "" {
			edited += " " + comment
		}
		lines[found] = edited
	case table == "" && lastKey >= 0:
		lines = append(lines[:lastKey+1], append([]string{line}, lines[lastKey+1:]...)...)
	case table == "":
		lines = append([]string{line}, lines...)
	case tableAt >= 0:
		at := max(lastKey, tableAt) + 1
		lines = append(lines[:at], append([]string{line}, lines[at:]...)...)
	default:
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "["+table+"]", line)
	}
	return strings.Join(lines, "\n") + "\n", nil
}

// headerName is the table name of a header line after its opening bracket, up to the closing one.
func headerName(rest string) string {
	name, _, _ := strings.Cut(rest, "]")
	return unquoteKey(name)
}

func unquoteKey(k string) string {
	k = strings.TrimSpace(k)
	if len(k) >= 2 && (k[0] == '"' || k[0] == '\'') && k[len(k)-1] == k[0] {
		return k[1 : len(k)-1]
	}
	return k
}

// valueEnd scans a TOML value that starts after the equals sign and reports where it ends, at a comment or the end of the line,
// and whether it ends on this line at all: a multi line string or an array left open continues below and cannot be replaced line by line.
func valueEnd(v string) (end int, complete bool) {
	body := strings.TrimLeft(v, " \t")
	if strings.HasPrefix(body, `"""`) || strings.HasPrefix(body, "'''") {
		return 0, false
	}
	off := len(v) - len(body)
	depth := 0
	var quote byte
	i := 0
scan:
	for ; i < len(body); i++ {
		c := body[i]
		switch {
		case quote == '"' && c == '\\':
			i++
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
		case c == '#':
			break scan
		}
	}
	return off + i, quote == 0 && depth == 0
}
