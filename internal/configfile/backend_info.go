package configfile

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
)

// BackendInfo holds the resolved backend identity for a beads workspace.
// Populate via ResolveBackendInfo; do not construct directly.
type BackendInfo struct {
	Backend          string   `json:"backend"` // "dolt" | "postgres" | "unconfigured" | "unknown"
	Mode             string   `json:"mode"`    // "embedded" | "server" | "postgres" | ""
	Host             string   `json:"host,omitempty"`
	Port             int      `json:"port,omitempty"`
	Database         string   `json:"database"`
	User             string   `json:"user,omitempty"`
	SSLMode          string   `json:"sslmode,omitempty"`  // Postgres only
	DataDir          string   `json:"data_dir,omitempty"` // Dolt embedded only
	ProjectID        string   `json:"project_id,omitempty"`
	Source           string   `json:"source"`                       // always "metadata.json"
	LegacyDoltDir    string   `json:"legacy_dolt_dir,omitempty"`    // stale .beads/dolt when backend=postgres
	LegacyDoltFields []string `json:"legacy_dolt_fields,omitempty"` // dolt_* fields set while backend=postgres
	ParseError       string   `json:"parse_error,omitempty"`
}

// ResolveBackendInfo reads metadata.json and environment variables to build
// a resolved backend view. It does not open the database or ping anything.
func ResolveBackendInfo(beadsDir string) BackendInfo {
	cfg, err := Load(beadsDir)
	if err != nil {
		return BackendInfo{
			Backend:    "unknown",
			Source:     "metadata.json",
			ParseError: fmt.Sprintf("metadata.json malformed: %v", err),
		}
	}
	if cfg == nil {
		return BackendInfo{
			Backend: "unconfigured",
			Source:  "metadata.json",
		}
	}

	info := BackendInfo{
		Source:    "metadata.json",
		ProjectID: cfg.ProjectID,
	}

	switch cfg.GetBackend() {
	case BackendPostgres:
		info.Backend = BackendPostgres
		info.Mode = "postgres"

		// Parse DSN fields then apply BEADS_POSTGRES_* env overrides,
		// mirroring dsn.ApplyEnvOverrides without importing the dsn package.
		host, port, db, user, sslmode := parsePgDSN(cfg.PostgresDSN)
		if v := os.Getenv("BEADS_POSTGRES_HOST"); v != "" {
			host = v
		}
		if v := os.Getenv("BEADS_POSTGRES_PORT"); v != "" {
			if p, e := strconv.Atoi(v); e == nil && p > 0 && p <= 65535 {
				port = p
			}
		}
		if v := os.Getenv("BEADS_POSTGRES_USER"); v != "" {
			user = v
		}
		if v := os.Getenv("BEADS_POSTGRES_DATABASE"); v != "" {
			db = v
		}
		if v := os.Getenv("BEADS_POSTGRES_SSLMODE"); v != "" {
			sslmode = v
		}

		info.Host = host
		info.Port = port
		info.Database = db
		info.User = user
		info.SSLMode = sslmode

		legacyDir, legacyFields := DetectLegacyDoltState(beadsDir, cfg)
		info.LegacyDoltDir = legacyDir
		info.LegacyDoltFields = legacyFields

	default: // BackendDolt (covers unconfigured / empty backend field)
		info.Backend = BackendDolt
		if cfg.IsDoltServerMode() || cfg.IsDoltProxiedServerMode() {
			info.Mode = "server"
			info.Host = cfg.GetDoltServerHost()
			info.Port = cfg.GetDoltServerPort()
			info.Database = cfg.GetDoltDatabase()
			info.User = cfg.GetDoltServerUser()
		} else {
			info.Mode = "embedded"
			info.Database = cfg.GetDoltDatabase()
			if dd := cfg.GetDoltDataDir(); dd != "" {
				info.DataDir = dd
			}
		}
	}

	return info
}

// parsePgDSN extracts connection fields from a postgres:// DSN using net/url
// without importing pgconn. The DSN must be a stripped (no-password) postgres:// URL
// as produced by BuildFromFields or ConfigToConnString. Returns empty values on failure.
func parsePgDSN(rawDSN string) (host string, port int, db, user, sslmode string) {
	u, err := url.Parse(rawDSN)
	if err != nil || u.Scheme != "postgres" {
		return
	}
	host = u.Hostname()
	if portStr := u.Port(); portStr != "" {
		port, _ = strconv.Atoi(portStr)
	}
	if len(u.Path) > 1 {
		db = u.Path[1:] // strip leading "/"
	}
	if u.User != nil {
		user = u.User.Username()
	}
	sslmode = u.Query().Get("sslmode")
	if sslmode == "" {
		sslmode = "disable"
	}
	return
}
