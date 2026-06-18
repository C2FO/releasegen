package changelog

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/c2fo/releasegen/internal/config"
)

// breakingMarker is matched case-sensitively per the PRD: a major bump must
// be opted into deliberately.
const breakingMarker = "BREAKING CHANGE"

var (
	breakingHeadingRE = regexp.MustCompile(heading3Prefix + `(?:Change|Remove)[sd]?`)
	addedRE           = regexp.MustCompile(heading3Prefix + `Add(?:s|ed)?`)
	deprecatedRE      = regexp.MustCompile(heading3Prefix + `Deprecate[sd]?`)
	securityRE        = regexp.MustCompile(heading3Prefix + `Security`)
	changedRE         = regexp.MustCompile(heading3Prefix + `Change[sd]?`)
	removedRE         = regexp.MustCompile(heading3Prefix + `Remove[sd]?`)
	fixedRE           = regexp.MustCompile(heading3Prefix + `Fixed`)
)

// Classify inspects an unreleased section and returns the appropriate bump
// type, taking custom change types into account.
//
// The decision rules are:
//   - A `### Changed` or `### Removed` heading without the literal phrase
//     "BREAKING CHANGE" anywhere in the section returns ErrIncompleteBreaking.
//   - With "BREAKING CHANGE" present, those sections drive a major bump.
//   - `### Added`, `### Deprecated`, `### Security` drive a minor bump.
//   - `### Fixed` drives a patch bump.
//   - Custom types are evaluated under the same priority order; a custom
//     "major" mapping still requires the BREAKING CHANGE marker.
//   - The highest-priority match wins.
//   - An empty section returns ErrNoChangesDetected.
//   - Anything else returns ErrUnrecognizedChangeType.
func Classify(unreleased string, custom map[string]config.BumpType) (config.BumpType, error) {
	if strings.TrimSpace(unreleased) == "" {
		return config.BumpNone, ErrNoChangesDetected
	}

	bump := classifyCustom(unreleased, custom)

	switch {
	case breakingHeadingRE.MatchString(unreleased):
		if !strings.Contains(unreleased, breakingMarker) {
			return config.BumpNone, ErrIncompleteBreaking
		}
		bump = config.BumpMajor
	case addedRE.MatchString(unreleased),
		deprecatedRE.MatchString(unreleased),
		securityRE.MatchString(unreleased),
		changedRE.MatchString(unreleased),
		removedRE.MatchString(unreleased):
		if bump < config.BumpMinor {
			bump = config.BumpMinor
		}
	case fixedRE.MatchString(unreleased):
		if bump < config.BumpPatch {
			bump = config.BumpPatch
		}
	default:
		if bump == config.BumpNone {
			return config.BumpNone, ErrUnrecognizedChangeType
		}
	}

	if bump == config.BumpNone {
		return config.BumpNone, ErrUnrecognizedChangeType
	}
	return bump, nil
}

func classifyCustom(unreleased string, custom map[string]config.BumpType) config.BumpType {
	highest := config.BumpNone
	for heading, b := range custom {
		re := regexp.MustCompile(heading3Prefix + regexp.QuoteMeta(heading))
		if !re.MatchString(unreleased) {
			continue
		}
		if b == config.BumpMajor && !strings.Contains(unreleased, breakingMarker) {
			// Major custom types still require the breaking marker.
			continue
		}
		if b > highest {
			highest = b
		}
	}
	return highest
}

// h3HeadingRE captures every "### Heading" line in a section body. The
// heading text is whatever follows the hashes (excluding markdown decorations
// at either end), trimmed of surrounding whitespace by the caller.
//
//nolint:gochecknoglobals // package-level compiled regex; cheap and reused.
var h3HeadingRE = regexp.MustCompile(`(?im)^###\s+(.+?)\s*$`)

// builtInHeadings is the canonical set of Keep a Changelog ### headings.
// Keys are lower-case; values are the bump triggered by the heading. The
// "Changed" / "Removed" entries are special-cased by ValidateSection because
// they require the BREAKING CHANGE marker.
//
//nolint:gochecknoglobals // tiny lookup table, intentionally package-scoped.
var builtInHeadings = map[string]config.BumpType{
	"added":      config.BumpMinor,
	"changed":    config.BumpMajor,
	"removed":    config.BumpMajor,
	"deprecated": config.BumpMinor,
	"security":   config.BumpMinor,
	"fixed":      config.BumpPatch,
}

// ValidateSection inspects the body of a `## [Unreleased]` section and
// returns every problem it finds, rather than stopping at the first like
// Classify does. The returned errors are wrappers around the package's
// existing sentinels (ErrUnrecognizedChangeType, ErrIncompleteBreaking) so
// callers can still match with errors.Is for exit-code mapping.
//
// An empty / whitespace-only section returns a single ErrNoChangesDetected
// so callers can distinguish "skip this module" from real problems. Callers
// that already treat empty sections as a non-error (e.g. the validate
// subcommand) should check for emptiness before calling.
//
// Unlike Classify, this function never returns the computed bump because
// validation is not the place to make release decisions.
func ValidateSection(unreleased string, custom map[string]config.BumpType) []error {
	if strings.TrimSpace(unreleased) == "" {
		return []error{ErrNoChangesDetected}
	}

	headings := h3HeadingRE.FindAllStringSubmatch(unreleased, -1)
	if len(headings) == 0 {
		// Body had content but no ### headings at all.
		return []error{
			fmt.Errorf("%w: section contains content but no ### heading", ErrUnrecognizedChangeType),
		}
	}

	var problems []error
	hasBreakingHeading := false
	for _, m := range headings {
		h := strings.ToLower(strings.TrimSpace(m[1]))
		if _, ok := builtInHeadings[h]; ok {
			if h == "changed" || h == "removed" {
				hasBreakingHeading = true
			}
			continue
		}
		if _, ok := custom[h]; ok {
			continue
		}
		problems = append(problems, fmt.Errorf(
			"%w: ### %s (declare it under custom_change_types if intentional)",
			ErrUnrecognizedChangeType, m[1],
		))
	}

	if hasBreakingHeading && !strings.Contains(unreleased, breakingMarker) {
		problems = append(problems, ErrIncompleteBreaking)
	}

	return problems
}

// NextVersion increments currentVersion by bump, returning the resulting
// SemVer string (without a "v" prefix).
func NextVersion(currentVersion string, bump config.BumpType) (string, error) {
	v, err := semver.NewVersion(strings.TrimPrefix(currentVersion, "v"))
	if err != nil {
		return "", fmt.Errorf("invalid current version %q: %w", currentVersion, err)
	}
	switch bump {
	case config.BumpMajor:
		next := v.IncMajor()
		return next.String(), nil
	case config.BumpMinor:
		next := v.IncMinor()
		return next.String(), nil
	case config.BumpPatch:
		next := v.IncPatch()
		return next.String(), nil
	default:
		return "", fmt.Errorf("invalid bump type: %s", bump)
	}
}
