package diff

import (
	"path"
	"strings"
)

// Match reports whether the slash-separated path matches pattern.
// "**" matches zero or more path segments and "*" matches within one
// segment. A pattern containing no "/" matches the basename at any depth.
func Match(pattern, p string) bool {
	if pattern == "**" {
		return true
	}
	if !strings.Contains(pattern, "/") {
		ok, err := path.Match(pattern, path.Base(p))
		return err == nil && ok
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(p, "/"))
}

func matchSegments(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			rest := pat[1:]
			if len(rest) == 0 {
				return true
			}
			for i := 0; i <= len(segs); i++ {
				if matchSegments(rest, segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		ok, err := path.Match(pat[0], segs[0])
		if err != nil || !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}
