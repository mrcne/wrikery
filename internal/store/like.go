package store

import "strings"

// likePattern builds the argument for `LIKE ? ESCAPE '\'`: the fragment lowered and wrapped in wildcards,
// with the three characters LIKE would otherwise interpret escaped.
// SQLite's LIKE folds case for ASCII only, the same holds for lower(), which is enough for a title fragment.
func likePattern(fragment string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(strings.ToLower(fragment)) + "%"
}
