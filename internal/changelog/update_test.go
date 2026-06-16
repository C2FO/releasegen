package changelog_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/c2fo/releasegen/internal/changelog"
)

type UpdateTestSuite struct {
	suite.Suite
}

func TestUpdateTestSuite(t *testing.T) {
	suite.Run(t, new(UpdateTestSuite))
}

func (s *UpdateTestSuite) TestUpdate() {
	now := time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		req         changelog.UpdateRequest
		wantVersion string
		wantSection string
		wantErr     bool
	}{
		{
			name: "valid changelog with previous version",
			req: changelog.UpdateRequest{
				Content: `
## [Unreleased]

### Added
- New feature X.

## [v1.2.3] - 2024-08-09
### Added
- Another.
`,
				OwnerRepo: "owner/repo",
				Now:       now,
			},
			wantVersion: "1.3.0",
			wantSection: "### Added\n- New feature X.",
		},
		{
			name: "no unreleased section returns ErrNoChangesDetected",
			req: changelog.UpdateRequest{
				Content: `## [v1.2.3] - 2024-08-09
### Added
- Another.
`,
				OwnerRepo: "owner/repo",
				Now:       now,
			},
			wantErr: true,
		},
		{
			name: "first release with module",
			req: changelog.UpdateRequest{
				Content: `# Changelog

## [Unreleased]
### Added
- Initial implementation
`,
				ModuleName: "microservice/foo",
				OwnerRepo:  "owner/repo",
				Now:        now,
			},
			wantVersion: "0.1.0",
			wantSection: "### Added\n- Initial implementation",
		},
		{
			name: "manual override appends footer",
			req: changelog.UpdateRequest{
				Content: `
## [Unreleased]
### Added
- New feature X.

## [v1.2.3] - 2024-08-09
`,
				OwnerRepo:     "owner/repo",
				ManualVersion: "9.9.9",
				ManualReason:  "hotfix",
				Actor:         "operator",
				Now:           now,
			},
			wantVersion: "9.9.9",
			wantSection: "### Added\n- New feature X.\n\nManual release by operator: hotfix",
		},
		{
			name: "manual override without reason omits dangling colon",
			req: changelog.UpdateRequest{
				Content: `
## [Unreleased]
### Added
- New feature X.

## [v1.2.3] - 2024-08-09
`,
				OwnerRepo:     "owner/repo",
				ManualVersion: "9.9.9",
				ManualReason:  "",
				Actor:         "operator",
				Now:           now,
			},
			wantVersion: "9.9.9",
			wantSection: "### Added\n- New feature X.\n\nManual release by operator",
		},
	}

	for i := range tests {
		tt := &tests[i]
		s.Run(tt.name, func() {
			res, err := changelog.Update(tt.req)
			if tt.wantErr {
				s.Require().Error(err)
				return
			}
			s.Require().NoError(err)
			s.Equal(tt.wantVersion, res.NextVersion)
			s.Equal(tt.wantSection, res.UnreleasedSection)
			s.Contains(res.NewContent, "## [Unreleased]")
			s.Contains(res.NewContent, res.NextVersion)
		})
	}
}
