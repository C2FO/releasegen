package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/c2fo/releasegen/internal/changelog"
	"github.com/c2fo/releasegen/internal/config"
	"github.com/c2fo/releasegen/internal/forge"
	"github.com/c2fo/releasegen/internal/vcs"
)

type CLITestSuite struct {
	suite.Suite
}

func TestCLITestSuite(t *testing.T) {
	suite.Run(t, new(CLITestSuite))
}

func (s *CLITestSuite) TestApplyFlagOverrides_AllFieldsApplied() {
	cfg := &config.Config{}
	err := applyFlagOverrides(cfg, flagOverrides{
		repoRoot:      "/repo",
		dryRun:        true,
		debug:         true,
		summaryFile:   "/tmp/sum.json",
		manualVersion: "v9.9.9",
		reason:        "hotfix",
		excludeDirs:   "vendor/, third_party/",
		ownerRepo:     "owner/repo",
		actor:         "alice",
		branch:        "main",
		token:         "tok",
	})
	s.Require().NoError(err)
	s.Equal("/repo", cfg.RepoRoot)
	s.True(cfg.DryRun)
	s.True(cfg.Debug)
	s.Equal("/tmp/sum.json", cfg.SummaryFile)
	s.Equal("v9.9.9", cfg.ManualVersion)
	s.Equal("hotfix", cfg.Reason)
	s.Equal([]string{"vendor/", "third_party/"}, cfg.ExcludeDirs)
	s.Equal("owner/repo", cfg.OwnerRepo)
	s.Equal("alice", cfg.Actor)
	s.Equal("main", cfg.Branch)
	s.Equal("tok", cfg.Token)
}

func (s *CLITestSuite) TestApplyFlagOverrides_DoesNotClobberWithEmpty() {
	cfg := &config.Config{
		RepoRoot:  "/preset",
		OwnerRepo: "owner/repo",
		Token:     "tok",
	}
	err := applyFlagOverrides(cfg, flagOverrides{})
	s.Require().NoError(err)
	s.Equal("/preset", cfg.RepoRoot)
	s.Equal("owner/repo", cfg.OwnerRepo)
	s.Equal("tok", cfg.Token)
}

func (s *CLITestSuite) TestApplyFlagOverrides_BadCustomTypesReturnsError() {
	cfg := &config.Config{}
	err := applyFlagOverrides(cfg, flagOverrides{customTypes: "not-a-pair"})
	s.Require().Error(err)
	s.Contains(err.Error(), "custom-change-types")
}

func (s *CLITestSuite) TestExitCodeFor() {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, exitOK},
		{"cliError-config", cliError{code: exitConfigErr, err: errors.New("bad")}, exitConfigErr},
		{"unwrapped-cliError", fmt.Errorf("wrap: %w", cliError{code: exitVCSErr, err: errors.New("x")}), exitVCSErr},
		{"changelog-unknown", fmt.Errorf("module x: %w", changelog.ErrUnrecognizedChangeType), exitChangelogErr},
		{"changelog-breaking", fmt.Errorf("module x: %w", changelog.ErrIncompleteBreaking), exitChangelogErr},
		{"vcs", fmt.Errorf("module x: %w", vcs.ErrVCS), exitVCSErr},
		{"forge", fmt.Errorf("module x: %w", forge.ErrForge), exitForgeErr},
		{"unknown", errors.New("something else"), exitInternal},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.Equal(tc.want, exitCodeFor(tc.err))
		})
	}
}

func (s *CLITestSuite) TestCliError_UnwrapAndError() {
	inner := errors.New("boom")
	wrapped := cliError{code: exitVCSErr, err: inner}

	s.Equal("boom", wrapped.Error())
	s.Same(inner, wrapped.Unwrap())
	s.Require().ErrorIs(wrapped, inner)
}

func (s *CLITestSuite) TestNewRootCmd_HasExpectedFlags() {
	cmd := newRootCmd()
	s.NotNil(cmd.Flags().Lookup("dry-run"))
	s.NotNil(cmd.Flags().Lookup("repository"))
	s.NotNil(cmd.Flags().Lookup("custom-change-types"))
	s.NotNil(cmd.Flags().Lookup("summary-file"))
	s.NotNil(cmd.Flags().Lookup("version"))
}

func (s *CLITestSuite) TestNewRootCmd_VersionShortCircuits() {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--version"})
	s.Require().NoError(cmd.Execute())
}
