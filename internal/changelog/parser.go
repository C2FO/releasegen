package changelog

import (
	"regexp"
	"strings"
)

const (
	heading2Prefix = `(?i)##\s*`
	// heading3Prefix matches a real `### ` markdown heading at the start of
	// a line. The `(?m)` flag scopes `^` to line starts (not just the start
	// of the whole string), and `\s*` keeps tolerance for stylistic
	// variations like `###Heading` or `###   Heading`. Anchoring to a line
	// start is critical: without it, prose that mentions `### Changed`
	// inside an inline code span — exactly the kind of text this very
	// changelog contains when documenting validation rules — would be
	// mistaken for a heading and could trigger a spurious major bump.
	heading3Prefix = `(?im)^###\s*`

	// SemverPattern is a regex pattern that matches a semantic version string
	// (per https://semver.org/spec/v2.0.0.html). Exposed for callers that need
	// to construct related expressions.
	SemverPattern = `(?P<major>0|[1-9]\d*)\.` +
		`(?P<minor>0|[1-9]\d*)\.` +
		`(?P<patch>0|[1-9]\d*)` +
		`(?:-` +
		`(?P<prerelease>` +
		`(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)` +
		`(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*` +
		`)` +
		`)?` +
		`(?:\+` +
		`(?P<buildmetadata>` +
		`[0-9a-zA-Z-]+` +
		`(?:\.[0-9a-zA-Z-]+)*` +
		`)` +
		`)?`
)

var (
	unreleasedRE               = regexp.MustCompile(`(?si)##\s*\[Unreleased\](.*?)##\s*\[{1,2}(?:.*?/)?v?` + SemverPattern + `\]`)
	unreleasedNoOtherVersionRE = regexp.MustCompile(`(?si)##\s*\[Unreleased\](.*?)\z`)
	existingVersionsRE         = regexp.MustCompile(heading2Prefix + `\[{1,2}(?:.*?/)?v?` + SemverPattern + `\]`)
	currentVersionRE           = regexp.MustCompile(heading2Prefix + `\[{1,2}(?:.*?/)?v?(` + SemverPattern + `)\]`)
)

// ExtractUnreleased returns the body of the `## [Unreleased]` section,
// trimmed of surrounding whitespace. It returns an empty string when no
// unreleased section exists or the section is empty.
func ExtractUnreleased(content string) string {
	if !existingVersionsRE.MatchString(content) {
		matches := unreleasedNoOtherVersionRE.FindStringSubmatch(content)
		if len(matches) > 1 {
			return strings.TrimSpace(matches[1])
		}
		return ""
	}
	matches := unreleasedRE.FindStringSubmatch(content)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// ExtractCurrentVersion returns the most recent versioned heading in the
// changelog, or "0.0.0" when none is present (i.e. a first release).
func ExtractCurrentVersion(content string) string {
	matches := currentVersionRE.FindStringSubmatch(content)
	if len(matches) > 1 {
		return matches[1]
	}
	return "0.0.0"
}
