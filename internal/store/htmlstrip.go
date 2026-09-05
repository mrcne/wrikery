package store

import "strings"

var entityReplacer = strings.NewReplacer(
	"&nbsp;", " ",
	"&amp;", "&",
	"&lt;", "<",
	"&gt;", ">",
	"&quot;", `"`,
	"&#39;", "'",
)

// stripHTML reduces Wrike description HTML to plain text for the search index.
// It drops tags, decodes the few entities Wrike emits and collapses whitespace.
// Search only needs the words, so this does not try to be a real HTML parser.
func stripHTML(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inTag := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '<':
			inTag = true
			b.WriteByte(' ')
		case c == '>':
			inTag = false
		case inTag:
		default:
			b.WriteByte(c)
		}
	}
	out := entityReplacer.Replace(b.String())
	return strings.Join(strings.Fields(out), " ")
}
