package store

import "strings"

// likePattern builds the argument for `LIKE ? ESCAPE '\'`: the fragment with ASCII letters lowercased and wrapped in wildcards,
// with the three characters LIKE would otherwise interpret escaped.
// The fragment is lowercased for ASCII letters only ('A'..'Z' to 'a'..'z'), matching how SQLite's lower() and LIKE fold case.
// Non-ASCII letters are not folded and must match exactly as stored.
func likePattern(fragment string) string {
	// Lowercase ASCII letters only, matching SQLite's case folding.
	var buf strings.Builder
	for i := 0; i < len(fragment); i++ {
		b := fragment[i]
		if b >= 'A' && b <= 'Z' {
			buf.WriteByte(b + ('a' - 'A'))
		} else {
			buf.WriteByte(b)
		}
	}
	lowered := buf.String()
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(lowered) + "%"
}
