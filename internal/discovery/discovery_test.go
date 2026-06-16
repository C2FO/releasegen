package discovery_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/c2fo/releasegen/internal/discovery"
	"github.com/c2fo/releasegen/internal/vcs"
	vcsmocks "github.com/c2fo/releasegen/internal/vcs/mocks"
)

type DiscoveryTestSuite struct {
	suite.Suite
}

func TestDiscoveryTestSuite(t *testing.T) {
	suite.Run(t, new(DiscoveryTestSuite))
}

func (s *DiscoveryTestSuite) TestModuleName() {
	tests := map[string]string{
		"CHANGELOG.md":       "",
		"dir/CHANGELOG.md":   "dir",
		"a/b/c/CHANGELOG.md": "a/b/c",
	}
	for in, want := range tests {
		s.Equal(want, discovery.ModuleName(in), in)
	}
}

func (s *DiscoveryTestSuite) TestRemoveExcluded() {
	in := []string{"a/CHANGELOG.md", "b/CHANGELOG.md", "c/CHANGELOG.md"}
	s.Equal(in, discovery.RemoveExcluded(in, nil))
	s.Equal([]string{"b/CHANGELOG.md", "c/CHANGELOG.md"}, discovery.RemoveExcluded(in, []string{"a/"}))
	s.Equal([]string{"b/CHANGELOG.md"}, discovery.RemoveExcluded(in, []string{"a", "c"}))
}

func (s *DiscoveryTestSuite) TestFind_HappyPath() {
	repo := vcsmocks.NewRepo(s.T())
	repo.EXPECT().AllChangelogPaths(mock.Anything).
		Return([]string{"CHANGELOG.md", "sub/CHANGELOG.md", "vendored/CHANGELOG.md"}, nil)
	repo.EXPECT().ReachableTags(mock.Anything).Return([]vcs.TagInfo{
		{Name: "v1.0.0", ModuleName: "", Date: 100},
		{Name: "sub/v0.1.0", ModuleName: "sub", Date: 50},
	}, nil)
	repo.EXPECT().IsChangelogModifiedSinceTag(mock.Anything, "CHANGELOG.md", "v1.0.0").Return(false, nil)
	repo.EXPECT().IsChangelogModifiedSinceTag(mock.Anything, "sub/CHANGELOG.md", "sub/v0.1.0").Return(true, nil)

	d := discovery.New(repo, []string{"vendored/"})
	got, err := d.Find(context.Background())
	s.Require().NoError(err)
	s.Len(got, 1)
	s.Equal("sub/CHANGELOG.md", got[0].Path)
	s.Equal("sub", got[0].ModuleName)
	s.Equal("sub/v0.1.0", got[0].LatestTag)
}

func (s *DiscoveryTestSuite) TestFind_FirstReleaseWhenNoTag() {
	repo := vcsmocks.NewRepo(s.T())
	repo.EXPECT().AllChangelogPaths(mock.Anything).Return([]string{"new/CHANGELOG.md"}, nil)
	repo.EXPECT().ReachableTags(mock.Anything).Return(nil, nil)
	repo.EXPECT().IsChangelogModifiedSinceTag(mock.Anything, "new/CHANGELOG.md", "").Return(true, nil)

	d := discovery.New(repo, nil)
	got, err := d.Find(context.Background())
	s.Require().NoError(err)
	s.Len(got, 1)
	s.Empty(got[0].LatestTag)
}

func (s *DiscoveryTestSuite) TestFind_PropagatesErrors() {
	repo := vcsmocks.NewRepo(s.T())
	repo.EXPECT().AllChangelogPaths(mock.Anything).Return(nil, errors.New("boom"))

	d := discovery.New(repo, nil)
	_, err := d.Find(context.Background())
	s.Require().Error(err)
}
