package ui

import (
	"bytes"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// A description is edited as markdown and stored as the HTML Wrike keeps, and the two do not map one to one.
// Underline, colors, checklists and mentions have no markdown, and Wrike's own editor writes plain text with br tags instead of paragraphs.
// So the editor works on blocks: the description is cut into top level blocks, each becomes a piece of markdown,
// and on the way back a block whose markdown did not change keeps the bytes it came with.
// Only the blocks the user touched go through markdown to HTML.

// descBlock is one top level piece of a description: a block element, or a run of inline content between two line breaks.
type descBlock struct {
	glue string // what sat in front of the block, stray br tags and whitespace, kept as they were
	html string
	md   string
	run  bool
}

var blockTags = map[string]bool{
	"p": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "pre": true, "table": true, "blockquote": true, "div": true, "hr": true,
}

var voidTags = map[string]bool{"br": true, "img": true, "input": true, "hr": true}

type itemKind int

const (
	itemInline itemKind = iota
	itemBlock
	itemBr
	itemSpace
)

type descItem struct {
	raw  string
	kind itemKind
}

// topLevelItems cuts the description into its top level tokens without re-serializing anything.
// The tokenizer hands out the raw bytes of every token, so a block that is not edited goes back exactly as Wrike stored it,
// entities and self closing slashes included, which a parse and render round trip would not keep.
func topLevelItems(s string) []descItem {
	var items []descItem
	z := html.NewTokenizer(strings.NewReader(s))
	depth := 0
	var buf strings.Builder
	open := itemInline
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		raw := string(z.Raw())
		name, _ := z.TagName()
		tag := string(name)
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			closed := tt == html.SelfClosingTagToken || voidTags[tag]
			if depth > 0 {
				buf.WriteString(raw)
				if !closed {
					depth++
				}
				continue
			}
			switch {
			case tag == "br":
				items = append(items, descItem{raw, itemBr})
			case closed:
				kind := itemInline
				if blockTags[tag] {
					kind = itemBlock
				}
				items = append(items, descItem{raw, kind})
			default:
				depth = 1
				buf.Reset()
				buf.WriteString(raw)
				open = itemInline
				if blockTags[tag] {
					open = itemBlock
				}
			}
		case html.EndTagToken:
			if depth == 0 {
				items = append(items, descItem{raw, itemInline})
				continue
			}
			buf.WriteString(raw)
			depth--
			if depth == 0 {
				items = append(items, descItem{buf.String(), open})
			}
		case html.TextToken:
			if depth > 0 {
				buf.WriteString(raw)
			} else if strings.TrimSpace(raw) == "" {
				items = append(items, descItem{raw, itemSpace})
			} else {
				items = append(items, descItem{raw, itemInline})
			}
		default:
			if depth > 0 {
				buf.WriteString(raw)
			} else {
				items = append(items, descItem{raw, itemSpace})
			}
		}
	}
	if depth > 0 {
		items = append(items, descItem{buf.String(), open})
	}
	return items
}

// splitDescription groups the top level tokens into blocks.
// A block element is a block of its own.
// Inline content forms a run, and a run ends at a block element or at two line breaks in a row, which is how Wrike's editor separates paragraphs.
// A single br followed by more inline content is a line break inside the run.
// Line breaks that end a run, and whitespace around them, are glue: they are kept in front of the block that follows.
// trailing is the glue after the last block.
func splitDescription(s string) (blocks []descBlock, trailing string) {
	items := topLevelItems(s)
	var glue, run strings.Builder
	hasText := false
	flush := func() {
		if hasText {
			blocks = append(blocks, descBlock{glue: glue.String(), html: run.String(), run: true})
			glue.Reset()
		} else {
			glue.WriteString(run.String())
		}
		run.Reset()
		hasText = false
	}
	for i := 0; i < len(items); i++ {
		it := items[i]
		switch it.kind {
		case itemBlock:
			flush()
			blocks = append(blocks, descBlock{glue: glue.String(), html: it.raw})
			glue.Reset()
		case itemBr:
			j, breaks := i, 0
			var seq strings.Builder
			for j < len(items) && (items[j].kind == itemBr || items[j].kind == itemSpace) {
				seq.WriteString(items[j].raw)
				if items[j].kind == itemBr {
					breaks++
				}
				j++
			}
			followsInline := j < len(items) && items[j].kind == itemInline
			if hasText && breaks == 1 && followsInline {
				run.WriteString(seq.String())
			} else {
				flush()
				glue.WriteString(seq.String())
			}
			i = j - 1
		case itemSpace:
			if hasText {
				run.WriteString(it.raw)
			} else {
				glue.WriteString(it.raw)
			}
		case itemInline:
			run.WriteString(it.raw)
			hasText = true
		}
	}
	flush()
	trailing = glue.String()
	// A block with nothing to edit, an empty paragraph for example, is glue for its neighbour.
	kept := blocks[:0]
	carry := ""
	for _, b := range blocks {
		b.md = editorMarkdown(b.html)
		if b.md == "" {
			carry += b.glue + b.html
			continue
		}
		b.glue = carry + b.glue
		carry = ""
		kept = append(kept, b)
	}
	return kept, carry + trailing
}

// editorText is what the editor opens on: the blocks as markdown, a blank line between them.
func editorText(description string) string {
	blocks, _ := splitDescription(description)
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		parts = append(parts, b.md)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n") + "\n"
}

// normalizeEdited makes the saved file comparable with the markdown it was opened with.
// Editors add or drop the final newline, strip trailing spaces and write a byte order mark on save, none of which is an edit.
func normalizeEdited(s string) string {
	s = strings.TrimPrefix(s, "\uFEFF")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Join(lines, "\n")
}

// findBlock looks for md in text at or after from, standing on its own: nothing but blank lines before and after it.
func findBlock(text, md string, from int) int {
	for {
		i := strings.Index(text[from:], md)
		if i < 0 {
			return -1
		}
		i += from
		before, after := text[:i], text[i+len(md):]
		if (strings.TrimSpace(before) == "" || strings.HasSuffix(strings.TrimRight(before, " \t"), "\n\n")) &&
			(strings.TrimSpace(after) == "" || strings.HasPrefix(strings.TrimLeft(after, " \t"), "\n\n")) {
			return i
		}
		from = i + 1
	}
}

type piece struct {
	html string
	run  bool
	idx  int // index of the block the piece came from unchanged, -1 for a converted one
}

// mergeDescription rebuilds the description from the saved file.
// Blocks whose markdown still stands in the file keep their HTML and their place, the text between them is converted.
// changed is false when every block is still there in its order and nothing was added, and when what is left renders to nothing.
func mergeDescription(description, edited string) (merged string, changed bool) {
	blocks, trailing := splitDescription(description)
	text := normalizeEdited(edited)
	// A description with no paragraph elements came from Wrike's editor, and a new one is written with paragraphs.
	brForm := len(blocks) > 0
	for _, b := range blocks {
		if l := strings.ToLower(b.html); !b.run && (strings.HasPrefix(l, "<p>") || strings.HasPrefix(l, "<p ")) {
			brForm = false
		}
	}
	var pieces []piece
	pos := 0
	convert := func(md string) {
		if strings.TrimSpace(md) == "" {
			return
		}
		changed = true
		pieces = append(pieces, wrikeHTML(md, brForm)...)
	}
	for i, b := range blocks {
		at := findBlock(text, normalizeEdited(b.md), pos)
		if at < 0 {
			changed = true
			continue
		}
		convert(text[pos:at])
		pieces = append(pieces, piece{html: b.html, run: b.run, idx: i})
		pos = at + len(normalizeEdited(b.md))
	}
	convert(text[pos:])
	if !changed {
		return "", false
	}
	// A kept block brings its own glue, unless it is too little for its new neighbour: two runs of text with no break between them would merge.
	var out strings.Builder
	for i, p := range pieces {
		switch {
		case p.idx >= 0 && (i > 0 || p.idx == 0):
			glue := blocks[p.idx].glue
			if i > 0 {
				if need := separator(pieces[i-1].run, p.run); strings.Count(glue, "<br") < strings.Count(need, "<br") {
					glue = need
				}
			}
			out.WriteString(glue)
		case i > 0:
			out.WriteString(separator(pieces[i-1].run, p.run))
		}
		out.WriteString(p.html)
	}
	if n := len(pieces); len(blocks) > 0 && n > 0 && pieces[n-1].idx == len(blocks)-1 {
		out.WriteString(trailing)
	}
	// A comment or a link reference on its own renders to nothing, which is an empty file and not a change.
	if strings.TrimSpace(out.String()) == "" {
		return "", false
	}
	return out.String(), true
}

// separator is what Wrike's editor puts between two neighbours: a blank line between two runs of text,
// one line break where a run leads into a block element, and nothing after a block element, which ends its line by itself.
func separator(prevRun, nextRun bool) string {
	switch {
	case prevRun && nextRun:
		return "<br /><br />"
	case prevRun:
		return "<br />"
	}
	return ""
}

func newEditorConverter() *converter.Converter {
	conv := converter.NewConverter(converter.WithPlugins(base.NewBasePlugin(), commonmark.NewCommonmarkPlugin(), strikethrough.NewStrikethroughPlugin()))
	// Markdown has no underline and no colored text, the tags stay in the file as they are and go back the same way.
	for _, tag := range []string{"u", "span", "font"} {
		conv.Register.RendererFor(tag, converter.TagTypeInline, renderTagAround, converter.PriorityEarly)
	}
	conv.Register.RendererFor("img", converter.TagTypeInline, renderVerbatim, converter.PriorityEarly)
	conv.Register.RendererFor("table", converter.TagTypeBlock, renderVerbatim, converter.PriorityEarly)
	// A mention is an anchor without an href whose class and rel carry the contact, so it stays as it is instead of turning into an empty link.
	conv.Register.RendererFor("a", converter.TagTypeInline, func(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
		if hasAttr(n, "href") && !strings.Contains(attr(n, "class"), "stream-user-id") {
			return converter.RenderTryNext
		}
		return renderTagAround(ctx, w, n)
	}, converter.PriorityEarly)
	conv.Register.RendererFor("input", converter.TagTypeInline, renderCheckbox, converter.PriorityEarly)
	// A newline in the editor is a line break, so a br is written as one instead of the two trailing spaces most editors strip on save.
	conv.Register.RendererFor("br", converter.TagTypeInline, func(_ converter.Context, w converter.Writer, _ *html.Node) converter.RenderStatus {
		_, _ = w.WriteString("\n")
		return converter.RenderSuccess
	}, converter.PriorityEarly)
	return conv
}

func renderTagAround(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	_, _ = w.WriteString(openTag(n))
	ctx.RenderChildNodes(ctx, w, n)
	_, _ = w.WriteString("</" + n.Data + ">")
	return converter.RenderSuccess
}

func renderVerbatim(_ converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	_, _ = w.WriteString(renderHTML(n))
	return converter.RenderSuccess
}

// renderCheckbox writes a task list box for the input Wrike's checklist items carry.
func renderCheckbox(_ converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
	if attr(n, "type") != "checkbox" {
		return converter.RenderTryNext
	}
	if hasAttr(n, "checked") {
		_, _ = w.WriteString("[x] ")
	} else {
		_, _ = w.WriteString("[ ] ")
	}
	return converter.RenderSuccess
}

func openTag(n *html.Node) string {
	var b strings.Builder
	b.WriteString("<" + n.Data)
	for _, a := range n.Attr {
		b.WriteString(" " + a.Key + `="` + html.EscapeString(a.Val) + `"`)
	}
	b.WriteString(">")
	return b.String()
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

// editorMarkdown turns one block of Wrike HTML into the markdown the editor shows for it.
func editorMarkdown(fragment string) string {
	md, err := newEditorConverter().ConvertString(fragment)
	if err != nil {
		return strings.TrimSpace(fragment)
	}
	return strings.TrimSpace(md)
}

func parseFragment(s string) []*html.Node {
	nodes, err := html.ParseFragment(strings.NewReader(s), &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil {
		return nil
	}
	return nodes
}

func renderHTML(n *html.Node) string {
	var b bytes.Buffer
	_ = html.Render(&b, n)
	// The renderer closes void elements as <br/>, Wrike writes <br />, and a slash before a closing bracket occurs nowhere else in rendered output.
	return strings.ReplaceAll(b.String(), "/>", " />")
}

func innerHTML(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(renderHTML(c))
	}
	return b.String()
}

// wrikeHTML converts markdown to the HTML Wrike stores, as pieces the caller joins with separators.
// In a description Wrike's editor wrote, brForm, a paragraph becomes a run of text like its neighbours instead of a p element.
func wrikeHTML(md string, brForm bool) []piece {
	gm := goldmark.New(
		goldmark.WithExtensions(extension.Strikethrough, extension.TaskList, extension.Table),
		goldmark.WithRendererOptions(gmhtml.WithHardWraps(), gmhtml.WithUnsafe()),
	)
	var out bytes.Buffer
	// Convert only fails on the writer, and a buffer does not.
	_ = gm.Convert([]byte(md), &out)
	var pieces []piece
	var run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			pieces = append(pieces, piece{html: run.String(), run: true, idx: -1})
			run.Reset()
		}
	}
	// The nodes hang under a body of their own so the fix sees the top level ones the way it sees every other child.
	body := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	for _, n := range parseFragment(out.String()) {
		body.AppendChild(n)
	}
	fixForWrike(body, false)
	for n := body.FirstChild; n != nil; n = n.NextSibling {
		switch n.Type {
		case html.TextNode:
			if strings.TrimSpace(n.Data) != "" {
				run.WriteString(html.EscapeString(n.Data))
			}
		case html.ElementNode:
			if !blockTags[n.Data] {
				run.WriteString(renderHTML(n))
				continue
			}
			flush()
			if brForm && n.Data == "p" {
				pieces = append(pieces, piece{html: innerHTML(n), run: true, idx: -1})
				continue
			}
			pieces = append(pieces, piece{html: renderHTML(n), idx: -1})
		}
	}
	flush()
	return pieces
}

// fixForWrike reshapes goldmark's output into what Wrike keeps, checked against the API on a live account:
// every raw newline is dropped on the way in, so a code block keeps its lines only as br tags,
// del and th are not kept, thead and tbody lose their cells, and a checklist has a shape of its own.
func fixForWrike(n *html.Node, inPre bool) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		switch c.Type {
		case html.CommentNode:
			n.RemoveChild(c)
		case html.TextNode:
			if inPre {
				lines := strings.Split(c.Data, "\n")
				if next == nil && len(lines) > 1 && lines[len(lines)-1] == "" {
					// goldmark ends a code block with a newline that is not a line of its own.
					lines = lines[:len(lines)-1]
				}
				c.Data = lines[0]
				for _, l := range lines[1:] {
					n.InsertBefore(&html.Node{Type: html.ElementNode, Data: "br", DataAtom: atom.Br}, next)
					n.InsertBefore(&html.Node{Type: html.TextNode, Data: l}, next)
				}
			} else {
				c.Data = strings.ReplaceAll(c.Data, "\n", "")
				if c.Data == "" {
					n.RemoveChild(c)
				}
			}
		case html.ElementNode:
			switch c.Data {
			case "del":
				c.Data, c.DataAtom = "s", atom.S
			case "th":
				c.Data, c.DataAtom = "td", atom.Td
			case "thead", "tbody":
				first := c.FirstChild
				for gc := c.FirstChild; gc != nil; gc = c.FirstChild {
					c.RemoveChild(gc)
					n.InsertBefore(gc, c)
				}
				n.RemoveChild(c)
				if first != nil {
					next = first
				}
				c = next
				continue
			}
			fixForWrike(c, inPre || c.Data == "pre")
			switch c.Data {
			case "li":
				fixListItem(c)
			case "ul":
				if isChecklist(c) {
					c.Attr = []html.Attribute{{Key: "class", Val: "checklist"}, {Key: "style", Val: "list-style-type: none;"}}
				}
			}
		}
		c = next
	}
}

// fixListItem unwraps the paragraphs goldmark puts into a loose item, and gives a task list item Wrike's checklist shape:
// the text inside a label, after an input the browser can tick.
func fixListItem(li *html.Node) {
	lastWasP := false
	for c := li.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.ElementNode && c.Data == "p" {
			if lastWasP {
				li.InsertBefore(&html.Node{Type: html.ElementNode, Data: "br", DataAtom: atom.Br}, c)
			}
			for gc := c.FirstChild; gc != nil; gc = c.FirstChild {
				c.RemoveChild(gc)
				li.InsertBefore(gc, c)
			}
			li.RemoveChild(c)
			lastWasP = true
		} else {
			lastWasP = false
		}
		c = next
	}
	box := li.FirstChild
	if box == nil || box.Type != html.ElementNode || box.Data != "input" || attr(box, "type") != "checkbox" {
		return
	}
	checked := hasAttr(box, "checked")
	li.RemoveChild(box)
	if t := li.FirstChild; t != nil && t.Type == html.TextNode {
		t.Data = strings.TrimPrefix(t.Data, " ")
	}
	input := &html.Node{Type: html.ElementNode, Data: "input", DataAtom: atom.Input, Attr: []html.Attribute{{Key: "type", Val: "checkbox"}}}
	if checked {
		input.Attr = append(input.Attr, html.Attribute{Key: "checked", Val: "checked"})
	}
	label := &html.Node{Type: html.ElementNode, Data: "label", DataAtom: atom.Label}
	label.AppendChild(input)
	for c := li.FirstChild; c != nil; c = li.FirstChild {
		li.RemoveChild(c)
		label.AppendChild(c)
	}
	li.AppendChild(label)
}

func isChecklist(ul *html.Node) bool {
	for li := ul.FirstChild; li != nil; li = li.NextSibling {
		if li.Type != html.ElementNode || li.Data != "li" {
			continue
		}
		if l := li.FirstChild; l != nil && l.Type == html.ElementNode && l.Data == "label" {
			if in := l.FirstChild; in != nil && in.Type == html.ElementNode && in.Data == "input" {
				return true
			}
		}
	}
	return false
}

// newDisplayConverter turns Wrike's HTML into the markdown glamour renders in the detail pane.
// Strike-through goes through as markdown, glamour draws it, underline goes through as markers, see underlineMarks, and a checklist item shows its box.
func newDisplayConverter() *converter.Converter {
	conv := converter.NewConverter(converter.WithPlugins(base.NewBasePlugin(), commonmark.NewCommonmarkPlugin(), strikethrough.NewStrikethroughPlugin()))
	conv.Register.RendererFor("u", converter.TagTypeInline, func(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
		_, _ = w.WriteString(underlineOn)
		ctx.RenderChildNodes(ctx, w, n)
		_, _ = w.WriteString(underlineOff)
		return converter.RenderSuccess
	}, converter.PriorityEarly)
	conv.Register.RendererFor("input", converter.TagTypeInline, renderCheckbox, converter.PriorityEarly)
	return conv
}
