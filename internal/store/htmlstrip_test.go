package store

import "testing"

func TestStripHTML(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain text passes", "just words", "just words"},
		{"tags dropped", "<p>hello <b>world</b></p>", "hello world"},
		{"tags become word boundaries", "one<br/>two", "one two"},
		{"entities decoded", "a &amp; b stays &lt;ok&gt;", "a & b stays <ok>"},
		{"quotes and nbsp", "&quot;x&quot;&nbsp;&#39;y&#39;", `"x" 'y'`},
		{"whitespace collapsed", "  a \n\t b  ", "a b"},
		{"unclosed tag dropped", "text <a href", "text"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripHTML(c.in); got != c.want {
				t.Errorf("stripHTML(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
