package forge

import (
	"context"
	"fmt"

	"github.com/google/go-github/v68/github"
)

// GitHubReleaser publishes releases against the public GitHub API.
type GitHubReleaser struct {
	client *github.Client
}

// NewGitHubReleaser returns a Releaser configured with the supplied bearer
// token (typically a GitHub App installation token).
func NewGitHubReleaser(token string) *GitHubReleaser {
	return &GitHubReleaser{client: github.NewClient(nil).WithAuthToken(token)}
}

// NewGitHubReleaserFromClient is a test-friendly constructor that accepts a
// pre-configured *github.Client. Production code should prefer
// NewGitHubReleaser; tests can swap in a client pointed at httptest.
func NewGitHubReleaserFromClient(client *github.Client) *GitHubReleaser {
	return &GitHubReleaser{client: client}
}

// CreateRelease creates a GitHub Release.
func (g *GitHubReleaser) CreateRelease(ctx context.Context, opts CreateReleaseOptions) error {
	release := &github.RepositoryRelease{
		TagName: github.Ptr(opts.TagName),
		Name:    github.Ptr(opts.Name),
		Body:    github.Ptr(opts.Body),
	}
	if _, _, err := g.client.Repositories.CreateRelease(ctx, opts.Owner, opts.Repo, release); err != nil {
		return fmt.Errorf("%w: create GitHub release %s: %w", ErrForge, opts.TagName, err)
	}
	return nil
}
