package changelog_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/c2fo/releasegen/internal/changelog"
	"github.com/c2fo/releasegen/internal/config"
)

type ClassifierTestSuite struct {
	suite.Suite
}

func TestClassifierTestSuite(t *testing.T) {
	suite.Run(t, new(ClassifierTestSuite))
}

func (s *ClassifierTestSuite) TestClassify() {
	tests := []struct {
		name        string
		section     string
		custom      map[string]config.BumpType
		wantBump    config.BumpType
		wantErr     bool
		wantErrMsg  string
		wantErrType error
	}{
		{
			name:     "major with breaking change",
			section:  "### Changed\n- **BREAKING CHANGE**: API behavior changed.",
			wantBump: config.BumpMajor,
		},
		{
			name:     "minor with addition",
			section:  "### Added\n- New feature X.",
			wantBump: config.BumpMinor,
		},
		{
			name:     "minor with deprecation",
			section:  "### Deprecated\n- Old thing.",
			wantBump: config.BumpMinor,
		},
		{
			name:     "minor with security",
			section:  "### Security\n- Patched.",
			wantBump: config.BumpMinor,
		},
		{
			name:     "patch with fix",
			section:  "### Fixed\n- Fixed bug.",
			wantBump: config.BumpPatch,
		},
		{
			name:        "Changed without breaking marker errors",
			section:     "### Changed\n- Just a plain change.",
			wantErr:     true,
			wantErrType: changelog.ErrIncompleteBreaking,
		},
		{
			name:        "Empty section is ErrNoChangesDetected",
			section:     "   \n  \n",
			wantErr:     true,
			wantErrType: changelog.ErrNoChangesDetected,
		},
		{
			name:        "Unrecognized heading",
			section:     "### Notes\n- something",
			wantErr:     true,
			wantErrType: changelog.ErrUnrecognizedChangeType,
		},
		{
			name:     "Custom minor type Performance",
			section:  "### Performance\n- improved",
			custom:   map[string]config.BumpType{"performance": config.BumpMinor},
			wantBump: config.BumpMinor,
		},
		{
			name:     "Custom patch Documentation",
			section:  "### Documentation\n- updated docs",
			custom:   map[string]config.BumpType{"documentation": config.BumpPatch},
			wantBump: config.BumpPatch,
		},
		{
			name:     "Custom + builtin: builtin major wins",
			section:  "### Performance\n- improved\n### Changed\n- **BREAKING CHANGE**: x",
			custom:   map[string]config.BumpType{"performance": config.BumpMinor},
			wantBump: config.BumpMajor,
		},
		{
			name:     "Custom + builtin: minor beats patch",
			section:  "### Documentation\n- docs\n### Added\n- new",
			custom:   map[string]config.BumpType{"documentation": config.BumpPatch},
			wantBump: config.BumpMinor,
		},
		{
			name:     "Custom + builtin: patch when only fixed and docs",
			section:  "### Documentation\n- docs\n### Fixed\n- bug",
			custom:   map[string]config.BumpType{"documentation": config.BumpPatch},
			wantBump: config.BumpPatch,
		},
		{
			name:     "Custom major with BREAKING CHANGE",
			section:  "### Blah\n- this is a BREAKING CHANGE",
			custom:   map[string]config.BumpType{"blah": config.BumpMajor},
			wantBump: config.BumpMajor,
		},
		{
			name:        "Custom major without BREAKING CHANGE falls through",
			section:     "### Blah\n- not breaking",
			custom:      map[string]config.BumpType{"blah": config.BumpMajor},
			wantErr:     true,
			wantErrType: changelog.ErrUnrecognizedChangeType,
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			got, err := changelog.Classify(tt.section, tt.custom)
			if tt.wantErr {
				s.Require().Error(err)
				if tt.wantErrType != nil {
					s.Require().ErrorIs(err, tt.wantErrType)
				}
				return
			}
			s.Require().NoError(err)
			s.Equal(tt.wantBump, got)
		})
	}
}

func (s *ClassifierTestSuite) TestNextVersion() {
	tests := []struct {
		name    string
		current string
		bump    config.BumpType
		want    string
		wantErr bool
	}{
		{"major", "1.2.3", config.BumpMajor, "2.0.0", false},
		{"minor", "1.2.3", config.BumpMinor, "1.3.0", false},
		{"patch", "1.2.3", config.BumpPatch, "1.2.4", false},
		{"strip v prefix", "v1.2.3", config.BumpPatch, "1.2.4", false},
		{"first release minor", "0.0.0", config.BumpMinor, "0.1.0", false},
		{"bad current", "not.a.version", config.BumpMinor, "", true},
		{"none", "1.2.3", config.BumpNone, "", true},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			got, err := changelog.NextVersion(tt.current, tt.bump)
			if tt.wantErr {
				s.Require().Error(err)
				return
			}
			s.Require().NoError(err)
			s.Equal(tt.want, got)
		})
	}
}
