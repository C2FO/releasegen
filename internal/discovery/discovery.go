// Package discovery walks the repo for CHANGELOG.md files, applies the
// configured exclude list, derives module names from paths, and pairs each
// candidate with its module-specific most-recent tag.
package discovery

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/c2fo/releasegen/internal/vcs"
)

// Candidate is a single changelog that has actually been modified since
// its module's most recent tag (or has no prior tag).
type Candidate struct {
	Path       string // repo-relative path to CHANGELOG.md
	ModuleName string // directory of the changelog ("" for repo root)
	LatestTag  string // "" when there is no prior tag for this module
}

// Discoverer pairs a vcs.Repo with discovery configuration.
type Discoverer struct {
	repo        vcs.Repo
	excludeDirs []string
}

// New returns a Discoverer that reads from repo and applies excludeDirs
// as path prefixes (each entry should already end with "/").
func New(repo vcs.Repo, excludeDirs []string) *Discoverer {
	return &Discoverer{repo: repo, excludeDirs: excludeDirs}
}

// Find returns the list of candidate changelogs to release.
func (d *Discoverer) Find(ctx context.Context) ([]Candidate, error) {
	paths, err := d.repo.AllChangelogPaths(ctx)
	if err != nil {
		return nil, fmt.Errorf("list changelog files: %w", err)
	}
	paths = RemoveExcluded(paths, d.excludeDirs)
	paths = slices.Compact(paths)

	tags, err := d.repo.ReachableTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("list reachable tags: %w", err)
	}

	var candidates []Candidate
	for _, p := range paths {
		module := ModuleName(p)
		latest := vcs.LatestTagForModule(tags, module)
		modified, err := d.repo.IsChangelogModifiedSinceTag(ctx, p, latest)
		if err != nil {
			return nil, fmt.Errorf("check %s: %w", p, err)
		}
		if !modified {
			continue
		}
		candidates = append(candidates, Candidate{
			Path:       p,
			ModuleName: module,
			LatestTag:  latest,
		})
	}
	return candidates, nil
}

// ModuleName returns the module name (directory) for a given changelog path.
// Root-level files return "".
func ModuleName(changelogPath string) string {
	dir := filepath.Dir(changelogPath)
	if dir == "." {
		return ""
	}
	return filepath.ToSlash(dir)
}

// RemoveExcluded filters out paths whose directory matches any prefix in
// excludeDirs. Each excludeDir must be a directory path ending in "/".
func RemoveExcluded(paths, excludeDirs []string) []string {
	if len(excludeDirs) == 0 {
		return paths
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		excluded := false
		for _, ex := range excludeDirs {
			if !strings.HasSuffix(ex, "/") {
				ex += "/"
			}
			if strings.HasPrefix(p, ex) {
				excluded = true
				break
			}
		}
		if !excluded {
			out = append(out, p)
		}
	}
	return out
}
