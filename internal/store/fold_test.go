package store

import "testing"

func TestTitleFolderFoldsEveryTitleLikeFoldTitle(t *testing.T) {
	f := newTitleFolder()
	for _, title := range []string{
		"Zazolc gesla jazn",
		"Zażółć gęślą jaźń",
		"Résumé Review",
		"Plain ASCII title",
		"",
		"́",
	} {
		want := FoldTitle(title)
		// Twice in a row, the chain keeps state between calls and must start clean each time.
		for i := 0; i < 2; i++ {
			if got := f.fold(title); got != want {
				t.Errorf("fold(%q) call %d = %q, want %q", title, i+1, got, want)
			}
		}
	}
}
