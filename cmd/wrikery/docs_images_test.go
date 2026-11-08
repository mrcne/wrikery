package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The screenshots are rendered from the demo at four times the window and halved with a 256 color palette.
// A render that skips the halving is a megabyte per file, and every re-render stays in the history, so the size is pinned.
const imageSizeLimit = 300 << 10

var docsWithImages = []string{"README.md", "docs/overview.md", "docs/architecture.md"}

var imageLink = regexp.MustCompile(`!\[[^\]]*\]\(([^)\s]+)\)`)

func TestDocsImagesExistAndStaySmall(t *testing.T) {
	root := filepath.Join("..", "..")
	linked := map[string]bool{}
	for _, doc := range docsWithImages {
		text, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(doc)))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range imageLink.FindAllStringSubmatch(string(text), -1) {
			if strings.Contains(m[1], "://") {
				continue // the badge
			}
			// An image link is relative to the file that holds it, docs/img/x.png from the README and img/x.png from the overview.
			rel := filepath.Join(filepath.Dir(filepath.FromSlash(doc)), filepath.FromSlash(m[1]))
			info, err := os.Stat(filepath.Join(root, rel))
			if err != nil {
				t.Errorf("%s links %s: %v", doc, m[1], err)
				continue
			}
			if info.Size() > imageSizeLimit {
				t.Errorf("%s is %d bytes, over the %d byte limit", rel, info.Size(), imageSizeLimit)
			}
			linked[filepath.ToSlash(rel)] = true
		}
	}
	images, err := filepath.Glob(filepath.Join(root, "docs", "img", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, img := range images {
		rel, _ := filepath.Rel(root, img)
		if !linked[filepath.ToSlash(rel)] {
			t.Errorf("%s is linked from no document", rel)
		}
	}
}
