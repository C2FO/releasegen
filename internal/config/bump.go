// Package config defines typed configuration for releasegen and the helpers
// used to load it from environment variables and CLI flags.
package config

import (
	"fmt"
	"strings"
)

// BumpType is the SemVer increment classification for an unreleased section.
//
// Values are ordered from least- to most-significant so callers can use
// numeric comparison ("if bump > current { current = bump }") to pick the
// highest-priority bump.
type BumpType uint8

const (
	// BumpNone means no recognized change was detected.
	BumpNone BumpType = iota
	// BumpPatch corresponds to a SemVer patch increment.
	BumpPatch
	// BumpMinor corresponds to a SemVer minor increment.
	BumpMinor
	// BumpMajor corresponds to a SemVer major increment.
	BumpMajor
)

// String returns the lower-case textual name of the bump type.
func (b BumpType) String() string {
	switch b {
	case BumpMajor:
		return "major"
	case BumpMinor:
		return "minor"
	case BumpPatch:
		return "patch"
	default:
		return "none"
	}
}

// ParseBumpType parses a textual bump name (case-insensitive) into a BumpType.
func ParseBumpType(s string) (BumpType, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "major":
		return BumpMajor, nil
	case "minor":
		return BumpMinor, nil
	case "patch":
		return BumpPatch, nil
	default:
		return BumpNone, fmt.Errorf("unrecognized bump type %q (want major, minor, or patch)", s)
	}
}
