package store

import "testing"

func TestStripHTML(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain text passes", "just words", "just words"},
		{"tags dropped", "<p>hello <b>world</b></p>", "hello world"},
		{"tags become word boundaries", "one<br/>two", "one two"},
		{"entities decoded", "a &amp; b stays &lt;ok&gt;", "a & b stays <ok>"},
		{"quotes and nbsp", "&quot;x&quot;&nbsp;&#39;y&#39;", `"x" 'y'`},
		{"numeric entities", `<a rel="&#64;assignees">&#64;assignees</a> &#x27;quoted&#x27; &#junk;`, "@assignees 'quoted' &#junk;"},
		{"an escaped entity decodes once", "&amp;#64; stays", "&#64; stays"},
		{"whitespace collapsed", "  a \n\t b  ", "a b"},
		{"unclosed tag dropped", "text <a href", "text"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StripHTML(c.in); got != c.want {
				t.Errorf("StripHTML(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// The thread keeps the line breaks the plain form of the API kept, a <br> is a newline there,
// and a list reads as lines where that form ran the items together (checked on the live account).
func TestCommentTextKeepsTheLineBreaks(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"br and list", "first line<br />second line<ul><li>item one</li><li>item two</li></ul>last", "first line\nsecond line\n- item one\n- item two\nlast"},
		{"mention anchor", `<a class="stream-user-id avatar" rel="U1">&#64;Ada Nowak</a> please look`, "@Ada Nowak please look"},
		{"inline tags and blank lines", "a  <b>bold</b>   word<br><br><br>after", "a bold word\n\nafter"},
		{"entities and a literal newline", "x &lt; y &amp; z\nsecond", "x < y & z\nsecond"},
		{"a link keeps its address", `see <a href="https://example.com/doc">the doc</a> now`, "see the doc https://example.com/doc now"},
		{"a pasted address once", `<a href="https://example.com/raw">https://example.com/raw</a>`, "https://example.com/raw"},
		{"a quote", "before<blockquote>quoted words<br>twice</blockquote>after", "before\n> quoted words\n> twice\nafter"},
		{"a tag with a > in a value", `<a href="https://example.com/?q=a>b" title="x>y">link</a> tail`, "link https://example.com/?q=a>b tail"},
		{"upper case break", "one<BR>two", "one\ntwo"},
		{"padding", "  padded  ", "padded"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CommentText(c.in); got != c.want {
				t.Errorf("CommentText(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
