package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// Config is the fully resolved runtime configuration for a single releasegen
// invocation. All fields are populated from environment variables and/or
// CLI flags before the runner starts; nothing else in the codebase reads
// the process environment.
type Config struct {
	// Required GitHub Actions context.
	Token     string
	OwnerRepo string // "<owner>/<repo>"
	Actor     string
	Branch    string

	// Optional manual override.
	ManualVersion string
	Reason        string

	// Discovery / classification settings.
	ExcludeDirs []string
	CustomTypes map[string]BumpType // canonical lowercase heading -> bump

	// Operational flags.
	DryRun   bool // do not commit, push, tag, or publish
	Debug    bool // verbose tag/discovery diagnostics
	RepoRoot string

	// SummaryFile, if non-empty, receives a JSON summary of the run.
	SummaryFile string

	// SelfReleaseModule and SelfReleaseRepo together identify the
	// "releasegen releasing itself" case: when the module with this name is
	// released inside SelfReleaseRepo, the resulting version is printed to
	// stdout for downstream workflow steps to consume. SelfReleaseModule is
	// the module's directory path relative to the repo root; an empty value
	// means the root module (releasegen lives at the repository root). The
	// feature is active whenever SelfReleaseRepo is non-empty and matches
	// the repository being released; set SelfReleaseRepo to "" to disable.
	SelfReleaseModule string
	SelfReleaseRepo   string

	// RequireChangelogEntry, when true, makes `releasegen validate` enforce
	// that any module whose non-CHANGELOG files changed (vs BaseRef) also
	// gained new content under its `## [Unreleased]` section. This folds
	// the historical "ensure_changelog" pre-merge guard into releasegen so
	// developers can't merge code without a changelog entry. Default false
	// so the syntactic validation can be adopted independently.
	RequireChangelogEntry bool

	// BaseRef is the git revision the changelog-entry check diffs against.
	// Any revspec go-git can resolve is accepted (branch, tag, remote
	// tracking ref like "origin/main", raw hash). Empty falls back to
	// "origin/$GITHUB_BASE_REF" when running under GitHub Actions PR
	// events, else "origin/main".
	BaseRef string
}

// Owner returns the "owner" portion of OwnerRepo.
func (c *Config) Owner() string {
	owner, _, _ := strings.Cut(c.OwnerRepo, "/")
	return owner
}

// Repo returns the "repo" portion of OwnerRepo.
func (c *Config) Repo() string {
	_, repo, _ := strings.Cut(c.OwnerRepo, "/")
	return repo
}

// Validate checks fields needed regardless of which command is running:
// repo root must be set and custom change types must be well-formed.
// Commands that need additional context (release vs. validate) should call
// the path-specific helpers below in addition to this method.
func (c *Config) Validate() error {
	var errs []error
	if c.RepoRoot == "" {
		errs = append(errs, errors.New("repo root is required"))
	}
	for heading, bump := range c.CustomTypes {
		if heading == "" {
			errs = append(errs, errors.New("custom change type has empty heading"))
		}
		if bump == BumpNone {
			errs = append(errs, fmt.Errorf("custom change type %q has invalid bump", heading))
		}
	}
	return errors.Join(errs...)
}

// ValidateForRelease enforces the additional fields required to perform an
// actual release (commit/tag/push and GitHub Release publication). Callers
// should invoke Validate() first so common errors aren't masked.
func (c *Config) ValidateForRelease() error {
	var errs []error
	if err := c.Validate(); err != nil {
		errs = append(errs, err)
	}
	if c.Token == "" {
		errs = append(errs, errors.New("GITHUB_TOKEN is required"))
	}
	if c.OwnerRepo == "" {
		errs = append(errs, errors.New("GITHUB_REPOSITORY is required"))
	} else if owner, repo, ok := strings.Cut(c.OwnerRepo, "/"); !ok || owner == "" || repo == "" {
		errs = append(errs, fmt.Errorf("GITHUB_REPOSITORY %q must be in <owner>/<repo> form", c.OwnerRepo))
	}
	if c.Actor == "" {
		errs = append(errs, errors.New("GITHUB_ACTOR is required"))
	}
	if c.Branch == "" {
		errs = append(errs, errors.New("GITHUB_REF_NAME is required"))
	}
	if c.ManualVersion != "" {
		if _, err := semver.NewVersion(strings.TrimPrefix(c.ManualVersion, "v")); err != nil {
			errs = append(errs, fmt.Errorf("MANUAL_VERSION %q is not a valid semver: %w", c.ManualVersion, err))
		}
	}
	return errors.Join(errs...)
}

// FromEnv builds a Config from process environment variables. It does not
// validate; callers should invoke Validate after applying any flag overrides.
func FromEnv() (*Config, error) {
	customTypes, err := ParseCustomTypes(os.Getenv("CUSTOM_CHANGE_TYPES"))
	if err != nil {
		return nil, err
	}
	return &Config{
		Token:         os.Getenv("GITHUB_TOKEN"),
		OwnerRepo:     os.Getenv("GITHUB_REPOSITORY"),
		Actor:         os.Getenv("GITHUB_ACTOR"),
		Branch:        os.Getenv("GITHUB_REF_NAME"),
		ManualVersion: os.Getenv("MANUAL_VERSION"),
		Reason:        os.Getenv("REASON"),
		ExcludeDirs:   ParseExcludeDirs(os.Getenv("EXCLUDE_DIRS")),
		CustomTypes:   customTypes,
		Debug:         strings.EqualFold(os.Getenv("DEBUG"), "true"),
		RepoRoot:      envOr("REPO_ROOT", "."),
		SummaryFile:   os.Getenv("SUMMARY_FILE"),
		// Empty default: releasegen lives at the root of c2fo/releasegen, so
		// the root module (module name "") is the self-release module.
		SelfReleaseModule: os.Getenv("RELEASEGEN_SELF_MODULE"),
		SelfReleaseRepo:   envOr("RELEASEGEN_SELF_REPO", "c2fo/releasegen"),
		// RELEASEGEN_REQUIRE_CHANGELOG_ENTRY accepts the usual truthy
		// strings; anything else (including unset) leaves the field at
		// its zero value so the config file can supply the answer.
		RequireChangelogEntry: strings.EqualFold(os.Getenv("RELEASEGEN_REQUIRE_CHANGELOG_ENTRY"), "true"),
		BaseRef:               os.Getenv("RELEASEGEN_BASE_REF"),
	}, nil
}

// DefaultBaseRef returns the effective base ref for the changelog-entry
// check after taking ambient CI environment into account. Callers should
// only invoke this when cfg.BaseRef is still empty after flag/env/file
// resolution; the result is "origin/<GITHUB_BASE_REF>" if running under a
// GitHub Actions pull_request event, otherwise "origin/main".
func DefaultBaseRef() string {
	if ghBase := os.Getenv("GITHUB_BASE_REF"); ghBase != "" {
		return "origin/" + ghBase
	}
	return "origin/main"
}

// envOr returns the value of the named env var, or fallback when unset.
func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

// ParseExcludeDirs splits a newline- or comma-separated list of directories,
// trims whitespace, and normalizes each entry to end with "/".
func ParseExcludeDirs(raw string) []string {
	if raw == "" {
		return nil
	}
	sep := ","
	if strings.Contains(raw, "\n") {
		sep = "\n"
	}
	parts := strings.Split(raw, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.HasSuffix(p, "/") {
			p += "/"
		}
		out = append(out, p)
	}
	return out
}

// ParseCustomTypes parses a newline-separated list of "<heading>:<bump>"
// pairs into a canonical lower-case heading -> BumpType map.
func ParseCustomTypes(raw string) (map[string]BumpType, error) {
	out := map[string]BumpType{}
	if raw == "" {
		return out, nil
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		heading, bumpStr, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("invalid CUSTOM_CHANGE_TYPES entry %q (want <heading>:<bump>)", line)
		}
		bump, err := ParseBumpType(bumpStr)
		if err != nil {
			return nil, fmt.Errorf("custom change type %q: %w", heading, err)
		}
		out[strings.ToLower(strings.TrimSpace(heading))] = bump
	}
	return out, nil
}
