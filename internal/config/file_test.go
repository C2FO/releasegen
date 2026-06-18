package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/c2fo/releasegen/internal/config"
)

type FileConfigTestSuite struct {
	suite.Suite
	tmpDir string
}

func TestFileConfigTestSuite(t *testing.T) {
	suite.Run(t, new(FileConfigTestSuite))
}

func (s *FileConfigTestSuite) SetupTest() {
	s.tmpDir = s.T().TempDir()
}

func (s *FileConfigTestSuite) writeFile(name, body string) {
	s.T().Helper()
	s.Require().NoError(os.WriteFile(filepath.Join(s.tmpDir, name), []byte(body), 0o600))
}

func (s *FileConfigTestSuite) TestLoadFile_NotPresent() {
	fc, path, err := config.LoadFile(s.tmpDir)
	s.Require().NoError(err)
	s.Nil(fc)
	s.Empty(path)
}

func (s *FileConfigTestSuite) TestLoadFile_YamlExtension() {
	s.writeFile(".releasegen.yaml", `
custom_change_types:
  Documentation: minor
  Performance: patch
exclude_dirs:
  - some/app
  - some/other/app
self_release_module: tools/releasegen
self_release_repo: myorg/myrepo
`)
	fc, path, err := config.LoadFile(s.tmpDir)
	s.Require().NoError(err)
	s.Require().NotNil(fc)
	s.Equal(filepath.Join(s.tmpDir, ".releasegen.yaml"), path)
	s.Equal("minor", fc.CustomChangeTypes["Documentation"])
	s.Equal("patch", fc.CustomChangeTypes["Performance"])
	s.Equal([]string{"some/app", "some/other/app"}, fc.ExcludeDirs)
	s.Require().NotNil(fc.SelfReleaseModule)
	s.Equal("tools/releasegen", *fc.SelfReleaseModule)
	s.Require().NotNil(fc.SelfReleaseRepo)
	s.Equal("myorg/myrepo", *fc.SelfReleaseRepo)
}

func (s *FileConfigTestSuite) TestLoadFile_YmlExtensionAlsoAccepted() {
	s.writeFile(".releasegen.yml", "exclude_dirs:\n  - x\n")
	fc, path, err := config.LoadFile(s.tmpDir)
	s.Require().NoError(err)
	s.Require().NotNil(fc)
	s.Equal(filepath.Join(s.tmpDir, ".releasegen.yml"), path)
	s.Equal([]string{"x"}, fc.ExcludeDirs)
}

func (s *FileConfigTestSuite) TestLoadFile_YamlWinsOverYmlWhenBothExist() {
	s.writeFile(".releasegen.yaml", "exclude_dirs:\n  - from-yaml\n")
	s.writeFile(".releasegen.yml", "exclude_dirs:\n  - from-yml\n")
	fc, path, err := config.LoadFile(s.tmpDir)
	s.Require().NoError(err)
	s.Require().NotNil(fc)
	s.Equal(filepath.Join(s.tmpDir, ".releasegen.yaml"), path)
	s.Equal([]string{"from-yaml"}, fc.ExcludeDirs)
}

func (s *FileConfigTestSuite) TestLoadFile_UnknownKeyRejected() {
	s.writeFile(".releasegen.yaml", "completely_made_up: 1\n")
	_, _, err := config.LoadFile(s.tmpDir)
	s.Require().Error(err)
	s.Contains(err.Error(), "completely_made_up")
}

func (s *FileConfigTestSuite) TestLoadFile_MalformedYAML() {
	s.writeFile(".releasegen.yaml", "exclude_dirs: [unclosed\n")
	_, _, err := config.LoadFile(s.tmpDir)
	s.Require().Error(err)
}

func (s *FileConfigTestSuite) TestApplyFile_FillsZeroFields() {
	cfg := &config.Config{RepoRoot: s.tmpDir}
	repo := "myorg/myrepo"
	mod := "tools/releasegen"
	fc := &config.FileConfig{
		CustomChangeTypes: map[string]string{"Documentation": "patch"},
		ExcludeDirs:       []string{"a", "b/"},
		SelfReleaseModule: &mod,
		SelfReleaseRepo:   &repo,
	}
	// Make sure no env interference.
	s.T().Setenv("RELEASEGEN_SELF_MODULE", "")
	s.Require().NoError(os.Unsetenv("RELEASEGEN_SELF_MODULE"))
	s.T().Setenv("RELEASEGEN_SELF_REPO", "")
	s.Require().NoError(os.Unsetenv("RELEASEGEN_SELF_REPO"))

	s.Require().NoError(config.ApplyFile(cfg, fc))
	s.Equal(map[string]config.BumpType{"documentation": config.BumpPatch}, cfg.CustomTypes)
	s.Equal([]string{"a/", "b/"}, cfg.ExcludeDirs)
	s.Equal("tools/releasegen", cfg.SelfReleaseModule)
	s.Equal("myorg/myrepo", cfg.SelfReleaseRepo)
}

func (s *FileConfigTestSuite) TestApplyFile_EnvWinsOverFile() {
	s.T().Setenv("RELEASEGEN_SELF_REPO", "env-org/env-repo")
	envCfg, err := config.FromEnv()
	s.Require().NoError(err)

	repo := "file-org/file-repo"
	fc := &config.FileConfig{SelfReleaseRepo: &repo}
	s.Require().NoError(config.ApplyFile(envCfg, fc))
	s.Equal("env-org/env-repo", envCfg.SelfReleaseRepo, "env value must beat the file")
}

func (s *FileConfigTestSuite) TestApplyFile_FileBeatsBuiltInDefault() {
	// Clear env so FromEnv leaves the built-in default in place.
	s.T().Setenv("RELEASEGEN_SELF_REPO", "")
	s.Require().NoError(os.Unsetenv("RELEASEGEN_SELF_REPO"))
	cfg, err := config.FromEnv()
	s.Require().NoError(err)
	s.Equal("c2fo/releasegen", cfg.SelfReleaseRepo)

	disabled := ""
	fc := &config.FileConfig{SelfReleaseRepo: &disabled}
	s.Require().NoError(config.ApplyFile(cfg, fc))
	s.Empty(cfg.SelfReleaseRepo, "file value must replace the built-in default when env is unset")
}

func (s *FileConfigTestSuite) TestApplyFile_DoesNotClobberExistingCustomTypes() {
	cfg := &config.Config{
		RepoRoot:    s.tmpDir,
		CustomTypes: map[string]config.BumpType{"documentation": config.BumpMinor},
	}
	fc := &config.FileConfig{
		CustomChangeTypes: map[string]string{"Documentation": "patch"},
	}
	s.Require().NoError(config.ApplyFile(cfg, fc))
	s.Equal(config.BumpMinor, cfg.CustomTypes["documentation"], "env-provided custom types must beat the file")
}

func (s *FileConfigTestSuite) TestApplyFile_NilFileIsNoOp() {
	cfg := &config.Config{RepoRoot: s.tmpDir}
	s.Require().NoError(config.ApplyFile(cfg, nil))
	s.Equal(&config.Config{RepoRoot: s.tmpDir}, cfg)
}

func (s *FileConfigTestSuite) TestApplyFile_InvalidBump() {
	cfg := &config.Config{RepoRoot: s.tmpDir}
	fc := &config.FileConfig{
		CustomChangeTypes: map[string]string{"Documentation": "bogus"},
	}
	s.Require().Error(config.ApplyFile(cfg, fc))
}
