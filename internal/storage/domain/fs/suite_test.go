package fs

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/steveyegge/beads/internal/storage/domain"
)

type testSuite struct {
	suite.Suite
	repo domain.BeadsDirFSRepository
}

func (s *testSuite) SetupTest() {
	s.repo = NewBeadsDirFSRepository()
}

func (s *testSuite) Ctx() context.Context {
	return context.Background()
}

func (s *testSuite) tmpRoot() string {
	return s.T().TempDir()
}

func (s *testSuite) beadsDir() (string, string) {
	root := s.tmpRoot()
	return root, filepath.Join(root, ".beads")
}

func TestDomainFS(t *testing.T) {
	suite.Run(t, &testSuite{})
}
