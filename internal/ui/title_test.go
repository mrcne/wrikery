package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mrcne/wrikery/internal/config"
)

func TestDisplayTitleDropsAConfiguredCode(t *testing.T) {
	hide := []string{"(MX)", "(TX)"}
	cases := map[string]string{
		"(MX) Frontend - Add better styles": "Frontend - Add better styles",
		"(TX) Something":                    "Something",
		"(MX)Frontend":                      "(MX)Frontend",
		"(MX)":                              "",
		"Plain title":                       "Plain title",
		"(MX)  Backend:  x":                 "Backend: x",
	}
	for in, want := range cases {
		if got := displayTitle(in, hide); got != want {
			t.Errorf("displayTitle(%q) = %q, want %q", in, got, want)
		}
	}
	if got := displayTitle("(MX) Kept", nil); got != "(MX) Kept" {
		t.Errorf("nothing configured keeps the code, got %q", got)
	}
	// Repeated spaces fold, the styled spans are offsets into the folded text.
	if got := displayTitle("(MX)  Backend:  x", nil); got != "(MX) Backend: x" {
		t.Errorf("repeated spaces should fold, got %q", got)
	}
}

func TestTitleSpansFindTheCodeAndThePartPrefixes(t *testing.T) {
	cases := []struct {
		title              string
		codeEnd, prefixEnd int
	}{
		{"(MX) Frontend - Add better styles", 5, 16},
		{"(MX) Backend: Kafka - Processing", 5, 22},
		{"(MX) Movies list: Quick prefix - More details", 5, 33},
		{"(MX) Bug: Don't show past movies when there are still any movies in the future", 5, 10},
		{"Plain title without anything", 0, 0},
		{"(MX) Note:", 5, 5},
		{"This is a very long segment here - tail", 0, 0},
		{"Frontend - Add better styles", 0, 11},
	}
	for _, c := range cases {
		code, prefix := titleSpans(c.title)
		if code != c.codeEnd || prefix != c.prefixEnd {
			t.Errorf("titleSpans(%q) = %d, %d, want %d, %d", c.title, code, prefix, c.codeEnd, c.prefixEnd)
		}
	}
}

func TestWrapTitleBreaksAtAPartPrefixInTheSecondHalf(t *testing.T) {
	cases := []struct {
		title string
		width int
		want  []string
	}{
		{"(MX) Frontend - Add better styles", 34, []string{"(MX) Frontend - Add better styles"}},
		{"(MX) Movies list: Quick prefix - More details", 34, []string{"(MX) Movies list: Quick prefix -", "More details"}},
		{"(MX) Bug: Don't show past movies when there are still any movies in the future", 34, []string{"(MX) Bug: Don't show past movies", "when there are still any movies.."}},
		{"(MX) Frontend - Add better styles", 20, []string{"(MX) Frontend -", "Add better styles"}},
		{"(MX) Backend: Kafka - Processing", 20, []string{"(MX) Backend:", "Kafka - Processing"}},
		{"(MX) Movies list: Quick prefix - More details", 20, []string{"(MX) Movies list:", "Quick prefix - Mor.."}},
		{"(MX) Bug: Don't show past movies when there are still any movies in the future", 20, []string{"(MX) Bug: Don't show", "past movies when.."}},
		{"Supercalifragilisticexpialidocious word", 20, []string{"Supercalifragilistic", "expialidocious word"}},
		{"", 20, []string{""}},
	}
	for _, c := range cases {
		lines := wrapTitle(c.title, c.width)
		var got []string
		for _, l := range lines {
			got = append(got, l.text)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("wrapTitle(%q, %d) = %q, want %q", c.title, c.width, got, c.want)
		}
	}
	lines := wrapTitle("(MX) Movies list: Quick prefix - More details", 34)
	if len(lines) != 2 || lines[0].start != 0 || lines[1].start != 33 {
		t.Errorf("line starts = %+v, the second line begins where the title's part prefixes end", lines)
	}
}

func TestStyledTitleKeepsTheTextAndDropsHiddenCodes(t *testing.T) {
	th := NewTheme(config.UIConfig{Theme: "dark", ASCII: true, HidePrefixes: []string{"(TX)"}})
	if got := ansi.Strip(th.styledTitle("(MX) Backend: Kafka - Processing")); got != "(MX) Backend: Kafka - Processing" {
		t.Errorf("styled text = %q", got)
	}
	if got := ansi.Strip(th.styledTitle("(TX) Backend: Kafka - Processing")); got != "Backend: Kafka - Processing" {
		t.Errorf("hidden code still there: %q", got)
	}
	// Colors cannot be asserted here, lipgloss draws none without a terminal, so the split itself is checked instead.
	line := titleLine{text: "Kafka - Processing", start: 14}
	if got := ansi.Strip(th.styleTitle(line, 5, 22)); got != "Kafka - Processing" {
		t.Errorf("a second line keeps its text through the spans: %q", got)
	}
	if !strings.HasPrefix(th.styledTitle("Plain"), "Plain") {
		t.Error("a title without code or prefix is drawn as it is")
	}
}
