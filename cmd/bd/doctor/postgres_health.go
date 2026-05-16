package doctor

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/steveyegge/beads/internal/configfile"
	pgdsn "github.com/steveyegge/beads/internal/storage/postgres/dsn"
)

// minPGMajorVersion is the minimum supported Postgres major version.
const minPGMajorVersion = 14

// PGProbeResult carries the structured output from a Postgres health probe.
// Used both for the bd doctor check line and the bd doctor --json backend block.
type PGProbeResult struct {
	Check          DoctorCheck
	Version        string
	Healthy        bool
	ProjectIDMatch bool
}

// CheckPostgresHealth probes the Postgres backend: TCP connectivity, version
// compatibility (≥14), and project_id drift against metadata.json.
// Used by bd doctor on Postgres workspaces in place of RunDoltHealthChecks.
func CheckPostgresHealth(beadsDir string) DoctorCheck {
	return ProbePostgresHealth(beadsDir).Check
}

// ProbePostgresHealth probes Postgres and returns both the DoctorCheck and
// runtime metadata (version, healthy, project_id_match) for JSON output.
func ProbePostgresHealth(beadsDir string) PGProbeResult {
	bi := configfile.ResolveBackendInfo(beadsDir)

	if bi.Host == "" {
		return PGProbeResult{Check: DoctorCheck{
			Name:     "Backend Health",
			Status:   StatusError,
			Message:  "postgres backend configured but no host in metadata.json",
			Category: CategoryCore,
		}}
	}

	target := fmt.Sprintf("%s:%d/%s", bi.Host, bi.Port, bi.Database)
	strippedDSN := pgdsn.BuildFromFields(bi.Host, bi.Port, bi.User, bi.Database, bi.SSLMode)
	fullDSN := pgdsn.Compose(strippedDSN, os.Getenv("BEADS_POSTGRES_PASSWORD"))

	cfg, err := pgconn.ParseConfig(fullDSN)
	if err != nil {
		return PGProbeResult{Check: DoctorCheck{
			Name:     "Backend Health",
			Status:   StatusError,
			Message:  fmt.Sprintf("backend postgres  %s  invalid DSN: %v", target, err),
			Category: CategoryCore,
		}}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := pgconn.ConnectConfig(ctx, cfg)
	if err != nil {
		return PGProbeResult{Check: DoctorCheck{
			Name:     "Backend Health",
			Status:   StatusError,
			Message:  fmt.Sprintf("backend postgres  %s  %v", target, err),
			Category: CategoryCore,
		}}
	}
	defer conn.Close(ctx) //nolint:errcheck

	// Version check.
	versionStr := conn.ParameterStatus("server_version")
	versionInt, displayVersion := parsePGVersion(versionStr)
	if versionInt > 0 && versionInt < minPGMajorVersion*10000 {
		return PGProbeResult{Check: DoctorCheck{
			Name:     "Backend Health",
			Status:   StatusError,
			Message:  fmt.Sprintf("backend postgres  %s  requires Postgres ≥%d; found %s", target, minPGMajorVersion, displayVersion),
			Category: CategoryCore,
		}, Version: displayVersion}
	}

	// Project_id drift detection.
	localID := bi.ProjectID
	projectIDMatch := true
	if localID != "" {
		if dbID := queryPGProjectID(ctx, conn); dbID != "" && dbID != localID {
			return PGProbeResult{
				Check: DoctorCheck{
					Name:    "Backend Health",
					Status:  StatusError,
					Message: fmt.Sprintf("backend postgres  %s  project_id mismatch", target),
					Detail: fmt.Sprintf(
						"expected  %s\n     found     %s\n     (pointing at wrong Postgres database — cross-project data leak risk)\n     (.beads path: %s/metadata.json)",
						localID, dbID, beadsDir),
					Category: CategoryCore,
				},
				Version:        displayVersion,
				Healthy:        true,
				ProjectIDMatch: false,
			}
		}
	}

	msg := fmt.Sprintf("backend postgres  %s  %s", target, displayVersion)
	if localID != "" {
		msg += "  project_id match"
	}
	return PGProbeResult{
		Check: DoctorCheck{
			Name:     "Backend Health",
			Status:   StatusOK,
			Message:  msg,
			Category: CategoryCore,
		},
		Version:        displayVersion,
		Healthy:        true,
		ProjectIDMatch: projectIDMatch,
	}
}

// queryPGProjectID queries _project_id from the metadata table.
// Returns "" when the table doesn't exist or has no matching row.
func queryPGProjectID(ctx context.Context, conn *pgconn.PgConn) string {
	mrr := conn.Exec(ctx, "SELECT value FROM metadata WHERE key = '_project_id'")
	defer mrr.Close() //nolint:errcheck
	for mrr.NextResult() {
		rr := mrr.ResultReader()
		for rr.NextRow() {
			if vals := rr.Values(); len(vals) > 0 {
				return string(vals[0])
			}
		}
		_, _ = rr.Close()
	}
	return ""
}

// parsePGVersion parses "14.12" or "14.12 (Debian ...)" → (140012, "v14.12").
func parsePGVersion(versionStr string) (int, string) {
	if versionStr == "" {
		return 0, "unknown"
	}
	tok := strings.Fields(versionStr)[0]
	parts := strings.SplitN(tok, ".", 2)
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, versionStr
	}
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(strings.Split(parts[1], " ")[0])
	}
	return major*10000 + minor, fmt.Sprintf("v%d.%d", major, minor)
}
