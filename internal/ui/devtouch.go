package ui

import (
	"regexp"
	"strings"

	"github.com/mrcne/wrikery/internal/store"
)

var (
	permalinkID = regexp.MustCompile(`[?&]id=(\d+)`)
	nonSlug     = regexp.MustCompile(`[^a-z0-9]+`)
)

// taskNumber is the numeric id from the permalink, what the web app shows and what people say out loud.
func taskNumber(permalink string) string {
	m := permalinkID.FindStringSubmatch(permalink)
	if m == nil {
		return ""
	}
	return m[1]
}

func slugify(title string, limit int) string {
	s := nonSlug.ReplaceAllString(strings.ToLower(title), "-")
	s = strings.Trim(s, "-")
	if len(s) > limit {
		s = strings.Trim(s[:limit], "-")
	}
	return s
}

// branchName fills the config template. {id} is the permalink number, or the API id when there is no permalink.
func branchName(tmpl string, task store.Task) string {
	id := taskNumber(task.Permalink)
	if id == "" {
		id = task.ID
	}
	return strings.NewReplacer("{id}", id, "{slug}", slugify(task.Title, 40)).Replace(tmpl)
}
