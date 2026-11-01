package store

import "testing"

func TestFoldTitleFoldsCaseAndCombiningMarks(t *testing.T) {
	for _, tc := range []struct{ title, want string }{
		{"Plain ASCII Title", "plain ascii title"},
		{"Résumé Review", "resume review"},
		{"Résumé decomposed", "resume decomposed"},
		{"Zażółć gęślą jaźń", "zazołc gesla jazn"},
		{"", ""},
		{"́", ""},
	} {
		// Twice in a row, a reused chain keeps state between calls and must start clean each time.
		for i := 0; i < 2; i++ {
			if got := FoldTitle(tc.title); got != tc.want {
				t.Errorf("FoldTitle(%q) call %d = %q, want %q", tc.title, i+1, got, tc.want)
			}
		}
	}
}

// A lookup folds every title of the cache and the command line folds every contact, the chain costs kilobytes to build.
func TestFoldTitleBuildsNoChainForEachCall(t *testing.T) {
	if n := testing.AllocsPerRun(100, func() { FoldTitle("Fix auth retry loop 1234") }); n > 1 {
		t.Errorf("folding an ASCII title allocates %v times, want at most the lowercased copy", n)
	}
	r := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			FoldTitle("Zażółć gęślą jaźń")
		}
	})
	if bytes := r.AllocedBytesPerOp(); bytes > 1024 {
		t.Errorf("folding a title with diacritics allocates %d bytes, want the folded copy and not a new chain", bytes)
	}
}
