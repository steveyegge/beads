package server

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExternalDoltConfigValidate(t *testing.T) {
	t.Run("tcp endpoint with host+port", func(t *testing.T) {
		require.NoError(t, configfile.ExternalDoltConfig{Host: "db.internal", Port: 3306}.Validate())
	})

	t.Run("unix socket endpoint", func(t *testing.T) {
		require.NoError(t, configfile.ExternalDoltConfig{Socket: "/var/run/dolt.sock"}.Validate())
	})

	t.Run("tcp endpoint with tls required and no cert/key", func(t *testing.T) {
		require.NoError(t, configfile.ExternalDoltConfig{
			Host:        "hosted-dolt.example.com",
			Port:        3306,
			TLSRequired: true,
		}.Validate())
	})

	t.Run("tcp endpoint with paired tls cert+key", func(t *testing.T) {
		require.NoError(t, configfile.ExternalDoltConfig{
			Host:        "hosted-dolt.example.com",
			Port:        3306,
			TLSRequired: true,
			TLSCert:     "/etc/beads/client.pem",
			TLSKey:      "/etc/beads/client.key",
		}.Validate())
	})

	t.Run("keep alive period zero is fine", func(t *testing.T) {
		require.NoError(t, configfile.ExternalDoltConfig{Host: "db", Port: 3306, KeepAlivePeriod: 0}.Validate())
	})

	t.Run("keep alive period positive is fine", func(t *testing.T) {
		require.NoError(t, configfile.ExternalDoltConfig{Host: "db", Port: 3306, KeepAlivePeriod: 60 * time.Second}.Validate())
	})

	t.Run("empty config rejected", func(t *testing.T) {
		err := configfile.ExternalDoltConfig{}.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must set Socket or (Host, Port)")
	})

	t.Run("socket and host together rejected", func(t *testing.T) {
		err := configfile.ExternalDoltConfig{Host: "db", Port: 3306, Socket: "/var/run/dolt.sock"}.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "either Socket OR (Host, Port)")
	})

	t.Run("socket and port together rejected", func(t *testing.T) {
		err := configfile.ExternalDoltConfig{Port: 3306, Socket: "/var/run/dolt.sock"}.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "either Socket OR (Host, Port)")
	})

	t.Run("host without port rejected", func(t *testing.T) {
		err := configfile.ExternalDoltConfig{Host: "db"}.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Host requires Port")
	})

	t.Run("port without host rejected", func(t *testing.T) {
		err := configfile.ExternalDoltConfig{Port: 3306}.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Port requires Host")
	})

	t.Run("port zero with host rejected (treated as missing)", func(t *testing.T) {
		err := configfile.ExternalDoltConfig{Host: "db", Port: 0}.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Host requires Port")
	})

	t.Run("port out of range rejected", func(t *testing.T) {
		err := configfile.ExternalDoltConfig{Host: "db", Port: 70000}.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "out of range")
	})

	t.Run("port negative rejected", func(t *testing.T) {
		err := configfile.ExternalDoltConfig{Host: "db", Port: -1}.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "out of range")
	})

	t.Run("relative socket path rejected", func(t *testing.T) {
		err := configfile.ExternalDoltConfig{Socket: "run/dolt.sock"}.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is not absolute")
	})

	t.Run("tls cert without key rejected", func(t *testing.T) {
		err := configfile.ExternalDoltConfig{Host: "db", Port: 3306, TLSCert: "/etc/beads/client.pem"}.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TLSCert set without TLSKey")
	})

	t.Run("tls key without cert rejected", func(t *testing.T) {
		err := configfile.ExternalDoltConfig{Host: "db", Port: 3306, TLSKey: "/etc/beads/client.key"}.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TLSKey set without TLSCert")
	})

	t.Run("relative tls cert rejected", func(t *testing.T) {
		err := configfile.ExternalDoltConfig{
			Host:    "db",
			Port:    3306,
			TLSCert: "client.pem",
			TLSKey:  "/etc/beads/client.key",
		}.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TLSCert")
		assert.Contains(t, err.Error(), "is not absolute")
	})

	t.Run("relative tls key rejected", func(t *testing.T) {
		err := configfile.ExternalDoltConfig{
			Host:    "db",
			Port:    3306,
			TLSCert: "/etc/beads/client.pem",
			TLSKey:  "client.key",
		}.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TLSKey")
		assert.Contains(t, err.Error(), "is not absolute")
	})

	t.Run("negative keep alive period rejected", func(t *testing.T) {
		err := configfile.ExternalDoltConfig{Host: "db", Port: 3306, KeepAlivePeriod: -1 * time.Second}.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "KeepAlivePeriod")
		assert.Contains(t, err.Error(), "negative")
	})
}

func TestNewExternalDoltServer_RejectsInvalidConfig(t *testing.T) {
	_, err := NewExternalDoltServer(configfile.ExternalDoltConfig{})
	require.Error(t, err)
}

func TestExternalDoltServer_ID(t *testing.T) {
	t.Run("same tcp endpoint produces same id", func(t *testing.T) {
		a, err := NewExternalDoltServer(configfile.ExternalDoltConfig{Host: "db", Port: 3306})
		require.NoError(t, err)
		b, err := NewExternalDoltServer(configfile.ExternalDoltConfig{Host: "db", Port: 3306})
		require.NoError(t, err)
		assert.Equal(t, a.ID(context.Background()), b.ID(context.Background()))
	})

	t.Run("different ports produce different ids", func(t *testing.T) {
		a, err := NewExternalDoltServer(configfile.ExternalDoltConfig{Host: "db", Port: 3306})
		require.NoError(t, err)
		b, err := NewExternalDoltServer(configfile.ExternalDoltConfig{Host: "db", Port: 3307})
		require.NoError(t, err)
		assert.NotEqual(t, a.ID(context.Background()), b.ID(context.Background()))
	})

	t.Run("different hosts produce different ids", func(t *testing.T) {
		a, err := NewExternalDoltServer(configfile.ExternalDoltConfig{Host: "db-a", Port: 3306})
		require.NoError(t, err)
		b, err := NewExternalDoltServer(configfile.ExternalDoltConfig{Host: "db-b", Port: 3306})
		require.NoError(t, err)
		assert.NotEqual(t, a.ID(context.Background()), b.ID(context.Background()))
	})

	t.Run("socket and tcp produce different ids even when notation overlaps", func(t *testing.T) {
		a, err := NewExternalDoltServer(configfile.ExternalDoltConfig{Host: "db", Port: 3306})
		require.NoError(t, err)
		b, err := NewExternalDoltServer(configfile.ExternalDoltConfig{Socket: "/var/run/dolt.sock"})
		require.NoError(t, err)
		assert.NotEqual(t, a.ID(context.Background()), b.ID(context.Background()))
	})
}

func TestExternalDoltServer_DSN(t *testing.T) {
	t.Run("tcp without tls", func(t *testing.T) {
		s, err := NewExternalDoltServer(configfile.ExternalDoltConfig{Host: "db", Port: 3306})
		require.NoError(t, err)
		dsn := s.DSN(context.Background(), "beads", "root", "secret")
		assert.Contains(t, dsn, "tcp(db:3306)")
		assert.Contains(t, dsn, "/beads")
		assert.Contains(t, dsn, "tls=false")
	})

	t.Run("tcp with tls required", func(t *testing.T) {
		s, err := NewExternalDoltServer(configfile.ExternalDoltConfig{Host: "db", Port: 3306, TLSRequired: true})
		require.NoError(t, err)
		dsn := s.DSN(context.Background(), "beads", "root", "")
		assert.Contains(t, dsn, "tls=true")
	})

	t.Run("unix socket", func(t *testing.T) {
		s, err := NewExternalDoltServer(configfile.ExternalDoltConfig{Socket: "/var/run/dolt.sock"})
		require.NoError(t, err)
		dsn := s.DSN(context.Background(), "beads", "root", "")
		assert.Contains(t, dsn, "unix(/var/run/dolt.sock)")
		assert.NotContains(t, dsn, "tcp(")
	})

	t.Run("password embedded", func(t *testing.T) {
		s, err := NewExternalDoltServer(configfile.ExternalDoltConfig{Host: "db", Port: 3306})
		require.NoError(t, err)
		dsn := s.DSN(context.Background(), "beads", "u", "p@ss")
		assert.True(t, strings.HasPrefix(dsn, "u:p@ss@") || strings.Contains(dsn, "u:p%40ss@"))
	})
}

func TestExternalDoltServer_Lifecycle(t *testing.T) {
	s, err := NewExternalDoltServer(configfile.ExternalDoltConfig{Host: "db", Port: 3306})
	require.NoError(t, err)

	ctx := context.Background()
	assert.False(t, s.Running(ctx))

	require.NoError(t, s.Start(ctx))
	assert.True(t, s.Running(ctx))

	require.Error(t, s.Start(ctx), "double start should fail")

	require.NoError(t, s.Stop(ctx))
	assert.False(t, s.Running(ctx))

	require.NoError(t, s.Start(ctx), "restart after stop should work")
	assert.True(t, s.Running(ctx))
}

func TestExternalDoltServer_Dial(t *testing.T) {
	t.Run("tcp dial against a live listener succeeds", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { _ = ln.Close() })

		host, portStr, err := net.SplitHostPort(ln.Addr().String())
		require.NoError(t, err)
		port, err := net.LookupPort("tcp", portStr)
		require.NoError(t, err)

		go func() {
			c, aerr := ln.Accept()
			if aerr == nil {
				_ = c.Close()
			}
		}()

		s, err := NewExternalDoltServer(configfile.ExternalDoltConfig{Host: host, Port: port})
		require.NoError(t, err)
		require.NoError(t, s.Start(context.Background()))
		t.Cleanup(func() { _ = s.Stop(context.Background()) })

		conn, err := s.Dial(context.Background())
		require.NoError(t, err)
		require.NotNil(t, conn)
		_ = conn.Close()
	})

	t.Run("tcp dial against a closed listener returns wrapped error", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		addr := ln.Addr().(*net.TCPAddr)
		require.NoError(t, ln.Close())

		s, err := NewExternalDoltServer(configfile.ExternalDoltConfig{Host: "127.0.0.1", Port: addr.Port})
		require.NoError(t, err)
		require.NoError(t, s.Start(context.Background()))
		t.Cleanup(func() { _ = s.Stop(context.Background()) })

		_, err = s.Dial(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ExternalDoltServer.Dial")
	})

	t.Run("dial honors a canceled context", func(t *testing.T) {
		s, err := NewExternalDoltServer(configfile.ExternalDoltConfig{Host: "10.255.255.1", Port: 9999})
		require.NoError(t, err)
		require.NoError(t, s.Start(context.Background()))
		t.Cleanup(func() { _ = s.Stop(context.Background()) })

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = s.Dial(ctx)
		require.Error(t, err)
	})
}
