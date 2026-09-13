package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// displayTitle drops a configured prefix from what rows and cards draw, a project code every task starts with for example.
// The detail pane keeps the full title, this is for the places where width is short.
func displayTitle(title string, hide []string) string {
	for _, p := range hide {
		if p == "" || !strings.HasPrefix(title, p) {
			continue
		}
		rest := title[len(p):]
		if rest == "" || rest[0] == ' ' {
			return strings.TrimLeft(rest, " ")
		}
	}
	return title
}

// titleSpans finds where a project code ends and where the part prefixes end, as rune offsets into the title.
// A code is a short word in parentheses at the start, "(MX) ". A part prefix is a segment of up to three words
// closed by ": " or " - ", several can chain, and something has to follow the last one.
// The shapes are how titles are written on a real account, "(MX) Backend: Kafka - Processing", a title that fits neither gets 0, 0.
func titleSpans(title string) (codeEnd, prefixEnd int) {
	runes := []rune(title)
	if len(runes) > 2 && runes[0] == '(' {
		if closing := strings.IndexRune(title, ')'); closing > 1 && closing <= 9 && !strings.ContainsAny(title[1:closing], " ()") {
			after := len([]rune(title[:closing+1]))
			if after < len(runes) && runes[after] == ' ' {
				codeEnd = after + 1
			}
		}
	}
	prefixEnd = codeEnd
	for {
		rest := runes[prefixEnd:]
		at, sepLen := findSeparator(rest)
		if at < 0 || at+sepLen >= len(rest) {
			break
		}
		segment := strings.TrimSpace(string(rest[:at]))
		if segment == "" || len(strings.Fields(segment)) > 3 || len([]rune(segment)) > 24 {
			break
		}
		prefixEnd += at + sepLen
	}
	return codeEnd, prefixEnd
}

// findSeparator is the first ": " or " - " in the runes, as its start and its length, or -1.
func findSeparator(rs []rune) (int, int) {
	for i := range rs {
		if rs[i] == ':' && i+1 < len(rs) && rs[i+1] == ' ' {
			return i, 2
		}
		if rs[i] == ' ' && i+2 < len(rs) && rs[i+1] == '-' && rs[i+2] == ' ' {
			return i, 3
		}
	}
	return -1, 0
}

// titleLine is one drawn line of a wrapped title and where it starts in the title, so the spans can be styled per line.
type titleLine struct {
	text  string
	start int
}

// wrapTitle breaks a title over one or two lines of width, at spaces, a word longer than the width cut into pieces.
// The first line ends at the last part prefix separator that sits in its second half, when there is one,
// so a card reads as a heading and a detail and not as a sentence cut anywhere.
// When the title goes on past the second line, that line ends with two dots.
func wrapTitle(title string, width int) []titleLine {
	width = max(width, 1)
	type word struct {
		text  string
		start int
	}
	var words []word
	runes := []rune(title)
	for i := 0; i < len(runes); {
		if runes[i] == ' ' {
			i++
			continue
		}
		j := i
		for j < len(runes) && runes[j] != ' ' {
			j++
		}
		for k := i; k < j; k += width {
			words = append(words, word{text: string(runes[k:min(k+width, j)]), start: k})
		}
		i = j
	}
	if len(words) == 0 {
		return []titleLine{{}}
	}
	join := func(ws []word) string {
		parts := make([]string, 0, len(ws))
		for _, w := range ws {
			parts = append(parts, w.text)
		}
		return strings.Join(parts, " ")
	}
	// fit is the index after the last word that fits on a line starting at from, at least one word.
	fit := func(from int) int {
		used, n := 0, from
		for n < len(words) {
			add := ansi.StringWidth(words[n].text)
			if n > from {
				add++
			}
			if used+add > width {
				break
			}
			used += add
			n++
		}
		return max(n, from+1)
	}
	end1 := fit(0)
	if end1 >= len(words) {
		return []titleLine{{text: join(words), start: 0}}
	}
	for j := end1 - 1; j > 0; j-- {
		if t := words[j].text; t != "-" && !strings.HasSuffix(t, ":") {
			continue
		}
		if ansi.StringWidth(join(words[:j+1]))*2 >= width {
			end1 = j + 1
		}
		break
	}
	end2 := fit(end1)
	second := join(words[end1:end2])
	if end2 < len(words) {
		second = strings.TrimRight(ansi.Truncate(second, width-2, ""), " ") + ".."
	}
	return []titleLine{{text: join(words[:end1]), start: 0}, {text: second, start: words[end1].start}}
}

// styleTitle draws one line of a title: the code muted, the part prefixes dim, the rest in the text color.
func (t Theme) styleTitle(line titleLine, codeEnd, prefixEnd int) string {
	runes := []rune(line.text)
	cut := func(end int) int { return min(max(end-line.start, 0), len(runes)) }
	c, p := cut(codeEnd), cut(prefixEnd)
	var b strings.Builder
	if c > 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(t.Muted).Render(string(runes[:c])))
	}
	if p > c {
		b.WriteString(lipgloss.NewStyle().Foreground(t.Dim).Render(string(runes[c:p])))
	}
	b.WriteString(string(runes[p:]))
	return b.String()
}

// styledTitle is the whole title on one line for the list and search rows, the configured codes dropped.
func (t Theme) styledTitle(title string) string {
	shown := displayTitle(title, t.HidePrefixes)
	code, prefix := titleSpans(shown)
	return t.styleTitle(titleLine{text: shown}, code, prefix)
}
