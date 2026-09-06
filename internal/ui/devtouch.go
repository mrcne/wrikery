package ui

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/mrcne/wrikery/internal/store"
)

var (
	permalinkID = regexp.MustCompile(`[?&]id=(\d+)`)
	nonSlug     = regexp.MustCompile(`[^a-z0-9]+`)
)

// transliterations holds the letters that NFD does not split into a base letter plus a combining mark,
// so they would otherwise fall through to the nonSlug collapse and vanish from the branch name.
// Only the lowercase forms are listed, the title is lowercased before it gets here.
var transliterations = map[rune]string{
	'ł': "l",  // l with stroke
	'ß': "ss", // sharp s
	'ø': "o",  // o with stroke
	'æ': "ae", // ae
	'œ': "oe", // oe
	'đ': "d",  // d with stroke
}

// taskNumber is the numeric id from the permalink, what the web app shows and what people say out loud.
func taskNumber(permalink string) string {
	m := permalinkID.FindStringSubmatch(permalink)
	if m == nil {
		return ""
	}
	return m[1]
}

func slugify(title string, limit int) string {
	// Lowercasing first is what covers the uppercase forms of the transliterated letters, NFD does not decompose them either.
	s := nonSlug.ReplaceAllString(transliterate(strings.ToLower(title)), "-")
	s = strings.Trim(s, "-")
	if len(s) > limit {
		s = strings.Trim(s[:limit], "-")
	}
	return s
}

// transliterate turns letters with diacritics into their plain ASCII base,
// so a title such as "Zazolc gesla jazn" keeps its words instead of losing every accented letter to the nonSlug collapse.
// NFD splits most of them into a base letter plus a combining mark, dropped here,
// and the handful that NFD does not split, such as the Polish l with stroke, go through the transliterations table.
func transliterate(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if mapped, ok := transliterations[r]; ok {
			b.WriteString(mapped)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// branchName fills the config template.
// {id} is the permalink number, or the API id when there is no permalink.
func branchName(tmpl string, task store.Task) string {
	id := taskNumber(task.Permalink)
	if id == "" {
		id = task.ID
	}
	return strings.NewReplacer("{id}", id, "{slug}", slugify(task.Title, 40)).Replace(tmpl)
}
