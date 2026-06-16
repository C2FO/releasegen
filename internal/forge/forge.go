// Package forge abstracts the "publish a release" operation against a code
// hosting provider. The default implementation (GitHubReleaser) targets the
// GitHub REST API; the interface keeps the runner provider-agnostic.
package forge

import (
	"context"
	"errors"
)

// ErrForge is the sentinel returned (wrapped) when a release-hosting
// operation fails. Callers should use errors.Is to branch on it.
var ErrForge = errors.New("forge error")

//go:generate mockery

// CreateReleaseOptions describes a single release publication.
type CreateReleaseOptions struct {
	Owner   string
	Repo    string
	TagName string // e.g. "v1.2.3" or "module/v1.2.3"
	Name    string // human-readable release title
	Body    string // release notes (markdown)
}

// Releaser is the abstraction for publishing a release.
type Releaser interface {
	CreateRelease(ctx context.Context, opts CreateReleaseOptions) error
}
