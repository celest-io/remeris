package releasenotes

import (
	"strings"
	"time"

	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

// HeaderLevel is the markdown header level of the release titles of a changelog.
type HeaderLevel int

// The header levels a changelog can use for its release titles.
const (
	// H2 titles look like "## 1.2.3 (2023-01-01)". It is the default.
	H2 HeaderLevel = iota
	// H1 titles look like "# 1.2.3 (2023-01-01)".
	H1
)

// String returns the markdown marker: "#" for H1 and "##" for H2.
func (l HeaderLevel) String() string {
	if l == H1 {
		return "#"
	}
	return "##"
}

// Changelog is the content of a changelog file and the header level of its release titles (F12.4). It is a value:
// the pure core never reads or writes the file, the caller supplies the content and executes the returned action.
type Changelog struct {
	// Path of the changelog file.
	Path string
	// Content is the content last read or written; empty when the file does not exist yet (F2.2).
	Content string
	// ReleaseHeaderLevel is the header level of the title of each release.
	ReleaseHeaderLevel HeaderLevel
}

// NewChangelog detects the header level of the releases from content: the second line starting with "#" decides
// (the first one is assumed to be the changelog's title). "##" means H2, a single "#" means H1, and H2 is the
// default when there is no second header.
func NewChangelog(path, content string) Changelog {
	level := H2
	n := 0
	for _, line := range splitLines(content) {
		if !strings.HasPrefix(line, "#") {
			continue
		}
		n++
		if n == 2 {
			if !strings.HasPrefix(line, "##") {
				level = H1
			}
			break
		}
	}
	return Changelog{Path: path, Content: content, ReleaseHeaderLevel: level}
}

// splitLines splits like Rust's str::lines, which Knope uses: lines end at "\n" and a "\r" before it is dropped;
// a final line without terminator is kept as is; a trailing newline adds no empty line; "" has no lines.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	last := len(parts) - 1
	if parts[last] == "" {
		parts = parts[:last]
	}
	for i := range parts {
		if i < last {
			parts[i] = strings.TrimSuffix(parts[i], "\r")
		}
	}
	return parts
}

// WithRelease returns the changelog with release r added, and the action that writes it (F12.4).
//
// The block is "{level} {title}\n\n{notes}", where the notes (written at header level 1) get one more "#" on
// each line starting with "#" in an H2 changelog. It goes right before the first line that is a release title of
// any level, or at the end of the content when there is none. Every original line is copied with "\n", so CRLF
// becomes LF, and the content keeps its final newline (an empty content gets one). The action's diff, shown in
// dry runs, is "\n" + block + "\n".
func (c Changelog) WithRelease(r action.Release) (Changelog, action.Action) {
	var body []string
	for _, line := range splitLines(r.Notes) {
		if strings.HasPrefix(line, "#") && c.ReleaseHeaderLevel == H2 {
			line = "#" + line
		}
		body = append(body, line)
	}
	block := c.ReleaseHeaderLevel.String() + " " + r.Title + "\n\n" + strings.Join(body, "\n")

	var sb strings.Builder
	written := false
	for _, line := range splitLines(c.Content) {
		if !written {
			if _, _, _, ok := parseTitle(line); ok {
				sb.WriteString(block + "\n\n")
				written = true
			}
		}
		sb.WriteString(line + "\n")
	}
	if !written {
		sb.WriteString(block)
	}
	out := sb.String()
	if (c.Content == "" || strings.HasSuffix(c.Content, "\n")) && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	c.Content = out
	return c, action.WriteToFile(c.Path, out, "\n"+block+"\n")
}

// GetRelease finds the release of version v in the changelog (F12.5). The title is the title line without its
// "#" prefix, the notes are the lines after it up to the next line starting with the release header marker and a
// space, at header level 1 (one "#" is removed from lines starting with "##" in an H2 changelog). It reports false
// when the version is not found or its notes are empty. PackageName is left nil: the caller sets it.
func (c Changelog) GetRelease(v semver.Version) (action.Release, bool) {
	marker := c.ReleaseHeaderLevel.String()
	start := marker + " " + v.String()
	lines := splitLines(c.Content)
	for i, line := range lines {
		if !strings.HasPrefix(line, start) {
			continue
		}
		level, tv, _, ok := parseTitle(line)
		if !ok || level != c.ReleaseHeaderLevel || !tv.Equal(v) {
			continue
		}
		var notes []string
		for _, next := range lines[i+1:] {
			if strings.HasPrefix(next, marker+" ") {
				break
			}
			if c.ReleaseHeaderLevel == H2 && strings.HasPrefix(next, "##") {
				next = next[1:]
			}
			notes = append(notes, next)
		}
		joined := strings.Join(notes, "\n")
		if joined == "" {
			return action.Release{}, false
		}
		return action.Release{Title: strings.TrimSpace(strings.TrimLeft(line, "#")), Version: tv, Notes: joined}, true
	}
	return action.Release{}, false
}

// parseTitle reads a release title such as "## 1.2.3 (2023-01-01)": a "#" or "##" token, then a version, then
// optional tokens of which the first valid "(YYYY-MM-DD)" (parentheses optional) is the date. Tokens are split on
// ASCII whitespace. date is the zero time when there is none.
func parseTitle(line string) (level HeaderLevel, v semver.Version, date time.Time, ok bool) {
	parts := strings.FieldsFunc(line, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\f' || r == '\r'
	})
	if len(parts) < 2 {
		return 0, semver.Version{}, time.Time{}, false
	}
	switch parts[0] {
	case "##":
		level = H2
	case "#":
		level = H1
	default:
		return 0, semver.Version{}, time.Time{}, false
	}
	v, err := semver.Parse(parts[1])
	if err != nil {
		return 0, semver.Version{}, time.Time{}, false
	}
	for _, p := range parts[2:] {
		p = strings.TrimRight(strings.TrimLeft(p, "("), ")")
		if d, err := time.Parse(time.DateOnly, p); err == nil {
			date = d
			break
		}
	}
	return level, v, date, true
}
