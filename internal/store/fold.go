package store

import (
	"database/sql/driver"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
	"modernc.org/sqlite"
)

// foldChains holds the chains that decompose a title, drop its combining marks and compose it again.
// A chain costs kilobytes to build and keeps state while it runs, so a fold borrows one and gives it back.
var foldChains = sync.Pool{New: func() any {
	return transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
}}

// FoldTitle lowercases s and strips combining marks, for the title lookups on the command line.
// The search index folds the same way (FTS5 unicode61 removes case and diacritics), so a word finds the same task in the interface and on the command line,
// and the exact match that settles an ambiguous fragment compares folded titles too, or a title typed without its accents would be refused.
// LIKE would not do, it ignores case for ASCII only.
// Letters that do not decompose, such as the Polish l with stroke, stay as they are in both places.
func FoldTitle(s string) string {
	ascii := true
	for i := 0; i < len(s) && ascii; i++ {
		ascii = s[i] < utf8.RuneSelf
	}
	lower := strings.ToLower(s)
	if ascii {
		// ASCII has no combining marks and decomposes to itself, the lowercase is the whole fold and most titles take this way.
		return lower
	}
	chain := foldChains.Get().(transform.Transformer)
	defer foldChains.Put(chain)
	// transform.String resets the chain first, so a fold never sees the state the one before left, see https://pkg.go.dev/golang.org/x/text/transform#String.
	out, _, err := transform.String(chain, lower)
	if err != nil {
		// The chain only fails on a broken transformer, the lowercased input is returned unfolded.
		return lower
	}
	return out
}

// title_contains(title, fragment) is 1 when the folded title holds the fragment, which the caller folded already.
// The title lookups match in SQL with it, so the order and the limit of a lookup apply to its matches in one statement.
// The driver's functions are global, a second registration is a programmer mistake.
func init() {
	if err := sqlite.RegisterDeterministicScalarFunction("title_contains", 2, titleContains); err != nil {
		panic(err)
	}
}

func titleContains(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	title, _ := args[0].(string)
	fragment, _ := args[1].(string)
	if strings.Contains(FoldTitle(title), fragment) {
		return int64(1), nil
	}
	return int64(0), nil
}
