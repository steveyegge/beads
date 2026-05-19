package fs

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/steveyegge/beads/internal/config"
)

func (s *testSuite) TestBeadsDirFSRepository() {
	s.Run("CreateBeadsDir", func() {
		s.Run("EmptyPathErrors", s.createBeadsDirEmptyPath)
		s.Run("CreatesDirectoryWithPerms", s.createBeadsDirCreates)
		s.Run("IdempotentOnExisting", s.createBeadsDirIdempotent)
	})
	s.Run("BeadsDirExists", func() {
		s.Run("MissingReturnsFalse", s.beadsDirExistsMissing)
		s.Run("PresentReturnsTrue", s.beadsDirExistsPresent)
		s.Run("FileNotDirReturnsFalse", s.beadsDirExistsIsFile)
	})
	s.Run("WriteBeadsGitignore", func() {
		s.Run("WritesTemplate", s.writeBeadsGitignoreWrites)
		s.Run("IdempotentOnMatchingContent", s.writeBeadsGitignoreIdempotent)
		s.Run("OverwritesDifferingContent", s.writeBeadsGitignoreOverwrites)
	})
	s.Run("BeadsGitignoreExists", func() {
		s.Run("MissingReturnsFalse", s.beadsGitignoreExistsMissing)
		s.Run("PresentReturnsTrue", s.beadsGitignoreExistsPresent)
	})
	s.Run("WriteProjectGitignore", func() {
		s.Run("EmptyRepoRootErrors", s.writeProjectGitignoreEmptyRoot)
		s.Run("CreatesWithHeaderAndPatterns", s.writeProjectGitignoreCreates)
		s.Run("AppendsToExisting", s.writeProjectGitignoreAppends)
		s.Run("SkipsAlreadyPresentPatterns", s.writeProjectGitignoreNoDuplicates)
		s.Run("NoChangeWhenAllPresent", s.writeProjectGitignoreNoOpWhenComplete)
		s.Run("AddsLeadingNewlineWhenMissing", s.writeProjectGitignoreFixesTrailingNewline)
	})
	s.Run("ProjectGitignoreExists", func() {
		s.Run("MissingReturnsFalse", s.projectGitignoreExistsMissing)
		s.Run("PresentReturnsTrue", s.projectGitignoreExistsPresent)
	})
	s.Run("WriteInteractionsLog", func() {
		s.Run("CreatesEmptyFile", s.writeInteractionsLogCreates)
		s.Run("PreservesExisting", s.writeInteractionsLogPreserves)
	})
	s.Run("WriteReadme", func() {
		s.Run("CreatesFromTemplate", s.writeReadmeCreates)
		s.Run("PreservesExisting", s.writeReadmePreserves)
	})
	s.Run("MetadataJSON", func() {
		s.Run("ReadMissingReturnsNil", s.readMetadataJSONMissing)
		s.Run("WriteThenReadRoundTrip", s.metadataJSONRoundTrip)
		s.Run("WriteOverwrites", s.metadataJSONOverwrite)
	})
	s.Run("ConfigYAML", func() {
		s.Run("ReadMissingReturnsNil", s.readConfigYAMLMissing)
		s.Run("WriteThenReadRoundTrip", s.configYAMLRoundTrip)
		s.Run("WriteOverwrites", s.configYAMLOverwrite)
	})
}

func (s *testSuite) createBeadsDirEmptyPath() {
	err := s.repo.CreateBeadsDir(s.Ctx(), "")
	s.Require().Error(err)
}

func (s *testSuite) createBeadsDirCreates() {
	_, dir := s.beadsDir()
	s.Require().NoError(s.repo.CreateBeadsDir(s.Ctx(), dir))

	info, err := os.Stat(dir)
	s.Require().NoError(err)
	s.True(info.IsDir())

	if runtime.GOOS != "windows" {
		s.Equal(config.BeadsDirPerm, info.Mode().Perm())
	}
}

func (s *testSuite) createBeadsDirIdempotent() {
	_, dir := s.beadsDir()
	s.Require().NoError(s.repo.CreateBeadsDir(s.Ctx(), dir))
	s.Require().NoError(s.repo.CreateBeadsDir(s.Ctx(), dir))
}

func (s *testSuite) beadsDirExistsMissing() {
	_, dir := s.beadsDir()
	exists, err := s.repo.BeadsDirExists(s.Ctx(), dir)
	s.Require().NoError(err)
	s.False(exists)
}

func (s *testSuite) beadsDirExistsPresent() {
	_, dir := s.beadsDir()
	s.Require().NoError(os.MkdirAll(dir, 0700))
	exists, err := s.repo.BeadsDirExists(s.Ctx(), dir)
	s.Require().NoError(err)
	s.True(exists)
}

func (s *testSuite) beadsDirExistsIsFile() {
	_, path := s.beadsDir()
	s.Require().NoError(os.WriteFile(path, []byte("not a dir"), 0600))
	exists, err := s.repo.BeadsDirExists(s.Ctx(), path)
	s.Require().NoError(err)
	s.False(exists)
}

func (s *testSuite) writeBeadsGitignoreWrites() {
	_, dir := s.beadsDir()
	s.Require().NoError(os.MkdirAll(dir, 0700))
	s.Require().NoError(s.repo.WriteBeadsGitignore(s.Ctx(), dir))

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	s.Require().NoError(err)
	s.Equal(beadsGitignoreTemplate, string(data))
}

func (s *testSuite) writeBeadsGitignoreIdempotent() {
	_, dir := s.beadsDir()
	s.Require().NoError(os.MkdirAll(dir, 0700))
	path := filepath.Join(dir, ".gitignore")
	s.Require().NoError(os.WriteFile(path, []byte(beadsGitignoreTemplate), 0600))

	before, err := os.Stat(path)
	s.Require().NoError(err)

	s.Require().NoError(s.repo.WriteBeadsGitignore(s.Ctx(), dir))

	after, err := os.Stat(path)
	s.Require().NoError(err)
	s.Equal(before.ModTime(), after.ModTime(), "file should not be rewritten when content already matches")
}

func (s *testSuite) writeBeadsGitignoreOverwrites() {
	_, dir := s.beadsDir()
	s.Require().NoError(os.MkdirAll(dir, 0700))
	path := filepath.Join(dir, ".gitignore")
	s.Require().NoError(os.WriteFile(path, []byte("stale\n"), 0600))

	s.Require().NoError(s.repo.WriteBeadsGitignore(s.Ctx(), dir))

	data, err := os.ReadFile(path)
	s.Require().NoError(err)
	s.Equal(beadsGitignoreTemplate, string(data))
}

func (s *testSuite) beadsGitignoreExistsMissing() {
	_, dir := s.beadsDir()
	s.Require().NoError(os.MkdirAll(dir, 0700))
	exists, err := s.repo.BeadsGitignoreExists(s.Ctx(), dir)
	s.Require().NoError(err)
	s.False(exists)
}

func (s *testSuite) beadsGitignoreExistsPresent() {
	_, dir := s.beadsDir()
	s.Require().NoError(os.MkdirAll(dir, 0700))
	s.Require().NoError(os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("x"), 0600))
	exists, err := s.repo.BeadsGitignoreExists(s.Ctx(), dir)
	s.Require().NoError(err)
	s.True(exists)
}

func (s *testSuite) writeProjectGitignoreEmptyRoot() {
	err := s.repo.WriteProjectGitignore(s.Ctx(), "")
	s.Require().Error(err)
}

func (s *testSuite) writeProjectGitignoreCreates() {
	root := s.tmpRoot()
	s.Require().NoError(s.repo.WriteProjectGitignore(s.Ctx(), root))

	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	s.Require().NoError(err)
	body := string(data)

	s.Contains(body, projectGitignoreHeader)
	for _, pattern := range projectGitignorePatterns {
		s.Contains(body, pattern)
	}
}

func (s *testSuite) writeProjectGitignoreAppends() {
	root := s.tmpRoot()
	path := filepath.Join(root, ".gitignore")
	preexisting := "node_modules/\n*.log\n"
	s.Require().NoError(os.WriteFile(path, []byte(preexisting), 0644))

	s.Require().NoError(s.repo.WriteProjectGitignore(s.Ctx(), root))

	data, err := os.ReadFile(path)
	s.Require().NoError(err)
	body := string(data)

	s.True(strings.HasPrefix(body, preexisting), "preexisting content must be preserved at the top")
	s.Contains(body, projectGitignoreHeader)
	for _, pattern := range projectGitignorePatterns {
		s.Contains(body, pattern)
	}
}

func (s *testSuite) writeProjectGitignoreNoDuplicates() {
	root := s.tmpRoot()
	path := filepath.Join(root, ".gitignore")
	preexisting := ".dolt/\nnode_modules/\n"
	s.Require().NoError(os.WriteFile(path, []byte(preexisting), 0644))

	s.Require().NoError(s.repo.WriteProjectGitignore(s.Ctx(), root))

	data, err := os.ReadFile(path)
	s.Require().NoError(err)

	s.Equal(1, strings.Count(string(data), ".dolt/\n"), "must not duplicate existing patterns")
}

func (s *testSuite) writeProjectGitignoreNoOpWhenComplete() {
	root := s.tmpRoot()
	path := filepath.Join(root, ".gitignore")
	var buf bytes.Buffer
	buf.WriteString("existing\n")
	buf.WriteString(projectGitignoreHeader + "\n")
	for _, pattern := range projectGitignorePatterns {
		buf.WriteString(pattern + "\n")
	}
	s.Require().NoError(os.WriteFile(path, buf.Bytes(), 0644))

	before, err := os.Stat(path)
	s.Require().NoError(err)

	s.Require().NoError(s.repo.WriteProjectGitignore(s.Ctx(), root))

	after, err := os.Stat(path)
	s.Require().NoError(err)
	s.Equal(before.ModTime(), after.ModTime(), "must not rewrite when all patterns already present")
}

func (s *testSuite) writeProjectGitignoreFixesTrailingNewline() {
	root := s.tmpRoot()
	path := filepath.Join(root, ".gitignore")
	s.Require().NoError(os.WriteFile(path, []byte("no-trailing-newline"), 0644))

	s.Require().NoError(s.repo.WriteProjectGitignore(s.Ctx(), root))

	data, err := os.ReadFile(path)
	s.Require().NoError(err)
	body := string(data)

	s.True(strings.HasPrefix(body, "no-trailing-newline\n"), "trailing newline must be inserted before appended content")
	s.Contains(body, projectGitignoreHeader)
}

func (s *testSuite) projectGitignoreExistsMissing() {
	root := s.tmpRoot()
	exists, err := s.repo.ProjectGitignoreExists(s.Ctx(), root)
	s.Require().NoError(err)
	s.False(exists)
}

func (s *testSuite) projectGitignoreExistsPresent() {
	root := s.tmpRoot()
	s.Require().NoError(os.WriteFile(filepath.Join(root, ".gitignore"), []byte("x"), 0644))
	exists, err := s.repo.ProjectGitignoreExists(s.Ctx(), root)
	s.Require().NoError(err)
	s.True(exists)
}

func (s *testSuite) writeInteractionsLogCreates() {
	_, dir := s.beadsDir()
	s.Require().NoError(os.MkdirAll(dir, 0700))
	s.Require().NoError(s.repo.WriteInteractionsLog(s.Ctx(), dir))

	data, err := os.ReadFile(filepath.Join(dir, "interactions.jsonl"))
	s.Require().NoError(err)
	s.Empty(data)
}

func (s *testSuite) writeInteractionsLogPreserves() {
	_, dir := s.beadsDir()
	s.Require().NoError(os.MkdirAll(dir, 0700))
	path := filepath.Join(dir, "interactions.jsonl")
	existing := []byte(`{"event":"x"}` + "\n")
	s.Require().NoError(os.WriteFile(path, existing, 0644))

	s.Require().NoError(s.repo.WriteInteractionsLog(s.Ctx(), dir))

	data, err := os.ReadFile(path)
	s.Require().NoError(err)
	s.Equal(existing, data)
}

func (s *testSuite) writeReadmeCreates() {
	_, dir := s.beadsDir()
	s.Require().NoError(os.MkdirAll(dir, 0700))
	s.Require().NoError(s.repo.WriteReadme(s.Ctx(), dir))

	data, err := os.ReadFile(filepath.Join(dir, "README.md"))
	s.Require().NoError(err)
	s.Equal(beadsReadmeTemplate, string(data))
}

func (s *testSuite) writeReadmePreserves() {
	_, dir := s.beadsDir()
	s.Require().NoError(os.MkdirAll(dir, 0700))
	path := filepath.Join(dir, "README.md")
	custom := []byte("# My custom readme\n")
	s.Require().NoError(os.WriteFile(path, custom, 0644))

	s.Require().NoError(s.repo.WriteReadme(s.Ctx(), dir))

	data, err := os.ReadFile(path)
	s.Require().NoError(err)
	s.Equal(custom, data)
}

func (s *testSuite) readMetadataJSONMissing() {
	_, dir := s.beadsDir()
	s.Require().NoError(os.MkdirAll(dir, 0700))
	data, err := s.repo.ReadMetadataJSON(s.Ctx(), dir)
	s.Require().NoError(err)
	s.Nil(data)
}

func (s *testSuite) metadataJSONRoundTrip() {
	_, dir := s.beadsDir()
	s.Require().NoError(os.MkdirAll(dir, 0700))
	body := []byte(`{"_project_id":"abc-123"}`)

	s.Require().NoError(s.repo.WriteMetadataJSON(s.Ctx(), dir, body))

	got, err := s.repo.ReadMetadataJSON(s.Ctx(), dir)
	s.Require().NoError(err)
	s.Equal(body, got)
}

func (s *testSuite) metadataJSONOverwrite() {
	_, dir := s.beadsDir()
	s.Require().NoError(os.MkdirAll(dir, 0700))
	s.Require().NoError(s.repo.WriteMetadataJSON(s.Ctx(), dir, []byte(`{"v":1}`)))
	s.Require().NoError(s.repo.WriteMetadataJSON(s.Ctx(), dir, []byte(`{"v":2}`)))

	got, err := s.repo.ReadMetadataJSON(s.Ctx(), dir)
	s.Require().NoError(err)
	s.Equal([]byte(`{"v":2}`), got)
}

func (s *testSuite) readConfigYAMLMissing() {
	_, dir := s.beadsDir()
	s.Require().NoError(os.MkdirAll(dir, 0700))
	data, err := s.repo.ReadConfigYAML(s.Ctx(), dir)
	s.Require().NoError(err)
	s.Nil(data)
}

func (s *testSuite) configYAMLRoundTrip() {
	_, dir := s.beadsDir()
	s.Require().NoError(os.MkdirAll(dir, 0700))
	body := []byte("issue_prefix: bd\n")

	s.Require().NoError(s.repo.WriteConfigYAML(s.Ctx(), dir, body))

	got, err := s.repo.ReadConfigYAML(s.Ctx(), dir)
	s.Require().NoError(err)
	s.Equal(body, got)
}

func (s *testSuite) configYAMLOverwrite() {
	_, dir := s.beadsDir()
	s.Require().NoError(os.MkdirAll(dir, 0700))
	s.Require().NoError(s.repo.WriteConfigYAML(s.Ctx(), dir, []byte("v: 1\n")))
	s.Require().NoError(s.repo.WriteConfigYAML(s.Ctx(), dir, []byte("v: 2\n")))

	got, err := s.repo.ReadConfigYAML(s.Ctx(), dir)
	s.Require().NoError(err)
	s.Equal([]byte("v: 2\n"), got)
}
