package store

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// foldTitle lowercases s and strips combining marks, for the title lookups on the command line.
// The search index folds the same way (FTS5 unicode61 removes case and diacritics), so a word finds the same task in the interface and on the command line.
// LIKE would not do, it ignores case for ASCII only.
// Letters that do not decompose, such as the Polish l with stroke, stay as they are in both places.
func foldTitle(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, strings.ToLower(s))
	if err != nil {
		// The chain only fails on a broken transformer, the lowercased input is returned unfolded.
		return strings.ToLower(s)
	}
	return out
}
