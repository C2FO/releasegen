package changelog_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/c2fo/releasegen/internal/changelog"
)

type RewriterTestSuite struct {
	suite.Suite
}

func TestRewriterTestSuite(t *testing.T) {
	suite.Run(t, new(RewriterTestSuite))
}

func (s *RewriterTestSuite) TestRewrite() {
	now := time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name              string
		content           string
		unreleasedSection string
		moduleName        string
		nextVersion       string
		expected          string
	}{
		{
			name: "Basic update",
			content: `
## [Unreleased]

### Added
- New feature X.

## [v1.2.3] - 2024-08-09
### Added
- Another new feature.
`,
			unreleasedSection: "### Added\n- New feature X.",
			nextVersion:       "1.2.4",
			expected: `
## [Unreleased]

## [[v1.2.4](https://github.com/owner/repo/releases/tag/v1.2.4)] - %s
### Added
- New feature X.

## [v1.2.3] - 2024-08-09
### Added
- Another new feature.
`,
		},
		{
			name: "With module name (path-escaped slash)",
			content: `
## [Unreleased]

### Added
- New feature X.

## [v1.2.3] - 2024-08-09
`,
			unreleasedSection: "### Added\n- New feature X.",
			moduleName:        "mymodule",
			nextVersion:       "1.2.4",
			expected: `
## [Unreleased]

## [[mymodule/v1.2.4](https://github.com/owner/repo/releases/tag/mymodule%%2Fv1.2.4)] - %s
### Added
- New feature X.

## [v1.2.3] - 2024-08-09
`,
		},
	}
	nowStr := now.UTC().Format("2006-01-02")
	for _, tt := range tests {
		s.Run(tt.name, func() {
			got := changelog.Rewrite(tt.content, changelog.RewriteOptions{
				ModuleName:   tt.moduleName,
				NextVersion:  tt.nextVersion,
				OwnerRepo:    "owner/repo",
				MatchSection: tt.unreleasedSection,
				Now:          now,
			})
			s.Equal(fmt.Sprintf(tt.expected, nowStr), got)
		})
	}
}

func (s *RewriterTestSuite) TestReleaseName() {
	s.Equal("v1.2.3", changelog.ReleaseName("", "1.2.3"))
	s.Equal("mod/v1.2.3", changelog.ReleaseName("mod", "1.2.3"))
}

func (s *RewriterTestSuite) TestReleaseURL() {
	s.Equal(
		"https://github.com/owner/repo/releases/tag/v1.2.3",
		changelog.ReleaseURL("owner/repo", "", "1.2.3"),
	)
	s.Equal(
		"https://github.com/owner/repo/releases/tag/mod%2Fv1.2.3",
		changelog.ReleaseURL("owner/repo", "mod", "1.2.3"),
	)
}
