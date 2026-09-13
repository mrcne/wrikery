package ui

import (
	"os"
	"strings"
	"testing"
)

func browserDescription(t *testing.T) (html, md string) {
	t.Helper()
	h, err := os.ReadFile("testdata/description_browser.html")
	if err != nil {
		t.Fatal(err)
	}
	m, err := os.ReadFile("testdata/description_browser.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(h), string(m)
}

func TestSplitDescriptionKeepsEveryByte(t *testing.T) {
	src, _ := browserDescription(t)
	blocks, trailing := splitDescription(src)
	var b strings.Builder
	for _, bl := range blocks {
		b.WriteString(bl.glue + bl.html)
	}
	b.WriteString(trailing)
	if b.String() != src {
		t.Errorf("the blocks and the glue do not add up to the source\n got: %s\nwant: %s", b.String(), src)
	}
	if len(blocks) != 14 {
		t.Fatalf("got %d blocks, want 14", len(blocks))
	}
	runs := 0
	for _, bl := range blocks {
		if bl.run {
			runs++
		}
	}
	if runs != 9 {
		t.Errorf("got %d runs, want 9", runs)
	}
	if blocks[0].glue != "<br />" || blocks[1].glue != "<br /><br />" || !strings.HasPrefix(blocks[1].html, "<h1>") {
		t.Errorf("leading blocks: %+v", blocks[:2])
	}
	if !strings.Contains(blocks[8].html, "<br /><a href") {
		t.Errorf("a single line break stays inside its run, got %q", blocks[8].html)
	}
	if trailing != "<br /><br /><br /><br /><br />" {
		t.Errorf("trailing = %q", trailing)
	}
}

func TestEditorTextOfABrowserDescription(t *testing.T) {
	src, want := browserDescription(t)
	if got := editorText(src); got != want {
		t.Errorf("editor text differs\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMergeUnchangedDescriptionWritesNothing(t *testing.T) {
	src, md := browserDescription(t)
	for _, edited := range []string{md, strings.TrimSuffix(md, "\n"), strings.ReplaceAll(md, "\n", "\r\n")} {
		if html, changed := mergeDescription(src, edited); changed {
			t.Errorf("an untouched file counts as a change, got %q", html)
		}
	}
}

func TestMergeReplacesOnlyTheEditedBlock(t *testing.T) {
	src, md := browserDescription(t)
	was := "Some paragraph with **bold text**, <u>underline</u>, *italics*, ~~strikethrough~~."
	edited := strings.Replace(md, was, strings.TrimSuffix(was, ".")+" edited.", 1)
	got, changed := mergeDescription(src, edited)
	if !changed {
		t.Fatal("the edit was not seen")
	}
	want := strings.Replace(src,
		"Some paragraph with <b>bold text</b>, <u>underline</u>, <i>italics</i>, <s>strikethrough</s>.",
		"Some paragraph with <strong>bold text</strong>, <u>underline</u>, <em>italics</em>, <s>strikethrough</s> edited.", 1)
	if got != want {
		t.Errorf("merged description differs\n got: %s\nwant: %s", got, want)
	}
}

func TestMergeInsertsAndDeletesBlocks(t *testing.T) {
	src, md := browserDescription(t)
	inserted := strings.Replace(md, "## Heading H2\n\n", "## Heading H2\n\nNew words here\n\n", 1)
	got, _ := mergeDescription(src, inserted)
	if !strings.Contains(got, "<h2>Heading H2</h2>New words here<br /><br />Some paragraph with <b>bold text</b>") {
		t.Errorf("inserted run is not separated from its neighbours as Wrike's editor would, got %s", got)
	}
	deleted := strings.Replace(md, "List of items:\n\n", "", 1)
	got, _ = mergeDescription(src, deleted)
	if !strings.Contains(got, "<s>strikethrough</s>.<br /><ul><li>item 1</li>") {
		t.Errorf("the list keeps its own line break after the deleted run, got %s", got)
	}
	if strings.Contains(got, "List of items") {
		t.Error("the deleted run is still there")
	}
}

func TestMergeOfAnEmptyDescriptionUsesParagraphs(t *testing.T) {
	got, changed := mergeDescription("", "hello\nthere\n\nworld\n")
	if !changed || got != "<p>hello<br />there</p><p>world</p>" {
		t.Errorf("got %q, changed %v", got, changed)
	}
}

func TestFindBlockNeedsBlankLinesAround(t *testing.T) {
	if at := findBlock("- a\n- b\n- c", "- a\n- b", 0); at != -1 {
		t.Errorf("a list that grew still matched at %d", at)
	}
	if at := findBlock("x\n\n- a\n- b\n\ny", "- a\n- b", 0); at != 3 {
		t.Errorf("got %d, want 3", at)
	}
	if at := findBlock("say a\n\na", "a", 0); at != 7 {
		t.Errorf("a word inside another block matched, got %d", at)
	}
}

func TestWrikeHTMLShapes(t *testing.T) {
	join := func(pieces []piece) string {
		var b strings.Builder
		for i, p := range pieces {
			if i > 0 {
				b.WriteString(separator(pieces[i-1].run, p.run))
			}
			b.WriteString(p.html)
		}
		return b.String()
	}
	cases := []struct {
		name, md string
		brForm   bool
		want     string
	}{
		{"line break in a paragraph", "a\nb", false, "<p>a<br />b</p>"},
		{"paragraph in a browser written description", "a\nb", true, "a<br />b"},
		{"heading then text", "# H\n\ntext", true, "<h1>H</h1>text"},
		{"two paragraphs", "a\n\nb", true, "a<br /><br />b"},
		{"code block lines become br tags", "```\nx\ny\n```", false, "<pre><code>x<br />y</code></pre>"},
		{"strike and underline", "~~x~~ and <u>y</u>", false, "<p><s>x</s> and <u>y</u></p>"},
		{"colored span survives", `<span style="color: rgb(255, 0, 0);">red</span> text`, true, `<span style="color: rgb(255, 0, 0);">red</span> text`},
		{"nested tight list", "- one\n- two\n  - nested", false, "<ul><li>one</li><li>two<ul><li>nested</li></ul></li></ul>"},
		{"checklist", "- [ ] open\n- [x] done", false,
			`<ul class="checklist" style="list-style-type: none;"><li><label><input type="checkbox" />open</label></li><li><label><input type="checkbox" checked="checked" />done</label></li></ul>`},
		{"table without header tags", "| a | b |\n| - | - |\n| 1 | 2 |", false, "<table><tr><td>a</td><td>b</td></tr><tr><td>1</td><td>2</td></tr></table>"},
		{"image alone on a line", `<img src="https://example.com/x.png" />`, true, `<img src="https://example.com/x.png" />`},
		{"comment dropped", "<!-- note -->\n\ntext", false, "<p>text</p>"},
	}
	for _, c := range cases {
		if got := join(wrikeHTML(c.md, c.brForm)); got != c.want {
			t.Errorf("%s:\n got: %s\nwant: %s", c.name, got, c.want)
		}
	}
}
