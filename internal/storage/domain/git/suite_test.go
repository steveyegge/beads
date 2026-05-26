package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/steveyegge/beads/internal/storage/domain"
)

type testSuite struct {
	suite.Suite
	repo   domain.GitRepository
	tmpDir string
}

func (s *testSuite) SetupSuite() {
	if _, err := exec.LookPath("git"); err != nil {
		s.T().Skip("git binary not found")
	}
}

func (s *testSuite) SetupTest() {
	s.tmpDir = s.T().TempDir()
	s.repo = NewGitRepository(s.tmpDir)
}

func (s *testSuite) Ctx() context.Context {
	return context.Background()
}

func (s *testSuite) gitInit() {
	s.T().Helper()
	s.run("git", "init", "-q")
	s.run("git", "config", "user.email", "test@example.com")
	s.run("git", "config", "user.name", "test")
	s.run("git", "config", "commit.gpgsign", "false")
}

func (s *testSuite) run(name string, args ...string) {
	s.T().Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = s.tmpDir
	s.Require().NoError(cmd.Run())
}

func (s *testSuite) writeFile(rel, body string) {
	s.T().Helper()
	s.Require().NoError(os.WriteFile(filepath.Join(s.tmpDir, rel), []byte(body), 0600))
}

func TestDomainGit(t *testing.T) {
	suite.Run(t, &testSuite{})
}
