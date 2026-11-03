package render

import (
	"net/url"
	"strconv"
	"strings"
)

// GitHubFileURL returns a Options.FileURL for a GitHub repository, such as
// https://github.com/owner/repo, at a commit; a line links as ?plain=1#L17 (so it works for Markdown and other rendered files) and a range as ?plain=1#L17-L19. It
// returns nil when the URL or the commit is empty.
func GitHubFileURL(repoURL, commit string) func(path string, line, endLine int) string {
	return fileURL(repoURL, commit, "/blob/", "-L")
}

// GitLabFileURL returns a Options.FileURL for a GitLab project, such as
// https://gitlab.example.com/group/project, at a commit; a range links as
// ?plain=1#L17-19. It returns nil when the URL or the commit is empty.
func GitLabFileURL(projectURL, commit string) func(path string, line, endLine int) string {
	return fileURL(projectURL, commit, "/-/blob/", "-")
}

// fileURL builds the link function; blob is the path between the project URL
// and the commit, rangeSep what goes between the first and last line.
func fileURL(baseURL, commit, blob, rangeSep string) func(path string, line, endLine int) string {
	baseURL = strings.TrimSuffix(baseURL, "/")
	if baseURL == "" || commit == "" {
		return nil
	}
	prefix := baseURL + blob + url.PathEscape(commit) + "/"
	return func(path string, line, endLine int) string {
		segments := strings.Split(path, "/")
		for i, s := range segments {
			segments[i] = url.PathEscape(s)
		}
		link := prefix + strings.Join(segments, "/")
		if line <= 0 {
			return link
		}
		// ?plain=1 shows a rendered file (Markdown, for one) as source, where
		// the line anchor works.
		link += "?plain=1#L" + strconv.Itoa(line)
		if endLine > line {
			link += rangeSep + strconv.Itoa(endLine)
		}
		return link
	}
}

// location is where a finding points, as shown in the comment: the file, the
// file and line, or the file and the range of lines.
func location(f Finding) string {
	if f.Line <= 0 {
		return f.File
	}
	loc := f.File + ":" + strconv.Itoa(f.Line)
	if f.EndLine > f.Line {
		loc += "–" + strconv.Itoa(f.EndLine)
	}
	return loc
}
