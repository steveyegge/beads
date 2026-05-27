//go:build !windows

package proxy_test

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage/dbproxy/proxy"
	"github.com/steveyegge/beads/internal/storage/dbproxy/server"
	"github.com/steveyegge/beads/internal/testutil"
)

func TestProxy_ExternalDoltBackend_EndToEnd(t *testing.T) {
	port := testutil.StartIsolatedDoltContainer(t)
	portInt, err := strconv.Atoi(port)
	require.NoError(t, err)

	srv, err := server.NewExternalDoltServer(configfile.ExternalDoltConfig{
		Host: "127.0.0.1",
		Port: portInt,
	})
	require.NoError(t, err)

	stats := &proxy.Stats{}
	proxyPort := freeTCPPort(t)
	root := t.TempDir()

	h := runProxy(t, proxy.ProxyOpts{
		RootDir:     root,
		Port:        proxyPort,
		IdleTimeout: 0,
		Server:      srv,
		Stats:       stats,
	})
	waitListening(t, root, listenWait)

	dsn := fmt.Sprintf("root:@tcp(127.0.0.1:%d)/beads_test?timeout=5s&parseTime=true&multiStatements=true", proxyPort)
	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var one int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT 1").Scan(&one))
	assert.Equal(t, 1, one)

	var dbName string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&dbName))
	assert.Equal(t, "beads_test", dbName)

	_, err = db.ExecContext(ctx, "CREATE TABLE t (id INT PRIMARY KEY, v VARCHAR(64))")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, "INSERT INTO t (id, v) VALUES (1, 'alpha'), (2, 'beta')")
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM t").Scan(&count))
	assert.Equal(t, 2, count)

	var v string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT v FROM t WHERE id = 2").Scan(&v))
	assert.Equal(t, "beta", v)

	require.NoError(t, db.Close())

	h.Cancel()
	require.NoError(t, h.waitErr(t, shutdownWait))

	s := stats.Snapshot()
	assert.GreaterOrEqual(t, s.ListenAndServeCalls, int64(1))
	assert.GreaterOrEqual(t, s.BackendStartCalls, int64(1))
	assert.GreaterOrEqual(t, s.BackendStopCalls, int64(1))
	assert.GreaterOrEqual(t, s.AcceptCalls, int64(1), "client must have connected through proxy")
	assert.GreaterOrEqual(t, s.HandledConns, int64(1))
	assert.Greater(t, s.BytesClientToBackend, int64(0), "queries must have flowed client→backend")
	assert.Greater(t, s.BytesBackendToClient, int64(0), "results must have flowed backend→client")
	assertNoPidFile(t, root)
}
