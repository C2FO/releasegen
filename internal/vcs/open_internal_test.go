package vcs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/stretchr/testify/suite"
)

type OpenInternalTestSuite struct {
	suite.Suite
}

func (s *OpenInternalTestSuite) TestDotGitFilesystem() {
	tests := []struct {
		name          string
		setup         func(root string)
		expectedRoot  func(root string) string
		expectedError string
		expectedIs    error
	}{
		{
			name:         "git directory",
			setup:        func(root string) { s.Require().NoError(os.Mkdir(filepath.Join(root, ".git"), 0o750)) },
			expectedRoot: func(root string) string { return filepath.Join(root, ".git") },
		},
		{
			name: "gitdir file with absolute path",
			setup: func(root string) {
				s.writeFile(filepath.Join(root, ".git"), "gitdir: /abs/target\n")
			},
			expectedRoot: func(string) string { return "/abs/target" },
		},
		{
			name: "gitdir file with relative path",
			setup: func(root string) {
				s.writeFile(filepath.Join(root, ".git"), "gitdir: ../main/.git/worktrees/wt\n")
			},
			expectedRoot: func(root string) string { return filepath.Join(filepath.Dir(root), "main", ".git", "worktrees", "wt") },
		},
		{
			name:       "missing .git",
			setup:      func(string) {},
			expectedIs: git.ErrRepositoryNotExists,
		},
		{
			name: "gitdir file without prefix",
			setup: func(root string) {
				s.writeFile(filepath.Join(root, ".git"), "not a pointer\n")
			},
			expectedError: "has no gitdir:",
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			root := filepath.Join(s.tempDir(), "wt")
			s.Require().NoError(os.Mkdir(root, 0o750))
			tt.setup(root)

			fs, err := dotGitFilesystem(root)
			switch {
			case tt.expectedIs != nil:
				s.Require().ErrorIs(err, tt.expectedIs)
			case tt.expectedError != "":
				s.Require().ErrorContains(err, tt.expectedError)
			default:
				s.Require().NoError(err)
				s.Equal(tt.expectedRoot(root), fs.Root())
			}
		})
	}
}

func (s *OpenInternalTestSuite) TestCommonDirFilesystem() {
	tests := []struct {
		name        string
		commondir   *string
		makeTarget  bool
		expectedNil bool
		expectedIs  error
	}{
		{name: "no commondir file", expectedNil: true},
		{name: "empty commondir file", commondir: ptr(""), expectedNil: true},
		{name: "relative commondir", commondir: ptr("../.."), makeTarget: true},
		{name: "commondir target missing", commondir: ptr("../../gone"), expectedIs: git.ErrRepositoryIncomplete},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			base := s.tempDir()
			dot := filepath.Join(base, "main", ".git", "worktrees", "wt")
			s.Require().NoError(os.MkdirAll(dot, 0o750))
			if tt.commondir != nil {
				s.writeFile(filepath.Join(dot, "commondir"), *tt.commondir+"\n")
			}

			fs, err := commonDirFilesystem(osfs.New(dot))
			switch {
			case tt.expectedIs != nil:
				s.Require().ErrorIs(err, tt.expectedIs)
			case tt.expectedNil:
				s.Require().NoError(err)
				s.Nil(fs)
			default:
				s.Require().NoError(err)
				s.Equal(filepath.Join(base, "main", ".git"), fs.Root())
			}
		})
	}
}

// tempDir returns a temp directory with symlinks resolved (macOS /var is a
// symlink to /private/var), so paths compare equal to what osfs reports.
func (s *OpenInternalTestSuite) tempDir() string {
	dir, err := filepath.EvalSymlinks(s.T().TempDir())
	s.Require().NoError(err)
	return dir
}

func (s *OpenInternalTestSuite) writeFile(path, content string) {
	s.Require().NoError(os.WriteFile(path, []byte(content), 0o600))
}

func ptr(s string) *string { return &s }

func TestOpenInternalTestSuite(t *testing.T) {
	suite.Run(t, new(OpenInternalTestSuite))
}
