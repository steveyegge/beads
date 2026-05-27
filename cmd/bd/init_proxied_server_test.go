package main

import (
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildProxiedServerClientInfo(t *testing.T) {
	t.Run("all empty returns nil", func(t *testing.T) {
		info, err := buildProxiedServerClientInfo("", "", "")
		require.NoError(t, err)
		assert.Nil(t, info)
	})

	t.Run("absolute paths pass through cleaned", func(t *testing.T) {
		info, err := buildProxiedServerClientInfo("/var/lib/beads/proxieddb", "/etc/dolt/server.yaml", "/var/log/server.log")
		require.NoError(t, err)
		require.NotNil(t, info)
		assert.Equal(t, "/var/lib/beads/proxieddb", info.RootPath)
		assert.Equal(t, "/etc/dolt/server.yaml", info.ConfigPath)
		assert.Equal(t, "/var/log/server.log", info.LogPath)
	})

	t.Run("filepath.Clean normalizes redundant separators and . segments", func(t *testing.T) {
		info, err := buildProxiedServerClientInfo("/var/lib//beads/./proxieddb", "", "")
		require.NoError(t, err)
		require.NotNil(t, info)
		assert.Equal(t, "/var/lib/beads/proxieddb", info.RootPath)
	})

	t.Run("mixed absolute + empty", func(t *testing.T) {
		info, err := buildProxiedServerClientInfo("/var/lib/beads/proxieddb", "", "/var/log/server.log")
		require.NoError(t, err)
		require.NotNil(t, info)
		assert.Equal(t, "/var/lib/beads/proxieddb", info.RootPath)
		assert.Equal(t, "", info.ConfigPath)
		assert.Equal(t, "/var/log/server.log", info.LogPath)
	})

	t.Run("relative root path is rejected", func(t *testing.T) {
		_, err := buildProxiedServerClientInfo("alt-root", "", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not absolute")
	})

	t.Run("relative config path is rejected", func(t *testing.T) {
		_, err := buildProxiedServerClientInfo("", "configs/server.yaml", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not absolute")
	})

	t.Run("relative log path is rejected", func(t *testing.T) {
		_, err := buildProxiedServerClientInfo("", "", "logs/server.log")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not absolute")
	})

	t.Run("absolute paths survive a round-trip through the sidecar resolver", func(t *testing.T) {
		const beadsDir = "/proj/.beads"
		info, err := buildProxiedServerClientInfo("/var/lib/beads/proxieddb", "", "")
		require.NoError(t, err)
		require.NotNil(t, info)
		assert.Equal(t, info.RootPath, (&configfile.ProxiedServerClientInfo{RootPath: info.RootPath}).ResolvedRootPath(beadsDir))
	})
}
