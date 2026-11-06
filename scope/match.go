package scope

import (
	"errors"
	"fmt"
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

// CheckPattern returns an error for a pattern with bad glob syntax or one that
// can never match a path from the repo root. Match reports both as no match,
// so without this check a mistake silently turns a skill's scope off. "api/"
// is the likely one: a directory is "api/**".
func CheckPattern(pattern string) error {
	if pattern == "" {
		return errors.New("pattern is empty")
	}
	dir := strings.TrimSuffix(pattern, "/")
	for _, seg := range strings.Split(dir, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf(`pattern %q can never match a file: paths start at the repo root, with no empty, "." or ".." segment`, pattern)
		}
		if _, err := path.Match(seg, ""); err != nil {
			return fmt.Errorf("pattern %q: %w", pattern, err)
		}
	}
	if dir != pattern {
		return fmt.Errorf("pattern %q can never match a file: for a directory use %q", pattern, pattern+"**")
	}
	return nil
}

// MatchAny reports whether p matches any of patterns.
func MatchAny(patterns []string, p string) bool {
	for _, pattern := range patterns {
		if Match(pattern, p) {
			return true
		}
	}
	return false
}
