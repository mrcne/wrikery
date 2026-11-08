package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The screenshots are rendered from the demo at four times the window and halved with a 256 color palette.
// A render that skips the halving is a megabyte per file, and every re-render stays in the history, so the size is pinned.
const imageSizeLimit = 300 << 10

// Every Markdown file in the repository may link an image, the working notes and the private checkout under work are not read.
var skippedDocDirs = []string{".git", "work", "docs/ai", "docs/plans", "dist"}

var (
	markdownImage = regexp.MustCompile(`!\[[^\]]*\]\(\s*([^)\s]+)(?:\s+"[^"]*")?\s*\)`)
	htmlImage     = regexp.MustCompile(`<img\b[^>]*\bsrc="([^"]+)"`)
)

// imageLinks returns the local image paths a Markdown document links, ![alt](path "title") and <img src="path">, a URL is not one.
func imageLinks(text string) []string {
	var out []string
	for _, re := range []*regexp.Regexp{markdownImage, htmlImage} {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if !strings.Contains(m[1], "://") {
				out = append(out, m[1])
			}
		}
	}
	return out
}

func TestImageLinksReadBothForms(t *testing.T) {
	text := "![a](docs/img/a.png)\n" +
		"![b](img/b.png \"the board\")\n" +
		"<img src=\"docs/img/c.png\" width=\"800\">\n" +
		"[![badge](https://x.test/b.svg)](https://x.test)\n" +
		"<img width=\"1\" src=\"d.png\">"
	got := imageLinks(text)
	want := []string{"docs/img/a.png", "img/b.png", "docs/img/c.png", "d.png"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("imageLinks = %q, want %q", got, want)
	}
}

func TestDocsImagesExistAndStaySmall(t *testing.T) {
	root := filepath.Join("..", "..")
	linked := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			for _, skip := range skippedDocDirs {
				if rel == filepath.FromSlash(skip) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, link := range imageLinks(string(text)) {
			// An image link is relative to the file that holds it, docs/img/x.png from the README and img/x.png from the overview.
			img := filepath.Join(filepath.Dir(rel), filepath.FromSlash(link))
			info, err := os.Stat(filepath.Join(root, img))
			if err != nil {
				t.Errorf("%s links %s: %v", rel, link, err)
				continue
			}
			if info.Size() > imageSizeLimit {
				t.Errorf("%s is %d bytes, over the %d byte limit", img, info.Size(), imageSizeLimit)
			}
			linked[filepath.ToSlash(img)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	images, err := filepath.Glob(filepath.Join(root, "docs", "img", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, img := range images {
		if strings.HasPrefix(filepath.Base(img), ".") {
			continue // .DS_Store and the like, Go's * matches a leading dot
		}
		rel, _ := filepath.Rel(root, img)
		if !linked[filepath.ToSlash(rel)] {
			t.Errorf("%s is linked from no document", rel)
		}
	}
}
