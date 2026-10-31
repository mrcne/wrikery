package store

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// titleFolder folds titles with one transformer chain.
// A lookup folds every title of the cache, and the chain costs more to build than to run,
// six times the time and seventeen times the bytes of folding a title with it at sixty thousand tasks.
type titleFolder struct {
	chain transform.Transformer
}

func newTitleFolder() titleFolder {
	return titleFolder{chain: transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)}
}

// fold lowercases s and strips combining marks.
// transform.String resets the chain first, so a fold never sees the state the one before left, see https://pkg.go.dev/golang.org/x/text/transform#String.
func (f titleFolder) fold(s string) string {
	out, _, err := transform.String(f.chain, strings.ToLower(s))
	if err != nil {
		// The chain only fails on a broken transformer, the lowercased input is returned unfolded.
		return strings.ToLower(s)
	}
	return out
}

// FoldTitle lowercases s and strips combining marks, for the title lookups on the command line.
// The search index folds the same way (FTS5 unicode61 removes case and diacritics), so a word finds the same task in the interface and on the command line,
// and the exact match that settles an ambiguous fragment compares folded titles too, or a title typed without its accents would be refused.
// LIKE would not do, it ignores case for ASCII only.
// Letters that do not decompose, such as the Polish l with stroke, stay as they are in both places.
// It builds the chain for the one string, a loop over many titles uses a titleFolder.
func FoldTitle(s string) string {
	return newTitleFolder().fold(s)
}
