package db

import (
	"github.com/steveyegge/beads/internal/storage/domain"
)

func (s *testSuite) TestConfigSQLRepository() {
	s.Run("GetMetadata", func() {
		s.Run("MissingKeyReturnsEmpty", s.configGetMetadataMissingKey)
		s.Run("RoundTrip", s.configGetMetadataRoundTrip)
	})
	s.Run("SetMetadata", func() {
		s.Run("Overwrite", s.configSetMetadataOverwrite)
	})
	s.Run("SetLocalMetadata", func() {
		s.Run("WritesToLocalMetadataTable", s.configSetLocalMetadataWrites)
	})
	s.Run("GetConfig", func() {
		s.Run("MissingKeyReturnsEmpty", s.configGetConfigMissingKey)
		s.Run("RoundTrip", s.configGetConfigRoundTrip)
	})
	s.Run("SetConfig", func() {
		s.Run("Overwrite", s.configSetConfigOverwrite)
		s.Run("IssuePrefixTrimsTrailingHyphen", s.configSetConfigIssuePrefixTrim)
		s.Run("IssuePrefixWithoutHyphenUnchanged", s.configSetConfigIssuePrefixUnchanged)
	})
}

func (s *testSuite) configRepo() domain.ConfigSQLRepository {
	return NewConfigSQLRepository(s.Runner())
}

func (s *testSuite) configGetMetadataMissingKey() {
	v, err := s.configRepo().GetMetadata(s.Ctx(), "no_such_key")
	s.Require().NoError(err)
	s.Equal("", v)
}

func (s *testSuite) configGetMetadataRoundTrip() {
	r := s.configRepo()
	s.Require().NoError(r.SetMetadata(s.Ctx(), "_project_id", "abc-123"))
	v, err := r.GetMetadata(s.Ctx(), "_project_id")
	s.Require().NoError(err)
	s.Equal("abc-123", v)
}

func (s *testSuite) configSetMetadataOverwrite() {
	r := s.configRepo()
	s.Require().NoError(r.SetMetadata(s.Ctx(), "k", "v1"))
	s.Require().NoError(r.SetMetadata(s.Ctx(), "k", "v2"))
	v, err := r.GetMetadata(s.Ctx(), "k")
	s.Require().NoError(err)
	s.Equal("v2", v)
}

func (s *testSuite) configSetLocalMetadataWrites() {
	r := s.configRepo()
	s.Require().NoError(r.SetLocalMetadata(s.Ctx(), "bd_version", "1.2.3"))

	var v string
	err := s.Runner().
		QueryRowContext(s.Ctx(), "SELECT value FROM local_metadata WHERE `key` = ?", "bd_version").
		Scan(&v)
	s.Require().NoError(err)
	s.Equal("1.2.3", v)
}

func (s *testSuite) configGetConfigMissingKey() {
	v, err := s.configRepo().GetConfig(s.Ctx(), "no_such_key")
	s.Require().NoError(err)
	s.Equal("", v)
}

func (s *testSuite) configGetConfigRoundTrip() {
	r := s.configRepo()
	s.Require().NoError(r.SetConfig(s.Ctx(), "team.sync_branch", "main"))
	v, err := r.GetConfig(s.Ctx(), "team.sync_branch")
	s.Require().NoError(err)
	s.Equal("main", v)
}

func (s *testSuite) configSetConfigOverwrite() {
	r := s.configRepo()
	s.Require().NoError(r.SetConfig(s.Ctx(), "k", "v1"))
	s.Require().NoError(r.SetConfig(s.Ctx(), "k", "v2"))
	v, err := r.GetConfig(s.Ctx(), "k")
	s.Require().NoError(err)
	s.Equal("v2", v)
}

func (s *testSuite) configSetConfigIssuePrefixTrim() {
	r := s.configRepo()
	s.Require().NoError(r.SetConfig(s.Ctx(), "issue_prefix", "bd-"))
	v, err := r.GetConfig(s.Ctx(), "issue_prefix")
	s.Require().NoError(err)
	s.Equal("bd", v)
}

func (s *testSuite) configSetConfigIssuePrefixUnchanged() {
	r := s.configRepo()
	s.Require().NoError(r.SetConfig(s.Ctx(), "issue_prefix", "bd"))
	v, err := r.GetConfig(s.Ctx(), "issue_prefix")
	s.Require().NoError(err)
	s.Equal("bd", v)
}
