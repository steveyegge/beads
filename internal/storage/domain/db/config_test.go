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
	s.Run("GetCustomTypes", func() {
		s.Run("MissingKeyReturnsNil", s.configGetCustomTypesMissing)
		s.Run("EmptyValueReturnsNil", s.configGetCustomTypesEmpty)
		s.Run("CommaSeparated", s.configGetCustomTypesCommaSeparated)
		s.Run("JSONArray", s.configGetCustomTypesJSONArray)
		s.Run("TrimsWhitespaceAndSkipsEmpty", s.configGetCustomTypesTrimsAndSkipsEmpty)
	})
	s.Run("GetAllowedPrefixes", func() {
		s.Run("MissingKeyReturnsEmpty", s.configGetAllowedPrefixesMissing)
		s.Run("ReturnsRawValue", s.configGetAllowedPrefixesRawValue)
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

func (s *testSuite) configGetCustomTypesMissing() {
	got, err := s.configRepo().GetCustomTypes(s.Ctx())
	s.Require().NoError(err)
	s.Nil(got)
}

func (s *testSuite) configGetCustomTypesEmpty() {
	r := s.configRepo()
	s.Require().NoError(r.SetConfig(s.Ctx(), "types.custom", ""))
	got, err := r.GetCustomTypes(s.Ctx())
	s.Require().NoError(err)
	s.Nil(got)
}

func (s *testSuite) configGetCustomTypesCommaSeparated() {
	r := s.configRepo()
	s.Require().NoError(r.SetConfig(s.Ctx(), "types.custom", "molecule,gate,convoy"))
	got, err := r.GetCustomTypes(s.Ctx())
	s.Require().NoError(err)
	s.Equal([]string{"molecule", "gate", "convoy"}, got)
}

func (s *testSuite) configGetCustomTypesJSONArray() {
	r := s.configRepo()
	s.Require().NoError(r.SetConfig(s.Ctx(), "types.custom", `["gate","convoy"]`))
	got, err := r.GetCustomTypes(s.Ctx())
	s.Require().NoError(err)
	s.Equal([]string{"gate", "convoy"}, got)
}

func (s *testSuite) configGetCustomTypesTrimsAndSkipsEmpty() {
	r := s.configRepo()
	s.Require().NoError(r.SetConfig(s.Ctx(), "types.custom", "  alpha , , beta  ,"))
	got, err := r.GetCustomTypes(s.Ctx())
	s.Require().NoError(err)
	s.Equal([]string{"alpha", "beta"}, got)
}

func (s *testSuite) configGetAllowedPrefixesMissing() {
	got, err := s.configRepo().GetAllowedPrefixes(s.Ctx())
	s.Require().NoError(err)
	s.Equal("", got)
}

func (s *testSuite) configGetAllowedPrefixesRawValue() {
	r := s.configRepo()
	s.Require().NoError(r.SetConfig(s.Ctx(), "allowed_prefixes", "hacker-news, me-py-toolkit, hq-cv"))
	got, err := r.GetAllowedPrefixes(s.Ctx())
	s.Require().NoError(err)
	s.Equal("hacker-news, me-py-toolkit, hq-cv", got)
}
