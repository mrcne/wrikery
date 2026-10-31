package store

import (
	stdhtml "html"
	"strings"

	"golang.org/x/net/html"
)

// StripHTML reduces Wrike description HTML to plain text, for the search index and for the one line
// the changes screen shows of a description.
// It drops tags, decodes the entities Wrike emits and collapses whitespace.
// Search only needs the words, so this does not try to be a real HTML parser.
func StripHTML(s string) string {
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
	// The API encodes the @ of a mention as &#64; in the HTML form, the decode is what makes it read as one.
	return strings.Join(strings.Fields(stdhtml.UnescapeString(b.String())), " ")
}

// CommentText reduces the HTML form of a comment to the text the thread shows.
// It is not StripHTML: the thread keeps the line breaks the plain form of the API kept, a <br> is a newline there
// (checked on the live account), where the search index wants one line.
// A link shows its text and then its address, or the address alone when the text is the address, the rule of the descriptions,
// the plain form of the API showed the address alone.
// A quote reads as lines after a > and a list as lines after a dash, where that plain form ran the items together.
// The tokenizer takes an attribute with a > in its value and an upper case tag, a hand written scan did not.
func CommentText(s string) string {
	var w commentWriter
	tz := html.NewTokenizer(strings.NewReader(s))
	var link *strings.Builder
	var href string
	for {
		switch tz.Next() {
		case html.ErrorToken:
			return w.finish()
		case html.TextToken:
			if link != nil {
				link.WriteString(tz.Token().Data)
			} else {
				w.text(tz.Token().Data)
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := tz.Token()
			switch tok.Data {
			case "br", "ul", "ol":
				w.newline()
			case "li":
				w.text("- ")
			case "blockquote":
				w.newline()
				w.quote++
			case "a":
				link, href = &strings.Builder{}, ""
				for _, a := range tok.Attr {
					if a.Key == "href" {
						href = strings.TrimSpace(a.Val)
					}
				}
			}
		case html.EndTagToken:
			switch tz.Token().Data {
			case "li", "p", "div":
				w.newline()
			case "blockquote":
				w.newline()
				w.quote = max(w.quote-1, 0)
			case "a":
				if link == nil {
					continue
				}
				w.text(link.String())
				if href != "" && strings.TrimSuffix(strings.TrimSpace(link.String()), "/") != strings.TrimSuffix(href, "/") {
					w.text(" " + href)
				}
				link = nil
			}
		}
	}
}

// commentWriter builds the text line by line, so a quote can mark the start of each of its lines.
type commentWriter struct {
	b         strings.Builder
	lineStart bool
	quote     int
}

func (w *commentWriter) newline() {
	w.b.WriteByte('\n')
	w.lineStart = true
}

func (w *commentWriter) text(s string) {
	for i, part := range strings.Split(s, "\n") {
		if i > 0 {
			w.newline()
		}
		if part == "" {
			continue
		}
		if w.lineStart && w.quote > 0 {
			w.b.WriteString("> ")
		}
		w.b.WriteString(part)
		w.lineStart = false
	}
}

// finish collapses the spaces inside each line and the blank lines between them, and trims the whole.
func (w *commentWriter) finish() string {
	lines := strings.Split(w.b.String(), "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	out := strings.Join(lines, "\n")
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(out)
}
