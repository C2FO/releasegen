package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigFileNames lists the file names (in lookup order) that LoadFile will
// try when discovering a config file at a given root directory. Each entry is
// a bare file name; LoadFile joins it with the supplied root.
//
//nolint:gochecknoglobals // intentionally exported and not meant to be mutated at runtime.
var ConfigFileNames = []string{".releasegen.yaml", ".releasegen.yml"}

// FileConfig is the on-disk representation of a `.releasegen.yaml` /
// `.releasegen.yml` file. Only the fields here are considered "repo-shape"
// configuration; per-run values (GitHub creds, manual override, etc.) remain
// environment- and flag-only.
type FileConfig struct {
	// CustomChangeTypes maps additional changelog headings to a bump level
	// ("major", "minor", or "patch"). Headings are case-insensitive.
	CustomChangeTypes map[string]string `yaml:"custom_change_types"`

	// ExcludeDirs lists directory prefixes to skip during changelog
	// discovery. Trailing slashes are optional.
	ExcludeDirs []string `yaml:"exclude_dirs"`

	// SelfReleaseModule is the module path (relative to the repo root)
	// that should be treated as "releasegen releasing itself" so that the
	// resolved version is printed to stdout. Empty means the root module.
	SelfReleaseModule *string `yaml:"self_release_module"`

	// SelfReleaseRepo is the "owner/repo" string the self-release feature
	// matches against. Empty disables the feature.
	SelfReleaseRepo *string `yaml:"self_release_repo"`

	// Validate carries options specific to the `releasegen validate`
	// subcommand. Kept as a nested block so the command can grow more
	// knobs without polluting the top-level namespace.
	Validate *ValidateFileConfig `yaml:"validate"`
}

// ValidateFileConfig is the on-disk representation of the `validate:` block.
// All fields are pointers so we can distinguish "not specified" from
// "explicitly false / empty string": flags and env vars layer on top, and we
// must know whether a missing key should defer to those defaults.
type ValidateFileConfig struct {
	// RequireChangelogEntry, when true, makes `validate` enforce that any
	// module whose non-CHANGELOG files changed (vs BaseRef) also gained
	// new content under its `## [Unreleased]` section.
	RequireChangelogEntry *bool `yaml:"require_changelog_entry"`

	// BaseRef overrides the git revision the changelog-entry check diffs
	// against. Falls back to the env-aware default in DefaultBaseRef when
	// neither this nor the flag/env is set.
	BaseRef *string `yaml:"base_ref"`
}

// LoadFile looks for one of ConfigFileNames in repoRoot and returns a parsed
// FileConfig along with the absolute path it loaded from. If no file is
// present, (nil, "", nil) is returned. Errors are returned for unreadable or
// malformed files.
func LoadFile(repoRoot string) (*FileConfig, string, error) {
	if repoRoot == "" {
		repoRoot = "."
	}
	for _, name := range ConfigFileNames {
		path := filepath.Join(repoRoot, name)
		data, err := os.ReadFile(path) //nolint:gosec // path comes from repoRoot + known suffix
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, "", fmt.Errorf("read %s: %w", path, err)
		}
		var fc FileConfig
		dec := yaml.NewDecoder(strings.NewReader(string(data)))
		dec.KnownFields(true) // catch typos in user configs early
		if err := dec.Decode(&fc); err != nil {
			return nil, "", fmt.Errorf("parse %s: %w", path, err)
		}
		return &fc, path, nil
	}
	return nil, "", nil
}

// ApplyFile merges file-level config into cfg using the precedence rule
// "flags > env > file > defaults": file values are only applied where cfg
// is still at its zero value, i.e. neither the environment nor a flag has
// supplied an explicit value.
//
// ApplyFile must be called before any flag overrides are applied, since the
// flag layer is meant to win unconditionally.
func ApplyFile(cfg *Config, fc *FileConfig) error {
	if fc == nil || cfg == nil {
		return nil
	}

	if len(fc.CustomChangeTypes) > 0 && len(cfg.CustomTypes) == 0 {
		parsed, err := parseFileCustomTypes(fc.CustomChangeTypes)
		if err != nil {
			return err
		}
		cfg.CustomTypes = parsed
	}

	if len(fc.ExcludeDirs) > 0 && len(cfg.ExcludeDirs) == 0 {
		cfg.ExcludeDirs = normalizeExcludeDirs(fc.ExcludeDirs)
	}

	// SelfReleaseModule defaults to "" which is meaningful (root module),
	// so we use a pointer in FileConfig to distinguish "not set" from
	// "explicitly set to empty". The corresponding env var is only applied
	// when set, so a missing env var means cfg.SelfReleaseModule has its
	// zero value and we can safely apply the file value.
	if fc.SelfReleaseModule != nil && os.Getenv("RELEASEGEN_SELF_MODULE") == "" {
		cfg.SelfReleaseModule = *fc.SelfReleaseModule
	}

	// SelfReleaseRepo has a non-empty default ("c2fo/releasegen") applied
	// by FromEnv, so we only let the file override when the env var is
	// unset and we're still at that default. Forks need this to disable
	// the feature without exporting an env var.
	if fc.SelfReleaseRepo != nil {
		if _, envSet := os.LookupEnv("RELEASEGEN_SELF_REPO"); !envSet {
			cfg.SelfReleaseRepo = *fc.SelfReleaseRepo
		}
	}

	applyValidateBlock(cfg, fc.Validate)
	return nil
}

// applyValidateBlock merges the optional `validate:` config-file block into
// cfg, honoring env-var precedence. Flag overrides are applied by the
// command layer afterwards.
func applyValidateBlock(cfg *Config, vc *ValidateFileConfig) {
	if vc == nil {
		return
	}
	if vc.RequireChangelogEntry != nil {
		if _, envSet := os.LookupEnv("RELEASEGEN_REQUIRE_CHANGELOG_ENTRY"); !envSet {
			cfg.RequireChangelogEntry = *vc.RequireChangelogEntry
		}
	}
	// base_ref: env wins if set, else apply file value. The env-aware
	// "origin/<GITHUB_BASE_REF> or origin/main" default is computed by
	// DefaultBaseRef when cfg.BaseRef remains empty at use time.
	if vc.BaseRef != nil {
		if _, envSet := os.LookupEnv("RELEASEGEN_BASE_REF"); !envSet {
			cfg.BaseRef = *vc.BaseRef
		}
	}
}

func parseFileCustomTypes(in map[string]string) (map[string]BumpType, error) {
	out := make(map[string]BumpType, len(in))
	for heading, bumpStr := range in {
		bump, err := ParseBumpType(bumpStr)
		if err != nil {
			return nil, fmt.Errorf("custom change type %q: %w", heading, err)
		}
		key := strings.ToLower(strings.TrimSpace(heading))
		if key == "" {
			return nil, errors.New("custom change type has empty heading")
		}
		out[key] = bump
	}
	return out, nil
}

func normalizeExcludeDirs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
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
