package util

import "strings"

// AppendURLPathSegment appends a path segment before any query or fragment.
func AppendURLPathSegment(rawURL, segment string) string {
	rawURL = strings.TrimSpace(rawURL)
	segment = strings.Trim(segment, "/")
	if rawURL == "" || segment == "" {
		return ""
	}

	suffixIndex := strings.IndexAny(rawURL, "?#")
	base, suffix := rawURL, ""
	if suffixIndex >= 0 {
		base, suffix = rawURL[:suffixIndex], rawURL[suffixIndex:]
	}
	wasRooted := strings.HasPrefix(base, "/")
	base = strings.TrimRight(base, "/")
	if base == "" {
		if !wasRooted {
			return ""
		}
		return "/" + segment + suffix
	}
	return base + "/" + segment + suffix
}
