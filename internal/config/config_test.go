package config_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/c2fo/releasegen/internal/config"
)

type ConfigTestSuite struct {
	suite.Suite
}

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(ConfigTestSuite))
}

func (s *ConfigTestSuite) TestParseBumpType() {
	tests := []struct {
		name    string
		in      string
		want    config.BumpType
		wantErr bool
	}{
		{"major", "major", config.BumpMajor, false},
		{"MINOR (case-insensitive)", "MINOR", config.BumpMinor, false},
		{"patch with whitespace", "  patch  ", config.BumpPatch, false},
		{"empty", "", config.BumpNone, true},
		{"unknown", "huge", config.BumpNone, true},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			got, err := config.ParseBumpType(tt.in)
			if tt.wantErr {
				s.Require().Error(err)
				return
			}
			s.Require().NoError(err)
			s.Equal(tt.want, got)
		})
	}
}

func (s *ConfigTestSuite) TestBumpTypeString() {
	s.Equal("major", config.BumpMajor.String())
	s.Equal("minor", config.BumpMinor.String())
	s.Equal("patch", config.BumpPatch.String())
	s.Equal("none", config.BumpNone.String())
}

func (s *ConfigTestSuite) TestParseExcludeDirs() {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"comma-separated", "a,b", []string{"a/", "b/"}},
		{"newline-separated", "a\nb", []string{"a/", "b/"}},
		{"trailing slash preserved", "a/,b", []string{"a/", "b/"}},
		{"whitespace trimmed", " a , b ", []string{"a/", "b/"}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.Equal(tt.want, config.ParseExcludeDirs(tt.in))
		})
	}
}

func (s *ConfigTestSuite) TestParseCustomTypes() {
	got, err := config.ParseCustomTypes("Documentation:patch\nPerformance:minor\n")
	s.Require().NoError(err)
	s.Equal(map[string]config.BumpType{
		"documentation": config.BumpPatch,
		"performance":   config.BumpMinor,
	}, got)
}

func (s *ConfigTestSuite) TestParseCustomTypesError() {
	_, err := config.ParseCustomTypes("Documentation")
	s.Require().Error(err)
	_, err = config.ParseCustomTypes("Documentation:bogus")
	s.Require().Error(err)
}

func (s *ConfigTestSuite) TestValidate() {
	good := &config.Config{
		Token:     "x",
		OwnerRepo: "owner/repo",
		Actor:     "me",
		Branch:    "main",
		RepoRoot:  ".",
	}
	s.Require().NoError(good.Validate())

	cases := map[string]func(c *config.Config){
		"missing token":      func(c *config.Config) { c.Token = "" },
		"missing repo":       func(c *config.Config) { c.OwnerRepo = "" },
		"malformed repo":     func(c *config.Config) { c.OwnerRepo = "owner-only" },
		"missing actor":      func(c *config.Config) { c.Actor = "" },
		"missing branch":     func(c *config.Config) { c.Branch = "" },
		"missing repo root":  func(c *config.Config) { c.RepoRoot = "" },
		"bad manual version": func(c *config.Config) { c.ManualVersion = "not-semver" },
	}
	for name, mutate := range cases {
		s.Run(name, func() {
			c := *good
			mutate(&c)
			s.Require().Error(c.Validate())
		})
	}
}

func (s *ConfigTestSuite) TestOwnerRepoSplit() {
	c := &config.Config{OwnerRepo: "c2fo/releasegen"}
	s.Equal("c2fo", c.Owner())
	s.Equal("releasegen", c.Repo())
}

func (s *ConfigTestSuite) TestFromEnv_Defaults() {
	// Clear any inherited values to assert true defaults.
	for _, k := range []string{"REPO_ROOT", "SUMMARY_FILE", "RELEASEGEN_SELF_MODULE", "RELEASEGEN_SELF_REPO"} {
		s.T().Setenv(k, "")
		s.Require().NoError(os.Unsetenv(k))
	}
	cfg, err := config.FromEnv()
	s.Require().NoError(err)
	s.Equal(".", cfg.RepoRoot)
	s.Empty(cfg.SummaryFile)
	s.Equal("releasegen", cfg.SelfReleaseModule)
	s.Equal("c2fo/releasegen", cfg.SelfReleaseRepo)
}

func (s *ConfigTestSuite) TestFromEnv_OverridesViaEnv() {
	s.T().Setenv("REPO_ROOT", "/work/checkout")
	s.T().Setenv("SUMMARY_FILE", "/tmp/summary.json")
	s.T().Setenv("RELEASEGEN_SELF_MODULE", "internal-tool")
	s.T().Setenv("RELEASEGEN_SELF_REPO", "myorg/myrepo")
	cfg, err := config.FromEnv()
	s.Require().NoError(err)
	s.Equal("/work/checkout", cfg.RepoRoot)
	s.Equal("/tmp/summary.json", cfg.SummaryFile)
	s.Equal("internal-tool", cfg.SelfReleaseModule)
	s.Equal("myorg/myrepo", cfg.SelfReleaseRepo)
}
