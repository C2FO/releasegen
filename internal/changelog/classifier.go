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
