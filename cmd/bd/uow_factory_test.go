package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProxiedServerUOWProvider_RoutesExternalConfigToExternalProvider(t *testing.T) {
	beadsDir := t.TempDir()
	require.NoError(t, configfile.SaveProxiedServerClientInfo(beadsDir, &configfile.ProxiedServerClientInfo{
		External: &configfile.ExternalDoltConfig{
			Host: "db.invalid",
		},
	}))

	_, err := newProxiedServerUOWProvider(context.Background(), beadsDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Host requires Port",
		"expected external validation error proving the external code path was taken; got: %v", err)
}

func TestNewExternalProxiedServerUOWProvider_CreatesRootDir(t *testing.T) {
	beadsDir := t.TempDir()
	external := &configfile.ExternalDoltConfig{Host: "db.invalid"}

	_, err := newExternalProxiedServerUOWProvider(context.Background(), beadsDir, "beads_test", external)
	require.Error(t, err, "invalid external config must surface a validation error")

	wantRoot := proxiedServerRoot(beadsDir)
	assert.DirExists(t, wantRoot, "external provider should create the proxied server root dir before validating")
	wantLog := filepath.Join(wantRoot, proxiedServerLogName)
	_ = wantLog
}
