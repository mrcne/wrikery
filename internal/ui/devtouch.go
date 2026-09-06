package ui

import "regexp"

var permalinkID = regexp.MustCompile(`[?&]id=(\d+)`)

// taskNumber is the numeric id from the permalink, what the web app shows and what people say out loud.
func taskNumber(permalink string) string {
	m := permalinkID.FindStringSubmatch(permalink)
	if m == nil {
		return ""
	}
	return m[1]
}
