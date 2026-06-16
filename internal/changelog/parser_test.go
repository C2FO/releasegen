package changelog_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/c2fo/releasegen/internal/changelog"
)

type ParserTestSuite struct {
	suite.Suite
}

func TestParserTestSuite(t *testing.T) {
	suite.Run(t, new(ParserTestSuite))
}

func (s *ParserTestSuite) TestExtractUnreleased() {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name: "Unreleased section present",
			content: `
## [Unreleased]
### Added
- New feature X.

## [1.2.3] - 2024-08-09
### Added
- Another new feature.
`,
			expected: "### Added\n- New feature X.",
		},
		{
			name: "Unreleased section with multiple lines",
			content: `
## [Unreleased]
### Added
- New feature X.
- Another feature.

## [1.2.3] - 2024-08-09
### Added
- Another new feature.
`,
			expected: "### Added\n- New feature X.\n- Another feature.",
		},
		{
			name: "No unreleased section",
			content: `
## [1.2.3] - 2024-08-09
### Added
- Another new feature.
`,
			expected: "",
		},
		{
			name: "Unreleased section no previous release",
			content: `
## [Unreleased]
### Added
- New feature X.
`,
			expected: "### Added\n- New feature X.",
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.Equal(tt.expected, changelog.ExtractUnreleased(tt.content))
		})
	}
}

func (s *ParserTestSuite) TestExtractCurrentVersion() {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name: "Current version present",
			content: `
## [Unreleased]
### Added
- x

## [1.2.3] - 2024-08-09
`,
			expected: "1.2.3",
		},
		{
			name: "Current version present - submodule prefix",
			content: `
## [Unreleased]

## [submodule/v1.2.3] - 2024-08-09
`,
			expected: "1.2.3",
		},
		{
			name: "First version (most recent appears first)",
			content: `
## [Unreleased]

## [1.2.3] - 2024-08-09
## [1.2.2] - 2024-08-08
`,
			expected: "1.2.3",
		},
		{
			name: "Linked version heading",
			content: `
## [Unreleased]

## [[v4.22.0](https://github.com/owner/repo/releases/tag/v4.22.0)] - 2025-03-10
`,
			expected: "4.22.0",
		},
		{
			name: "No previous version present (first release)",
			content: `
## [Unreleased]
### Added
- New feature X.
`,
			expected: "0.0.0",
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.Equal(tt.expected, changelog.ExtractCurrentVersion(tt.content))
		})
	}
}

// FuzzExtractUnreleased ensures the parser never panics on arbitrary input.
func FuzzExtractUnreleased(f *testing.F) {
	f.Add("")
	f.Add("## [Unreleased]\n### Added\n- x\n## [1.0.0] - 2024-01-01\n")
	f.Add("# garbage")
	f.Add("## [Unreleased]\n## [foo/v1.2.3-rc.1+build.5]")
	f.Fuzz(func(_ *testing.T, content string) {
		_ = changelog.ExtractUnreleased(content)
		_ = changelog.ExtractCurrentVersion(content)
	})
}
