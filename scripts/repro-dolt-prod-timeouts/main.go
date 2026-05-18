// repro-dolt-prod-timeouts runs production-shaped bd CLI timeout scenarios.
//
// It initializes a real server-mode beads workspace, bulk-loads a graph that
// mirrors maintainer-city's skew (large mostly-closed issue table, large
// dependency table, small active frontier), then forks actual bd commands.
//
// Usage:
//
//	go run ./scripts/repro-dolt-prod-timeouts --bd ./bd --scenario all
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/steveyegge/beads/internal/storage/doltutil"
)

type config struct {
	BDPath        string
	Workspace     string
	SeedMode      string
	Scenario      string
	IssueCount    int
	DepCount      int
	Concurrency   int
	Ops           int
	Timeout       time.Duration
	ChainDepth    int
	KeepWorkdir   bool
	ManagedServer bool
}

type workspace struct {
	Dir      string
	BeadsDir string
	Port     int
	Database string
}

type opResult struct {
	Kind       string        `json:"kind"`
	Argv       []string      `json:"argv"`
	Latency    time.Duration `json:"latency"`
	TimedOut   bool          `json:"timed_out"`
	Err        string        `json:"err,omitempty"`
	StderrTail string        `json:"stderr_tail,omitempty"`
}

type job struct {
	Kind string
	Argv []string
	Env  []string
	Sh   string
}

func main() {
	cfg := parseFlags()
	ctx := context.Background()

	ws, err := openOrCreateWorkspace(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if cfg.Workspace == "" {
			stopWorkspace(context.Background(), cfg, ws)
		}
		if cfg.KeepWorkdir {
			fmt.Printf("kept workdir: %s\n", ws.Dir)
			return
		}
		if cfg.Workspace == "" {
			_ = os.RemoveAll(ws.Dir)
		}
	}()

	fmt.Printf("workspace=%s port=%d database=%s\n", ws.Dir, ws.Port, ws.Database)
	if err := loadProductionShape(ctx, ws, cfg); err != nil {
		log.Fatal(err)
	}

	switch cfg.Scenario {
	case "ready":
		report("ready", runReadyScenario(ctx, cfg, ws))
	case "dep":
		report("dep", runDepScenario(ctx, cfg, ws))
	case "control":
		report("control", runControlQueryScenario(ctx, cfg, ws))
	case "mixed":
		report("mixed", runMixedCityLoadScenario(ctx, cfg, ws))
	case "outage":
		report("outage", runOutageScenario(ctx, cfg, ws))
	case "cycle-current":
		report("cycle-current", runCycleCheckScenario(ctx, cfg, ws, cycleCheckCurrentSQL))
	case "cycle-deps-only":
		report("cycle-deps-only", runCycleCheckScenario(ctx, cfg, ws, cycleCheckDependenciesOnlySQL))
	case "cycle-wisps-only":
		report("cycle-wisps-only", runCycleCheckScenario(ctx, cfg, ws, cycleCheckWispsOnlySQL))
	case "cycle-bfs":
		report("cycle-bfs", runCycleCheckScenario(ctx, cfg, ws, cycleCheckBatchedBFS))
	case "all":
		report("ready", runReadyScenario(ctx, cfg, ws))
		report("dep", runDepScenario(ctx, cfg, ws))
	default:
		log.Fatalf("unknown scenario %q", cfg.Scenario)
	}
}

func parseFlags() config {
	var cfg config
	flag.StringVar(&cfg.BDPath, "bd", "bd", "bd binary to execute")
	flag.StringVar(&cfg.Workspace, "workspace", "", "existing workspace to test instead of creating a synthetic one")
	flag.StringVar(&cfg.SeedMode, "seed-mode", "full", "fixture seed mode: full, dep-only, none")
	flag.StringVar(&cfg.Scenario, "scenario", "all", "scenario: ready, dep, control, mixed, outage, cycle-current, cycle-deps-only, cycle-wisps-only, cycle-bfs, all")
	flag.IntVar(&cfg.IssueCount, "issues", 100000, "issue rows to seed")
	flag.IntVar(&cfg.DepCount, "deps", 85000, "dependency rows to seed")
	flag.IntVar(&cfg.Concurrency, "concurrency", 20, "concurrent bd processes")
	flag.IntVar(&cfg.Ops, "ops", 80, "total operations per scenario")
	flag.DurationVar(&cfg.Timeout, "timeout", 30*time.Second, "per-command timeout")
	flag.IntVar(&cfg.ChainDepth, "chain-depth", 0, "existing blocking chain depth behind each dep-add target")
	flag.BoolVar(&cfg.KeepWorkdir, "keep-workdir", false, "keep temp workspace")
	flag.Parse()
	return cfg
}

func openOrCreateWorkspace(ctx context.Context, cfg config) (*workspace, error) {
	if cfg.Workspace != "" {
		return openWorkspace(ctx, cfg, cfg.Workspace)
	}
	return createWorkspace(ctx, cfg)
}

func openWorkspace(ctx context.Context, cfg config, dir string) (*workspace, error) {
	bdPath, err := exec.LookPath(cfg.BDPath)
	if err != nil {
		return nil, fmt.Errorf("find bd binary %q: %w", cfg.BDPath, err)
	}
	cfg.BDPath = bdPath

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	beadsDir := filepath.Join(absDir, ".beads")
	if _, err := os.Stat(beadsDir); err != nil {
		return nil, fmt.Errorf("open .beads in %s: %w", absDir, err)
	}
	port, err := readInt(filepath.Join(beadsDir, "dolt-server.port"))
	if err != nil {
		return nil, fmt.Errorf("read dolt-server.port: %w", err)
	}
	if !isPortOpen(port) {
		if err := startWorkspaceDolt(ctx, cfg, absDir); err != nil {
			return nil, err
		}
		port, err = readInt(filepath.Join(beadsDir, "dolt-server.port"))
		if err != nil {
			return nil, fmt.Errorf("read dolt-server.port: %w", err)
		}
	}
	database, err := readDoltDatabase(filepath.Join(beadsDir, "metadata.json"))
	if err != nil {
		return nil, err
	}
	return &workspace{Dir: absDir, BeadsDir: beadsDir, Port: port, Database: database}, nil
}

func isPortOpen(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func createWorkspace(ctx context.Context, cfg config) (*workspace, error) {
	bdPath, err := exec.LookPath(cfg.BDPath)
	if err != nil {
		return nil, fmt.Errorf("find bd binary %q: %w", cfg.BDPath, err)
	}
	cfg.BDPath = bdPath

	dir, err := os.MkdirTemp("", "bd-prod-timeout-*")
	if err != nil {
		return nil, err
	}

	initTimeout := cfg.Timeout * 4
	if initTimeout < 2*time.Minute {
		initTimeout = 2 * time.Minute
	}
	initCtx, cancel := context.WithTimeout(ctx, initTimeout)
	defer cancel()

	fmt.Printf("initializing server workspace timeout=%s\n", initTimeout)
	cmd := exec.CommandContext(initCtx, cfg.BDPath,
		"init",
		"--server",
		"--prefix=perf",
		"--non-interactive",
		"--quiet",
		"--skip-hooks",
		"--skip-agents",
	)
	cmd.Dir = dir
	cmd.Env = cleanEnv(os.Environ(), "BEADS_DIR", "BEADS_DOLT_SERVER_PORT", "BEADS_DOLT_PORT")
	cmd.Env = append(cmd.Env, "BD_NON_INTERACTIVE=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("bd init after %s: %w\n%s", initTimeout, err, string(out))
	}

	beadsDir := filepath.Join(dir, ".beads")
	port, err := readInt(filepath.Join(beadsDir, "dolt-server.port"))
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("read dolt-server.port: %w", err)
	}
	database, err := readDoltDatabase(filepath.Join(beadsDir, "metadata.json"))
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return &workspace{Dir: dir, BeadsDir: beadsDir, Port: port, Database: database}, nil
}

func startWorkspaceDolt(ctx context.Context, cfg config, dir string) error {
	startTimeout := cfg.Timeout * 2
	if startTimeout < time.Minute {
		startTimeout = time.Minute
	}
	startCtx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	cmd := exec.CommandContext(startCtx, cfg.BDPath, "dolt", "start")
	cmd.Dir = dir
	cmd.Env = cleanEnv(os.Environ(), "BEADS_DIR", "BEADS_DOLT_SERVER_PORT", "BEADS_DOLT_PORT")
	cmd.Env = append(cmd.Env, "BD_NON_INTERACTIVE=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bd dolt start after %s: %w\n%s", startTimeout, err, string(out))
	}
	return nil
}

func readDoltDatabase(path string) (string, error) {
	//nolint:gosec // G304: benchmark harness reads metadata from the selected workspace.
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read metadata.json: %w", err)
	}
	var meta struct {
		DoltDatabase string `json:"dolt_database"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", fmt.Errorf("parse metadata.json: %w", err)
	}
	if meta.DoltDatabase == "" {
		return "", fmt.Errorf("metadata.json missing dolt_database")
	}
	return meta.DoltDatabase, nil
}

func stopWorkspace(ctx context.Context, cfg config, ws *workspace) {
	cmd := exec.CommandContext(ctx, cfg.BDPath, "dolt", "stop")
	cmd.Dir = ws.Dir
	cmd.Env = append(cleanEnv(os.Environ(), "BEADS_DIR"), "BD_NON_INTERACTIVE=1")
	_ = cmd.Run()
}

func loadProductionShape(ctx context.Context, ws *workspace, cfg config) error {
	if cfg.SeedMode == "none" {
		fmt.Printf("seed skipped seed_mode=%s\n", cfg.SeedMode)
		return nil
	}

	dsn := doltutil.ServerDSN{
		Host:     "127.0.0.1",
		Port:     ws.Port,
		User:     "root",
		Database: ws.Database,
		Timeout:  cfg.Timeout,
	}.String()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	start := time.Now()
	switch cfg.SeedMode {
	case "full":
		if err := insertIssues(ctx, db, cfg.IssueCount, cfg.Ops, cfg.ChainDepth); err != nil {
			return err
		}
		if err := insertDependencies(ctx, db, cfg.DepCount); err != nil {
			return err
		}
	case "dep-only":
		if err := insertDepIssues(ctx, db, cfg.Ops, cfg.ChainDepth); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown seed mode %q", cfg.SeedMode)
	}
	if cfg.ChainDepth > 0 {
		if err := insertDepAddChains(ctx, db, cfg.Ops, cfg.ChainDepth); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, "CALL DOLT_ADD('-A')"); err != nil {
		return fmt.Errorf("DOLT_ADD fixture: %w", err)
	}
	if _, err := db.ExecContext(ctx, "CALL DOLT_COMMIT('-m', 'seed production timeout fixture')"); err != nil {
		return fmt.Errorf("DOLT_COMMIT fixture: %w", err)
	}
	fmt.Printf("seeded mode=%s issues=%d deps=%d in %s\n", cfg.SeedMode, cfg.IssueCount, cfg.DepCount, time.Since(start).Round(time.Millisecond))
	return nil
}

func insertIssues(ctx context.Context, db *sql.DB, count, depOps, chainDepth int) error {
	const batchSize = 500
	depIssueCount := depOps * 2
	if chainDepth > 0 {
		depIssueCount = depOps*(chainDepth+3) + chainDepth + 2
	}
	total := count + depIssueCount
	for start := 0; start < total; start += batchSize {
		end := start + batchSize
		if end > total {
			end = total
		}
		var q strings.Builder
		q.WriteString(`INSERT INTO issues
			(id, title, description, design, acceptance_criteria, notes,
			 status, priority, issue_type, assignee, metadata)
			VALUES `)
		args := make([]any, 0, (end-start)*11)
		for i := start; i < end; i++ {
			if i > start {
				q.WriteByte(',')
			}
			q.WriteString("(?,?,?,?,?,?,?,?,?,?,?)")
			id := fmt.Sprintf("perf-%06d", i)
			status := "closed"
			assignee := ""
			metadata := "{}"
			if i < 350 {
				status = "open"
			}
			if i < 40 {
				assignee = "gascity--control-dispatcher"
			}
			if i >= 40 && i < 80 {
				metadata = `{"gc.routed_to":"gascity/control-dispatcher"}`
			}
			if i >= count {
				status = "open"
				id = depIssueID(i - count)
			}
			args = append(args, id, fmt.Sprintf("prod timeout issue %d", i), "fixture", "", "", "", status, (i%4)+1, "task", assignee, metadata)
		}
		if _, err := db.ExecContext(ctx, q.String(), args...); err != nil {
			return fmt.Errorf("insert issues %d-%d: %w", start, end, err)
		}
	}
	return nil
}

func insertDepIssues(ctx context.Context, db *sql.DB, depOps, chainDepth int) error {
	const batchSize = 500
	total := depFixtureIssueCount(depOps, chainDepth)
	for start := 0; start < total; start += batchSize {
		end := start + batchSize
		if end > total {
			end = total
		}
		var q strings.Builder
		q.WriteString(`INSERT INTO issues
			(id, title, description, design, acceptance_criteria, notes,
			 status, priority, issue_type, assignee, metadata)
			VALUES `)
		args := make([]any, 0, (end-start)*11)
		for i := start; i < end; i++ {
			if i > start {
				q.WriteByte(',')
			}
			q.WriteString("(?,?,?,?,?,?,?,?,?,?,?)")
			args = append(args, depIssueID(i), fmt.Sprintf("prod copy dep issue %d", i), "fixture", "", "", "", "open", (i%4)+1, "task", "", "{}")
		}
		if _, err := db.ExecContext(ctx, q.String(), args...); err != nil {
			return fmt.Errorf("insert dep issues %d-%d: %w", start, end, err)
		}
	}
	return nil
}

func depFixtureIssueCount(depOps, chainDepth int) int {
	if chainDepth > 0 {
		return depOps*(chainDepth+3) + chainDepth + 2
	}
	return depOps * 2
}

func insertDependencies(ctx context.Context, db *sql.DB, count int) error {
	const batchSize = 500
	for start := 0; start < count; start += batchSize {
		end := start + batchSize
		if end > count {
			end = count
		}
		var q strings.Builder
		q.WriteString(`INSERT INTO dependencies
			(issue_id, depends_on_id, type, created_by, metadata)
			VALUES `)
		args := make([]any, 0, (end-start)*5)
		for i := start; i < end; i++ {
			if i > start {
				q.WriteByte(',')
			}
			q.WriteString("(?,?,?,?,?)")
			bulkIssueIndex := (i + 1000) % 100000
			issueID := fmt.Sprintf("perf-%06d", bulkIssueIndex)
			dependsOnID := fmt.Sprintf("perf-%06d", (bulkIssueIndex+300)%100000)
			depType := "parent-child"
			if i < 20 || (i >= 40 && i < 60) {
				issueID = fmt.Sprintf("perf-%06d", i)
				dependsOnID = fmt.Sprintf("perf-%06d", 200+(i%80))
				depType = "blocks"
			} else if i < 5000 {
				depType = "blocks"
			}
			args = append(args, issueID, dependsOnID, depType, "bench", "{}")
		}
		if _, err := db.ExecContext(ctx, q.String(), args...); err != nil {
			return fmt.Errorf("insert dependencies %d-%d: %w", start, end, err)
		}
	}
	return nil
}

func insertDepAddChains(ctx context.Context, db *sql.DB, ops, depth int) error {
	const batchSize = 500
	if depth <= 0 || ops <= 0 {
		return nil
	}

	total := ops * depth
	for start := 0; start < total; start += batchSize {
		end := start + batchSize
		if end > total {
			end = total
		}
		var q strings.Builder
		q.WriteString(`INSERT INTO dependencies
			(issue_id, depends_on_id, type, created_by, metadata)
			VALUES `)
		args := make([]any, 0, (end-start)*5)
		for i := start; i < end; i++ {
			if i > start {
				q.WriteByte(',')
			}
			q.WriteString("(?,?,?,?,?)")
			op := i / depth
			step := i % depth
			base := depBase(op, depth)
			issueID := depIssueID(base + 1 + step)
			dependsOnID := depIssueID(base + 2 + step)
			args = append(args, issueID, dependsOnID, "blocks", "bench", "{}")
		}
		if _, err := db.ExecContext(ctx, q.String(), args...); err != nil {
			return fmt.Errorf("insert dep-add chains %d-%d: %w", start, end, err)
		}
	}
	return nil
}

func runReadyScenario(ctx context.Context, cfg config, ws *workspace) []opResult {
	jobs := make([]job, 0, cfg.Ops)
	for i := 0; i < cfg.Ops; i++ {
		if i%2 == 0 {
			jobs = append(jobs, job{Kind: "ready", Argv: []string{"ready", "--assignee=gascity--control-dispatcher", "--json", "--limit=20"}})
		} else {
			jobs = append(jobs, job{Kind: "ready", Argv: []string{"ready", "--metadata-field", "gc.routed_to=gascity/control-dispatcher", "--unassigned", "--json", "--limit=20"}})
		}
	}
	return runJobs(ctx, cfg, ws, jobs)
}

func runDepScenario(ctx context.Context, cfg config, ws *workspace) []opResult {
	jobs := make([]job, 0, cfg.Ops)
	for i := 0; i < cfg.Ops; i++ {
		jobs = append(jobs, depAddJob(i, cfg.ChainDepth))
	}
	return runJobs(ctx, cfg, ws, jobs)
}

func runControlQueryScenario(ctx context.Context, cfg config, ws *workspace) []opResult {
	return runJobs(ctx, cfg, ws, controlQueryJobs(cfg.Ops))
}

func runOutageScenario(ctx context.Context, cfg config, ws *workspace) []opResult {
	jobs := make([]job, 0, cfg.Ops*2)
	jobs = append(jobs, controlQueryJobs(cfg.Ops)...)
	for i := 0; i < cfg.Ops; i++ {
		jobs = append(jobs, depAddJob(i, cfg.ChainDepth))
	}
	return runJobs(ctx, cfg, ws, jobs)
}

type cycleCheckFunc func(context.Context, *sql.DB, string, string, int) error

func runCycleCheckScenario(ctx context.Context, cfg config, ws *workspace, check cycleCheckFunc) []opResult {
	dsn := doltutil.ServerDSN{
		Host:     "127.0.0.1",
		Port:     ws.Port,
		User:     "root",
		Database: ws.Database,
		Timeout:  cfg.Timeout,
	}.String()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return []opResult{{Kind: "cycle-open", Err: err.Error()}}
	}
	defer db.Close()
	db.SetMaxOpenConns(cfg.Concurrency)
	db.SetMaxIdleConns(cfg.Concurrency)

	start := time.Now()
	jobCh := make(chan int)
	resCh := make(chan opResult, cfg.Ops)
	var wg sync.WaitGroup
	for w := 0; w < cfg.Concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobCh {
				opCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
				base := depBase(i, cfg.ChainDepth)
				issueID := depIssueID(base)
				dependsOnID := depIssueID(base + 1)
				opStart := time.Now()
				err := check(opCtx, db, issueID, dependsOnID, cfg.ChainDepth)
				latency := time.Since(opStart)
				res := opResult{Kind: "cycle", Argv: []string{"cycle-check", issueID, dependsOnID}, Latency: latency}
				if opCtx.Err() == context.DeadlineExceeded {
					res.TimedOut = true
				}
				if err != nil {
					res.Err = err.Error()
					res.StderrTail = tail(err.Error(), 300)
				}
				cancel()
				resCh <- res
			}
		}()
	}
	for i := 0; i < cfg.Ops; i++ {
		jobCh <- i
	}
	close(jobCh)
	wg.Wait()
	close(resCh)

	results := make([]opResult, 0, cfg.Ops)
	for res := range resCh {
		results = append(results, res)
	}
	fmt.Printf("scenario wall=%s\n", time.Since(start).Round(time.Millisecond))
	return results
}

func cycleCheckCurrentSQL(ctx context.Context, db *sql.DB, issueID, dependsOnID string, _ int) error {
	var reachable int
	err := db.QueryRowContext(ctx, `
		WITH RECURSIVE reachable AS (
			SELECT ? AS node, 0 AS depth
			UNION ALL
			SELECT d.depends_on_id, r.depth + 1
			FROM reachable r
			JOIN (
				SELECT issue_id, depends_on_id FROM dependencies WHERE type IN ('blocks', 'conditional-blocks')
				UNION ALL
				SELECT issue_id, depends_on_id FROM wisp_dependencies WHERE type IN ('blocks', 'conditional-blocks')
			) d ON d.issue_id = r.node
			WHERE r.depth < 100
		)
		SELECT COUNT(*) FROM reachable WHERE node = ?
	`, dependsOnID, issueID).Scan(&reachable)
	if err != nil {
		return err
	}
	if reachable > 0 {
		return fmt.Errorf("cycle detected")
	}
	return nil
}

func cycleCheckDependenciesOnlySQL(ctx context.Context, db *sql.DB, issueID, dependsOnID string, _ int) error {
	return cycleCheckOneTableSQL(ctx, db, "dependencies", issueID, dependsOnID)
}

func cycleCheckWispsOnlySQL(ctx context.Context, db *sql.DB, issueID, dependsOnID string, _ int) error {
	return cycleCheckOneTableSQL(ctx, db, "wisp_dependencies", issueID, dependsOnID)
}

func cycleCheckOneTableSQL(ctx context.Context, db *sql.DB, table, issueID, dependsOnID string) error {
	var reachable int
	//nolint:gosec // G201: table is selected by fixed scenario wrappers.
	query := fmt.Sprintf(`
		WITH RECURSIVE reachable AS (
			SELECT ? AS node, 0 AS depth
			UNION ALL
			SELECT d.depends_on_id, r.depth + 1
			FROM reachable r
			JOIN %s d ON d.issue_id = r.node
			WHERE d.type IN ('blocks', 'conditional-blocks') AND r.depth < 100
		)
		SELECT COUNT(*) FROM reachable WHERE node = ?
	`, table)
	if err := db.QueryRowContext(ctx, query, dependsOnID, issueID).Scan(&reachable); err != nil {
		return err
	}
	if reachable > 0 {
		return fmt.Errorf("cycle detected")
	}
	return nil
}

func cycleCheckBatchedBFS(ctx context.Context, db *sql.DB, issueID, dependsOnID string, maxDepth int) error {
	if maxDepth <= 0 || maxDepth > 100 {
		maxDepth = 100
	}
	seen := map[string]struct{}{dependsOnID: {}}
	frontier := []string{dependsOnID}
	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		next, err := fetchBlockingTargets(ctx, db, frontier)
		if err != nil {
			return err
		}
		frontier = frontier[:0]
		for _, id := range next {
			if id == issueID {
				return fmt.Errorf("cycle detected")
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			frontier = append(frontier, id)
		}
	}
	return nil
}

func fetchBlockingTargets(ctx context.Context, db *sql.DB, issueIDs []string) ([]string, error) {
	if len(issueIDs) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(issueIDs)*2)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(issueIDs)), ",")
	//nolint:gosec // G201: placeholders are generated from ? markers only.
	query := fmt.Sprintf(`
		SELECT depends_on_id FROM dependencies
		WHERE issue_id IN (%s) AND type IN ('blocks', 'conditional-blocks')
		UNION ALL
		SELECT depends_on_id FROM wisp_dependencies
		WHERE issue_id IN (%s) AND type IN ('blocks', 'conditional-blocks')
	`, placeholders, placeholders)
	for _, id := range issueIDs {
		args = append(args, id)
	}
	for _, id := range issueIDs {
		args = append(args, id)
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func runMixedCityLoadScenario(ctx context.Context, cfg config, ws *workspace) []opResult {
	start := time.Now()
	depCount := cfg.Ops
	backgroundCount := cfg.Ops * cfg.Concurrency

	results := make([]opResult, 0, depCount+backgroundCount)
	resultCh := make(chan opResult, depCount+backgroundCount)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < depCount; i++ {
			job := depAddJob(i, cfg.ChainDepth)
			job.Kind = "dispatcher-dep"
			resultCh <- runJob(ctx, cfg, ws, job)
		}
	}()

	backgroundJobs := mixedBackgroundJobs(backgroundCount)
	jobCh := make(chan job)
	for w := 0; w < cfg.Concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				resultCh <- runJob(ctx, cfg, ws, job)
			}
		}()
	}
	for _, job := range backgroundJobs {
		jobCh <- job
	}
	close(jobCh)

	wg.Wait()
	close(resultCh)
	for res := range resultCh {
		results = append(results, res)
	}
	fmt.Printf("scenario wall=%s\n", time.Since(start).Round(time.Millisecond))
	return results
}

func mixedBackgroundJobs(count int) []job {
	jobs := make([]job, 0, count)
	for i := 0; i < count; i++ {
		switch i % 6 {
		case 0:
			jobs = append(jobs, job{Kind: "session-ready", Argv: []string{"ready", "--include-ephemeral", "--assignee=" + sessionAssignee(i), "--json", "--limit=1"}})
		case 1:
			jobs = append(jobs, job{Kind: "control-ready", Argv: []string{"--readonly", "--sandbox", "ready", "--include-ephemeral", "--assignee=gascity--control-dispatcher", "--json", "--limit=20"}})
		case 2:
			jobs = append(jobs, job{Kind: "route-ready", Argv: []string{"--readonly", "--sandbox", "ready", "--include-ephemeral", "--metadata-field", "gc.routed_to=gascity/control-dispatcher", "--unassigned", "--json", "--limit=20"}})
		case 3:
			jobs = append(jobs, job{Kind: "show", Argv: []string{"show", fmt.Sprintf("perf-%06d", i%350), "--json"}})
		case 4:
			jobs = append(jobs, job{Kind: "list", Argv: []string{"list", "--json", "--status", "in_progress", "--assignee=" + sessionAssignee(i), "--limit=1"}})
		case 5:
			jobs = append(jobs, job{Kind: "claim", Argv: []string{"update", fmt.Sprintf("perf-%06d", i%40), "--claim", "--json"}})
		}
	}
	return jobs
}

func sessionAssignee(i int) string {
	return fmt.Sprintf("mc-%07d", i%64)
}

func depAddJob(i, chainDepth int) job {
	base := depBase(i, chainDepth)
	return job{
		Kind: "dep",
		Argv: []string{"dep", "add", depIssueID(base), depIssueID(base + 1), "--type", "blocks", "--json"},
	}
}

func depBase(i, chainDepth int) int {
	if chainDepth <= 0 {
		return i * 2
	}
	return i * (chainDepth + 3)
}

func controlQueryJobs(count int) []job {
	targets := []struct {
		target  string
		session string
		legacy  string
	}{
		{target: "control-dispatcher", session: "control-dispatcher", legacy: "workflow-control"},
		{target: "gascity/control-dispatcher", session: "gascity--control-dispatcher", legacy: "gascity/workflow-control"},
		{target: "gasworks-gui/control-dispatcher", session: "gasworks-gui--control-dispatcher", legacy: "gasworks-gui/workflow-control"},
		{target: "gtest-rig/control-dispatcher", session: "gtest-rig--control-dispatcher", legacy: "gtest-rig/workflow-control"},
	}

	jobs := make([]job, 0, count)
	script := controlQueryScript()
	for i := 0; i < count; i++ {
		t := targets[i%len(targets)]
		jobs = append(jobs, job{
			Kind: "control",
			Sh:   script,
			Env: []string{
				"BD_EXPORT_AUTO=false",
				"GC_CONTROL_TARGET=" + t.target,
				"GC_CONTROL_SESSION_NAME=" + t.session,
				"GC_CONTROL_LEGACY_TARGET=" + t.legacy,
				"GC_SESSION_NAME=" + t.session,
				"GC_ALIAS=" + t.target,
				"GC_SESSION_ID=" + t.session,
			},
		})
	}
	return jobs
}

func controlQueryScript() string {
	return `BD_EXPORT_AUTO=false
tmp=$(mktemp)
trap "rm -f \"$tmp\"" EXIT
emit_ready() {
  r=$("$@" 2>/dev/null || true)
  [ -n "$r" ] && [ "$r" != "[]" ] && printf "%s\n" "$r" >> "$tmp"
}
for id in "$GC_CONTROL_SESSION_NAME" "$GC_SESSION_NAME" "$GC_ALIAS" "$GC_CONTROL_TARGET" "$GC_SESSION_ID"; do
  [ -z "$id" ] && continue
  legacy=""
  case "$id" in *control-dispatcher) legacy="${id%control-dispatcher}workflow-control";; esac
  for cand in "$id" "$legacy"; do
    [ -z "$cand" ] && continue
    emit_ready "$BD_BIN" --readonly --sandbox ready --assignee="$cand" --json --limit=20
  done
done
emit_ready "$BD_BIN" --readonly --sandbox ready --metadata-field "gc.routed_to=$GC_CONTROL_TARGET" --unassigned --json --limit=20
emit_ready "$BD_BIN" --readonly --sandbox ready --metadata-field "gc.routed_to=$GC_CONTROL_LEGACY_TARGET" --unassigned --json --limit=20
[ -s "$tmp" ] && jq -s 'reduce add[] as $item ([]; if any(.[]; .id == $item.id) then . else . + [$item] end)' "$tmp" || printf "[]"
`
}

func depIssueID(i int) string {
	return fmt.Sprintf("perf-dep-%06d", i)
}

func runJobs(ctx context.Context, cfg config, ws *workspace, jobs []job) []opResult {
	start := time.Now()
	jobCh := make(chan job)
	resCh := make(chan opResult, len(jobs))
	var wg sync.WaitGroup
	for w := 0; w < cfg.Concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				resCh <- runJob(ctx, cfg, ws, job)
			}
		}()
	}
	for _, job := range jobs {
		jobCh <- job
	}
	close(jobCh)
	wg.Wait()
	close(resCh)

	results := make([]opResult, 0, len(jobs))
	for res := range resCh {
		results = append(results, res)
	}
	fmt.Printf("scenario wall=%s\n", time.Since(start).Round(time.Millisecond))
	return results
}

func runJob(ctx context.Context, cfg config, ws *workspace, j job) opResult {
	if j.Sh != "" {
		return runShell(ctx, cfg, ws, j)
	}
	return runBD(ctx, cfg, ws, j)
}

func runBD(ctx context.Context, cfg config, ws *workspace, j job) opResult {
	opCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	cmd := exec.CommandContext(opCtx, cfg.BDPath, j.Argv...)
	cmd.Dir = ws.Dir
	cmd.Env = append(cleanEnv(os.Environ(), "BEADS_DIR"), "BD_NON_INTERACTIVE=1")
	cmd.Env = append(cmd.Env, j.Env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	latency := time.Since(start)
	res := opResult{Kind: j.Kind, Argv: j.Argv, Latency: latency}
	if len(res.Argv) == 0 {
		res.Argv = []string{"sh", "-c", compactShell(j.Sh)}
	}
	if opCtx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
	}
	if err != nil {
		res.Err = err.Error()
	}
	res.StderrTail = tail(stderr.String(), 300)
	return res
}

func runShell(ctx context.Context, cfg config, ws *workspace, j job) opResult {
	opCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	cmd := exec.CommandContext(opCtx, "sh", "-c", j.Sh)
	cmd.Dir = ws.Dir
	cmd.Env = append(cleanEnv(os.Environ(), "BEADS_DIR"), "BD_NON_INTERACTIVE=1", "BD_BIN="+cfg.BDPath)
	cmd.Env = append(cmd.Env, j.Env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	latency := time.Since(start)
	res := opResult{Kind: j.Kind, Argv: []string{"sh", "-c", compactShell(j.Sh)}, Latency: latency}
	if opCtx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
	}
	if err != nil {
		res.Err = err.Error()
	}
	res.StderrTail = tail(stderr.String(), 300)
	return res
}

func compactShell(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func report(name string, results []opResult) {
	sort.Slice(results, func(i, j int) bool { return results[i].Latency < results[j].Latency })
	var failures, timeouts int
	var driverReadTimeouts int
	for _, r := range results {
		if r.Err != "" {
			failures++
		}
		if r.TimedOut {
			timeouts++
		}
		if isDriverReadTimeout(r) {
			driverReadTimeouts++
		}
	}
	fmt.Printf("\n[%s] ops=%d failures=%d harness_timeouts=%d driver_read_timeouts=%d p50=%s p95=%s max=%s\n",
		name, len(results), failures, timeouts, driverReadTimeouts,
		percentile(results, 50).Round(time.Millisecond),
		percentile(results, 95).Round(time.Millisecond),
		percentile(results, 100).Round(time.Millisecond),
	)
	for i := len(results) - 1; i >= 0 && i >= len(results)-5; i-- {
		r := results[i]
		fmt.Printf("  slow kind=%s latency=%s timeout=%t err=%q stderr=%q argv=%s\n",
			r.Kind, r.Latency.Round(time.Millisecond), r.TimedOut, r.Err, r.StderrTail, strings.Join(r.Argv, " "))
	}
}

func isDriverReadTimeout(r opResult) bool {
	return strings.Contains(r.StderrTail, "packets.go:58 read tcp") &&
		strings.Contains(r.StderrTail, "i/o timeout")
}

func percentile(results []opResult, p int) time.Duration {
	if len(results) == 0 {
		return 0
	}
	if p >= 100 {
		return results[len(results)-1].Latency
	}
	idx := (len(results)*p + 99) / 100
	if idx <= 0 {
		idx = 1
	}
	return results[idx-1].Latency
}

func readInt(path string) (int, error) {
	//nolint:gosec // G304: benchmark harness reads control files from the selected workspace.
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func cleanEnv(env []string, keys ...string) []string {
	drop := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		drop[key] = struct{}{}
	}
	out := env[:0]
	for _, e := range env {
		key, _, ok := strings.Cut(e, "=")
		if ok {
			if _, skip := drop[key]; skip {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

func tail(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[len(s)-max:]
}
