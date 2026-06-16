// Package vcs abstracts the version-control operations releasegen needs.
//
// The Repo interface is defined here in the consumer package per Go best
// practice. The default implementation is GitRepo (in git.go), which uses
// go-git. Tests use mockery-generated mocks under internal/vcs/mocks.
package vcs

import (
	"context"
	"errors"

	"github.com/go-git/go-git/v5/plumbing"
)

// ErrVCS is the sentinel returned (wrapped) when any git-side operation
// (open, walk, commit, tag, push) fails. Callers should use errors.Is to
// branch on it for exit code mapping rather than scanning error strings.
var ErrVCS = errors.New("vcs error")

//go:generate mockery

// TagInfo describes a single tag reachable from the release branch.
type TagInfo struct {
	Name       string        // e.g. "v1.2.3" or "module/v1.2.3"
	ModuleName string        // empty for root tags
	Date       int64         // unix seconds (commit or tagger date)
	Hash       plumbing.Hash // the commit the tag points at
}

// CommitTagPushOptions describes a single per-module commit + tag + push.
type CommitTagPushOptions struct {
	ChangelogPath string
	ModuleName    string
	Version       string // bare semver
	Actor         string
	Token         string // pushed via basic auth
}

// Repo is the abstraction the runner uses to interact with git. It is
// intentionally tiny so it can be mocked easily.
type Repo interface {
	// AllChangelogPaths returns every CHANGELOG.md file in HEAD's tree.
	AllChangelogPaths(ctx context.Context) ([]string, error)

	// ReachableTags returns all tags whose commits are reachable from the
	// configured release branch.
	ReachableTags(ctx context.Context) ([]TagInfo, error)

	// IsChangelogModifiedSinceTag reports whether the file at changelogPath
	// has been modified between the commit pointed at by tagName and HEAD.
	// When tagName is empty (no prior tag) the function returns true.
	IsChangelogModifiedSinceTag(ctx context.Context, changelogPath, tagName string) (bool, error)

	// CommitTagAndPush stages the changelog file, commits it with a
	// "[skip ci]" message, pushes the commit, creates an annotated tag for
	// the release name, and pushes the tag. It is intentionally a single
	// operation so the implementation can attempt cleanup on failure.
	CommitTagAndPush(ctx context.Context, opts CommitTagPushOptions) error
}
