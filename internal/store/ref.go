package store

import "strings"

// RefKind says what a task reference typed by the user is.
type RefKind int

const (
	// RefID is a Wrike task id.
	RefID RefKind = iota + 1
	// RefNumber is the number from the task's link in the browser, typed alone or inside the link.
	RefNumber
)

// TaskRef is a reference to one task that can be looked up on Wrike: an id, or the number from its link.
type TaskRef struct {
	Kind  RefKind
	Value string
}

// ParseTaskRef reads a task reference out of what the user typed, trimmed.
// A link holds open.htm?id= followed by digits, the host before it does not matter, and a link without digits is not a reference.
// A bare number is the number from such a link, never a part of a title.
// An id is twelve or sixteen characters from A-Z, a-z, 0-9, - and _ with a digit or an upper case letter after the first one,
// the shapes of the task, folder and status ids seen on the live account on 2026-10-22, and a plain word of that length is not one.
// Anything else is a title fragment and stays in the cache.
func ParseTaskRef(s string) (TaskRef, bool) {
	s = strings.TrimSpace(s)
	if _, rest, found := strings.Cut(s, "open.htm?id="); found {
		if n := leadingDigits(rest); n != "" {
			return TaskRef{Kind: RefNumber, Value: n}, true
		}
		return TaskRef{}, false
	}
	if s != "" && leadingDigits(s) == s {
		return TaskRef{Kind: RefNumber, Value: s}, true
	}
	if isIDShape(s) {
		return TaskRef{Kind: RefID, Value: s}, true
	}
	return TaskRef{}, false
}

func leadingDigits(s string) string {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	return s[:end]
}

func isIDShape(s string) bool {
	if len(s) != 12 && len(s) != 16 {
		return false
	}
	marked := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			marked = true
		case c >= 'A' && c <= 'Z':
			if i > 0 {
				marked = true
			}
		case c >= 'a' && c <= 'z', c == '-', c == '_':
		default:
			return false
		}
	}
	return marked
}
