// Package changelog parses, classifies, and rewrites Keep a Changelog files.
//
// All functions in this package are pure: they accept the changelog text and
// configuration as input and return new text and a classification result.
// They never read from the process environment, the file system, or git.
package changelog

import "errors"

// ErrNoChangesDetected is returned when an unreleased section is empty
// or absent and the module should be silently skipped.
var ErrNoChangesDetected = errors.New("no changes detected")

// ErrUnrecognizedChangeType is returned when an unreleased section contains
// content that does not match any built-in or configured custom heading.
var ErrUnrecognizedChangeType = errors.New("missing or unrecognized change type in unreleased section")

// ErrIncompleteBreaking is returned when a `### Changed` or `### Removed`
// heading is present but no entry under it contains the literal phrase
// "BREAKING CHANGE".
var ErrIncompleteBreaking = errors.New(
	"### Changed or ### Removed section found with missing or incomplete description." +
		" Per Keep a Changelog, these sections are for BREAKING CHANGES only and must include 'BREAKING CHANGE' in" +
		" the description. If this is NOT a breaking change, please use a different section type: ### Added (new features)" +
		", ### Fixed (bug fixes), ### Deprecated, or ### Security",
)
