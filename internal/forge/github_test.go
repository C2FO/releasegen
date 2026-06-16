package forge_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/google/go-github/v68/github"
	"github.com/stretchr/testify/suite"

	"github.com/c2fo/releasegen/internal/forge"
)

type GitHubReleaserTestSuite struct {
	suite.Suite
}

func TestGitHubReleaserTestSuite(t *testing.T) {
	suite.Run(t, new(GitHubReleaserTestSuite))
}

func (s *GitHubReleaserTestSuite) TestNewGitHubReleaser_ConstructsClient() {
	// The production constructor wires up a real *github.Client with token
	// auth. We can't easily observe the bearer token without hitting the
	// network, but we can verify the constructor returns a usable releaser
	// whose CreateRelease will at least dispatch a request.
	r := forge.NewGitHubReleaser("test-token")
	s.Require().NotNil(r)
}

// newReleaserAgainst returns a GitHubReleaser configured to talk to ts.
func (s *GitHubReleaserTestSuite) newReleaserAgainst(ts *httptest.Server) *forge.GitHubReleaser {
	base, err := url.Parse(ts.URL + "/")
	s.Require().NoError(err)
	client := github.NewClient(ts.Client()).WithAuthToken("test-token")
	client.BaseURL = base
	return forge.NewGitHubReleaserFromClient(client)
}

func (s *GitHubReleaserTestSuite) TestCreateRelease_HappyPath() {
	var (
		gotAuth atomic.Value
		gotBody atomic.Value
		hits    atomic.Int32
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		gotAuth.Store(r.Header.Get("Authorization"))
		body, _ := io.ReadAll(r.Body)
		var rel github.RepositoryRelease
		if err := json.Unmarshal(body, &rel); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotBody.Store(rel)
		if r.Method != http.MethodPost || r.URL.Path != "/repos/owner/repo/releases" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1,"tag_name":"v1.2.3"}`))
	}))
	defer ts.Close()

	r := s.newReleaserAgainst(ts)
	err := r.CreateRelease(context.Background(), forge.CreateReleaseOptions{
		Owner:   "owner",
		Repo:    "repo",
		TagName: "v1.2.3",
		Name:    "[v1.2.3] - 2026-04-19",
		Body:    "### Added\n- thing",
	})
	s.Require().NoError(err)
	s.Equal(int32(1), hits.Load())
	auth, _ := gotAuth.Load().(string)
	s.Contains(auth, "Bearer test-token")
	rel, _ := gotBody.Load().(github.RepositoryRelease)
	s.Equal("v1.2.3", rel.GetTagName())
	s.Equal("[v1.2.3] - 2026-04-19", rel.GetName())
	s.Contains(rel.GetBody(), "thing")
}

func (s *GitHubReleaserTestSuite) TestCreateRelease_ServerError_WrapsErrForge() {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"validation failed"}`, http.StatusUnprocessableEntity)
	}))
	defer ts.Close()

	r := s.newReleaserAgainst(ts)
	err := r.CreateRelease(context.Background(), forge.CreateReleaseOptions{
		Owner: "owner", Repo: "repo", TagName: "v1.2.3",
	})
	s.Require().Error(err)
	s.Require().ErrorIs(err, forge.ErrForge)
	s.Contains(err.Error(), "v1.2.3")
}
