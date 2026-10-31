package store

import "testing"

func TestTitleFolderFoldsCaseAndCombiningMarks(t *testing.T) {
	f := newTitleFolder()
	for _, tc := range []struct{ title, want string }{
		{"Plain ASCII Title", "plain ascii title"},
		{"R\u00e9sum\u00e9 Review", "resume review"},
		{"Re\u0301sume\u0301 decomposed", "resume decomposed"},
		{"Za\u017c\u00f3\u0142\u0107 g\u0119\u015bl\u0105 ja\u017a\u0144", "zazo\u0142c gesla jazn"},
		{"", ""},
		{"\u0301", ""},
	} {
		// Twice in a row, the chain keeps state between calls and must start clean each time.
		for i := 0; i < 2; i++ {
			if got := f.fold(tc.title); got != tc.want {
				t.Errorf("fold(%q) call %d = %q, want %q", tc.title, i+1, got, tc.want)
			}
		}
		if got := FoldTitle(tc.title); got != tc.want {
			t.Errorf("FoldTitle(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}

func TestTitleFolderFoldsAnASCIITitleWithoutTheChain(t *testing.T) {
	f := newTitleFolder()
	if n := testing.AllocsPerRun(100, func() { f.fold("Fix auth retry loop 1234") }); n > 1 {
		t.Errorf("folding an ASCII title allocates %v times, want at most the lowercased copy", n)
	}
}
