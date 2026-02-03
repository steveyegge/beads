package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

type pacmanScoreboard struct {
	Scores map[string]pacmanScore `json:"scores"`
}

type pacmanScore struct {
	Dots int `json:"dots"`
}

type pacmanAgents struct {
	Agents []pacmanAgent `json:"agents"`
}

type pacmanAgent struct {
	Name   string `json:"name"`
	Joined string `json:"joined"`
	Score  int    `json:"score,omitempty"`
}

type pacmanPause struct {
	Reason string `json:"reason"`
	From   string `json:"from"`
	TS     string `json:"ts"`
}

type pacmanState struct {
	Agent        string              `json:"agent"`
	Score        int                 `json:"score"`
	Dots         []reflectIssueInfo  `json:"dots,omitempty"`
	Blockers     []pacmanBlocker     `json:"blockers,omitempty"`
	Paused       *pacmanPause        `json:"paused,omitempty"`
	Leaderboard  []pacmanLeader      `json:"leaderboard,omitempty"`
	Achievements []pacmanAchievement `json:"achievements,omitempty"`
}

type pacmanBlocker struct {
	ID        string   `json:"id"`
	BlockedBy []string `json:"blocked_by,omitempty"`
}

type pacmanLeader struct {
	Name        string `json:"name"`
	Dots        int    `json:"dots"`
	SkillsFixed int    `json:"skills_fixed,omitempty"`
}

type pacmanAchievement struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

var pacmanCmd = &cobra.Command{
	Use:   "pacman",
	Short: "Play pacman mode with beads (serverless)",
	Long: `Pacman mode shows open work as dots and blocked work as ghosts.

Use --eat to close a bead and increment your score.`,
	Run: runPacman,
}

func init() {
	pacmanCmd.Flags().String("eat", "", "Close a bead and increment your score")
	_ = pacmanCmd.Flags().MarkHidden("eat")
	pacmanCmd.Flags().String("pause", "", "Pause agents with an optional reason")
	pacmanCmd.Flags().Bool("resume", false, "Clear pause signal")
	pacmanCmd.Flags().Bool("join", false, "Register yourself in agents.json")
	pacmanCmd.Flags().Bool("global", false, "Show aggregate stats across all projects in workspace")
	pacmanCmd.Flags().Bool("badge", false, "Generate GitHub badge markdown for your score")
	pacmanCmd.Flags().String("workspace", "", "Workspace root for --global (default: ~/Desktop/workspace)")
	rootCmd.AddCommand(pacmanCmd)
}

func runPacman(cmd *cobra.Command, args []string) {
	eatID, _ := cmd.Flags().GetString("eat")
	pauseReason, _ := cmd.Flags().GetString("pause")
	resumePause, _ := cmd.Flags().GetBool("resume")
	joinAgent, _ := cmd.Flags().GetBool("join")
	globalMode, _ := cmd.Flags().GetBool("global")
	badgeMode, _ := cmd.Flags().GetBool("badge")
	workspaceRoot, _ := cmd.Flags().GetString("workspace")
	agent := getPacmanAgentName()

	// Global mode: scan workspace for all projects
	if globalMode {
		runGlobalPacman(agent, workspaceRoot)
		return
	}

	// Badge mode: generate GitHub badge
	if badgeMode {
		generatePacmanBadge(agent)
		return
	}

	if pauseReason != "" {
		CheckReadonly("pacman")
		if err := writePacmanPause(pauseReason, agent); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		if !jsonOutput {
			fmt.Printf("%s Paused: %s\n", ui.RenderWarnIcon(), pauseReason)
		}
		return
	}

	if resumePause {
		CheckReadonly("pacman")
		if err := clearPacmanPause(); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		if !jsonOutput {
			fmt.Printf("%s Resumed\n", ui.RenderPassIcon())
		}
		return
	}

	if joinAgent {
		CheckReadonly("pacman")
		if err := registerPacmanAgent(agent); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		if !jsonOutput {
			fmt.Printf("%s Registered %s\n", ui.RenderPassIcon(), agent)
		}
		return
	}

	if eatID != "" {
		CheckReadonly("pacman")
		if err := closeIssueWithRouting(eatID, "Pacman eat"); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		if err := incrementPacmanScore(agent, 1); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		if !jsonOutput {
			fmt.Printf("%s Closed %s (score +1)\n", ui.RenderPassIcon(), eatID)
		}
	}

	state, err := buildPacmanState(agent)
	if err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	if jsonOutput {
		printJSON(state)
		return
	}

	renderPacmanState(state)
}

func renderPacmanArt(state pacmanState) {
	content := renderPacmanArtString(state)
	fmt.Println("╭──────────────────────────────────────────────────────────╮")
	padding := 60 - len([]rune(content))
	if padding < 0 {
		padding = 0
	}
	fmt.Printf("│%-60s│\n", content)
	fmt.Println("╰──────────────────────────────────────────────────────────╯")
}

func renderPacmanArtString(state pacmanState) string {
	var maze strings.Builder
	maze.WriteString("  ᗧ")

	for i, dot := range state.Dots {
		if i >= 4 {
			maze.WriteString("···")
			break
		}
		maze.WriteString("····○ ")
		id := dot.ID
		if len(id) > 6 {
			id = id[:6]
		}
		maze.WriteString(id)
	}

	if len(state.Blockers) > 0 {
		maze.WriteString(" ····◐")
		if len(state.Blockers) > 1 {
			maze.WriteString(fmt.Sprintf("x%d", len(state.Blockers)))
		}
	}

	if len(state.Dots) == 0 && len(state.Blockers) == 0 {
		maze.WriteString("····················✓ CLEAR!")
	}

	return maze.String()
}

func buildPacmanState(agent string) (pacmanState, error) {
	scoreboard, err := loadPacmanScoreboard()
	if err != nil {
		return pacmanState{}, err
	}

	score := 0
	if entry, ok := scoreboard.Scores[agent]; ok {
		score = entry.Dots
	}

	if daemonClient == nil && store == nil {
		return pacmanState{}, fmt.Errorf("beads database not found (run 'bd init')")
	}

	var dots []*types.Issue
	var blocked []*types.BlockedIssue
	if daemonClient != nil {
		resp, err := daemonClient.Ready(&rpc.ReadyArgs{})
		if err != nil {
			return pacmanState{}, err
		}
		if err := json.Unmarshal(resp.Data, &dots); err != nil {
			return pacmanState{}, err
		}

		blockedResp, err := daemonClient.Blocked(&rpc.BlockedArgs{})
		if err != nil {
			return pacmanState{}, err
		}
		if err := json.Unmarshal(blockedResp.Data, &blocked); err != nil {
			return pacmanState{}, err
		}
	} else {
		var err error
		dots, err = store.GetReadyWork(rootCtx, types.WorkFilter{})
		if err != nil {
			return pacmanState{}, err
		}
		blocked, err = store.GetBlockedIssues(rootCtx, types.WorkFilter{})
		if err != nil {
			return pacmanState{}, err
		}
	}

	state := pacmanState{
		Agent: agent,
		Score: score,
	}

	for _, issue := range dots {
		state.Dots = append(state.Dots, issueToInfo(issue))
	}

	for _, issue := range blocked {
		state.Blockers = append(state.Blockers, pacmanBlocker{
			ID:        issue.ID,
			BlockedBy: issue.BlockedBy,
		})
	}

	if pause, err := readPacmanPause(); err == nil {
		state.Paused = pause
	}

	state.Achievements = computePacmanAchievements(agent, time.Now().UTC(), state.Paused, score)
	state.Leaderboard = buildLeaderboard(scoreboard)
	return state, nil
}

func renderPacmanState(state pacmanState) {
	// ASCII art header
	renderPacmanArt(state)
	fmt.Println()

	if state.Paused != nil {
		fmt.Printf("%s PAUSED by %s: %s\n", ui.RenderWarnIcon(), state.Paused.From, state.Paused.Reason)
		fmt.Printf("%s Run: bd pacman --resume\n\n", ui.RenderInfoIcon())
	}
	fmt.Printf("YOU: %s\n", state.Agent)
	fmt.Printf("SCORE: %d dots\n\n", state.Score)

	fmt.Println("DOTS NEARBY:")
	if len(state.Dots) == 0 {
		fmt.Printf("  %s None\n", ui.RenderMuted(ui.GetStatusIcon(string(types.StatusOpen))))
	} else {
		for _, dot := range state.Dots {
			fmt.Printf("  %s %s %s \"%s\"\n",
				ui.GetStatusIcon(string(types.StatusOpen)),
				dot.ID,
				ui.RenderPriority(dot.Priority),
				dot.Title,
			)
		}
	}

	fmt.Println()
	fmt.Println("BLOCKERS:")
	if len(state.Blockers) == 0 {
		fmt.Printf("  %s None\n", ui.RenderMuted(ui.GetStatusIcon(string(types.StatusBlocked))))
	} else {
		for _, blocker := range state.Blockers {
			if len(blocker.BlockedBy) > 0 {
				fmt.Printf("  %s %s blocked by %s\n", ui.GetStatusIcon(string(types.StatusBlocked)), blocker.ID, blocker.BlockedBy[0])
			} else {
				fmt.Printf("  %s %s blocked\n", ui.GetStatusIcon(string(types.StatusBlocked)), blocker.ID)
			}
		}
	}

	fmt.Println()
	fmt.Println("LEADERBOARD:")
	if len(state.Leaderboard) == 0 {
		fmt.Println("  None")
	} else {
		for i, entry := range state.Leaderboard {
			fmt.Printf("  #%d %-16s %3d pts  %2d fixed\n", i+1, entry.Name, entry.Dots, entry.SkillsFixed)
		}
	}

	if len(state.Achievements) > 0 {
		fmt.Println()
		fmt.Println("ACHIEVEMENTS:")
		for _, achievement := range state.Achievements {
			fmt.Printf("  %s %s\n", ui.RenderPass("✓"), achievement.Label)
		}
	}
}

func getPacmanAgentName() string {
	if name := os.Getenv("AGENT_NAME"); name != "" {
		return name
	}
	if user := os.Getenv("USER"); user != "" {
		return user
	}
	return "unknown"
}

func pacmanScoreboardPath() string {
	if dbPath != "" {
		beadsDir := filepath.Dir(dbPath)
		return filepath.Join(beadsDir, "scoreboard.json")
	}
	return filepath.Join(".beads", "scoreboard.json")
}

func pacmanAgentsPath() string {
	if dbPath != "" {
		beadsDir := filepath.Dir(dbPath)
		return filepath.Join(beadsDir, "agents.json")
	}
	return filepath.Join(".beads", "agents.json")
}

func pacmanPausePath() string {
	if dbPath != "" {
		beadsDir := filepath.Dir(dbPath)
		return filepath.Join(beadsDir, "pause.json")
	}
	return filepath.Join(".beads", "pause.json")
}

func loadPacmanScoreboard() (pacmanScoreboard, error) {
	path := pacmanScoreboardPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return pacmanScoreboard{Scores: map[string]pacmanScore{}}, nil
		}
		return pacmanScoreboard{}, err
	}

	var scoreboard pacmanScoreboard
	if err := json.Unmarshal(data, &scoreboard); err != nil {
		return pacmanScoreboard{}, err
	}
	if scoreboard.Scores == nil {
		scoreboard.Scores = map[string]pacmanScore{}
	}
	return scoreboard, nil
}

func incrementPacmanScore(agent string, delta int) error {
	scoreboard, err := loadPacmanScoreboard()
	if err != nil {
		return err
	}
	entry := scoreboard.Scores[agent]
	entry.Dots += delta
	if entry.Dots < 0 {
		entry.Dots = 0
	}
	scoreboard.Scores[agent] = entry
	return writePacmanScoreboard(scoreboard)
}

func writePacmanScoreboard(scoreboard pacmanScoreboard) error {
	path := pacmanScoreboardPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(scoreboard, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func buildLeaderboard(scoreboard pacmanScoreboard) []pacmanLeader {
	history, err := loadWobbleHistory()
	if err != nil {
		history = nil
	}
	leaders := make([]pacmanLeader, 0, len(scoreboard.Scores))
	for name, score := range scoreboard.Scores {
		fixed := 0
		if len(history) > 0 {
			fixed = skillsFixedFromHistory(history, name)
		}
		leaders = append(leaders, pacmanLeader{Name: name, Dots: score.Dots, SkillsFixed: fixed})
	}
	sort.SliceStable(leaders, func(i, j int) bool {
		if leaders[i].Dots != leaders[j].Dots {
			return leaders[i].Dots > leaders[j].Dots
		}
		return leaders[i].Name < leaders[j].Name
	})
	return leaders
}

func loadPacmanAgents() (pacmanAgents, error) {
	path := pacmanAgentsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return pacmanAgents{Agents: []pacmanAgent{}}, nil
		}
		return pacmanAgents{}, err
	}
	var agents pacmanAgents
	if err := json.Unmarshal(data, &agents); err != nil {
		return pacmanAgents{}, err
	}
	return agents, nil
}

func registerPacmanAgent(name string) error {
	agents, err := loadPacmanAgents()
	if err != nil {
		return err
	}
	for _, agent := range agents.Agents {
		if agent.Name == name {
			return nil
		}
	}
	agents.Agents = append(agents.Agents, pacmanAgent{
		Name:   name,
		Joined: time.Now().Format("2006-01-02"),
	})
	return writePacmanAgents(agents)
}

func writePacmanAgents(agents pacmanAgents) error {
	path := pacmanAgentsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(agents, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func readPacmanPause() (*pacmanPause, error) {
	data, err := os.ReadFile(pacmanPausePath())
	if err != nil {
		return nil, err
	}
	var pause pacmanPause
	if err := json.Unmarshal(data, &pause); err != nil {
		return nil, err
	}
	return &pause, nil
}

func writePacmanPause(reason, from string) error {
	pause := pacmanPause{
		Reason: reason,
		From:   from,
		TS:     time.Now().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(pause, "", "  ")
	if err != nil {
		return err
	}
	path := pacmanPausePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func clearPacmanPause() error {
	path := pacmanPausePath()
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}

// Global mode: workspace-wide view across all projects
func runGlobalPacman(agent, workspaceRoot string) {
	if workspaceRoot == "" {
		home, _ := os.UserHomeDir()
		workspaceRoot = filepath.Join(home, "Desktop", "workspace")
	}

	type projectStats struct {
		Name   string
		Path   string
		Dots   int
		Ghosts int
		Score  int
	}

	var projects []projectStats
	totalDots := 0
	totalGhosts := 0
	totalScore := 0
	globalScores := map[string]pacmanScore{}

	// Walk workspace looking for .beads directories
	_ = filepath.Walk(workspaceRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}
		if info.Name() == ".beads" {
			projectPath := filepath.Dir(path)
			projectName := filepath.Base(projectPath)

			// Count dots (open issues via JSONL)
			dots := countDotsInProject(path)
			ghosts := countGhostsInProject(path)
			score := getScoreForAgent(path, agent)
			mergeScoreboard(path, globalScores)

			if dots > 0 || ghosts > 0 || score > 0 {
				projects = append(projects, projectStats{
					Name:   projectName,
					Path:   projectPath,
					Dots:   dots,
					Ghosts: ghosts,
					Score:  score,
				})
				totalDots += dots
				totalGhosts += ghosts
				totalScore += score
			}
			return filepath.SkipDir
		}
		// Skip nested deep directories
		if strings.Count(path, string(os.PathSeparator))-strings.Count(workspaceRoot, string(os.PathSeparator)) > 3 {
			return filepath.SkipDir
		}
		return nil
	})

	// Render global view
	fmt.Println("╭──────────────────────────────────────────────────────────╮")
	fmt.Printf("│  GLOBAL PACMAN · %d projects · %d dots · %d ghosts          │\n", len(projects), totalDots, totalGhosts)
	fmt.Println("╰──────────────────────────────────────────────────────────╯")
	fmt.Println()

	fmt.Printf("YOU: %s\n", agent)
	fmt.Printf("TOTAL SCORE: %d dots across all projects\n\n", totalScore)

	fmt.Println("PROJECTS:")
	if len(projects) == 0 {
		fmt.Println("  No projects with beads found")
	} else {
		// Sort by dots descending
		sort.SliceStable(projects, func(i, j int) bool {
			return projects[i].Dots > projects[j].Dots
		})
		for _, p := range projects {
			status := "✓"
			if p.Dots > 0 {
				status = fmt.Sprintf("%d○", p.Dots)
			}
			ghost := ""
			if p.Ghosts > 0 {
				ghost = fmt.Sprintf(" ◐%d", p.Ghosts)
			}
			fmt.Printf("  %s %-25s %s%s\n", status, p.Name, fmt.Sprintf("(%d pts)", p.Score), ghost)
		}
	}

	if len(globalScores) > 0 {
		fmt.Println()
		fmt.Println("LEADERBOARD:")
		leaders := buildLeaderboard(pacmanScoreboard{Scores: globalScores})
		for i, entry := range leaders {
			fmt.Printf("  #%d %-16s %3d pts  %2d fixed\n", i+1, entry.Name, entry.Dots, entry.SkillsFixed)
		}
	}
}

func countDotsInProject(beadsDir string) int {
	issuesPath := filepath.Join(beadsDir, "issues.jsonl")
	data, err := os.ReadFile(issuesPath)
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"status":"open"`) || strings.Contains(line, `"status":"in_progress"`) {
			count++
		}
	}
	return count
}

func countGhostsInProject(beadsDir string) int {
	issuesPath := filepath.Join(beadsDir, "issues.jsonl")
	data, err := os.ReadFile(issuesPath)
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"status":"blocked"`) {
			count++
		}
	}
	return count
}

func getScoreForAgent(beadsDir, agent string) int {
	scoreboardPath := filepath.Join(beadsDir, "scoreboard.json")
	data, err := os.ReadFile(scoreboardPath)
	if err != nil {
		return 0
	}
	var scoreboard pacmanScoreboard
	if err := json.Unmarshal(data, &scoreboard); err != nil {
		return 0
	}
	if entry, ok := scoreboard.Scores[agent]; ok {
		return entry.Dots
	}
	return 0
}

func mergeScoreboard(beadsDir string, scores map[string]pacmanScore) {
	scoreboardPath := filepath.Join(beadsDir, "scoreboard.json")
	data, err := os.ReadFile(scoreboardPath)
	if err != nil {
		return
	}
	var scoreboard pacmanScoreboard
	if err := json.Unmarshal(data, &scoreboard); err != nil {
		return
	}
	for name, entry := range scoreboard.Scores {
		current := scores[name]
		current.Dots += entry.Dots
		scores[name] = current
	}
}

// Badge mode: generate GitHub profile badge
func generatePacmanBadge(agent string) {
	scoreboard, err := loadPacmanScoreboard()
	if err != nil {
		fmt.Println("No scoreboard found")
		return
	}

	score := 0
	if entry, ok := scoreboard.Scores[agent]; ok {
		score = entry.Dots
	}

	// Generate shields.io badge
	color := "brightgreen"
	if score == 0 {
		color = "lightgrey"
	} else if score < 5 {
		color = "yellow"
	} else if score < 20 {
		color = "green"
	}

	badgeURL := fmt.Sprintf("https://img.shields.io/badge/pacman%%20score-%d%%20dots-%s", score, color)
	markdown := fmt.Sprintf("![Pacman Score](%s)", badgeURL)

	fmt.Println("## GitHub Badge")
	fmt.Println()
	fmt.Println("Add to your README:")
	fmt.Println()
	fmt.Printf("  %s\n", markdown)
	fmt.Println()
	fmt.Println("Preview:")
	fmt.Printf("  ✓ %s: %d dots\n", agent, score)
}

func computePacmanAchievements(agent string, now time.Time, pause *pacmanPause, score int) []pacmanAchievement {
	db := (*sql.DB)(nil)
	if store != nil {
		db = store.UnderlyingDB()
	}
	if db == nil {
		if score > 0 {
			return []pacmanAchievement{{ID: "first-blood", Label: "First Blood"}}
		}
		return nil
	}
	return computePacmanAchievementsFromDB(db, agent, now, pause)
}

func computePacmanAchievementsFromDB(db *sql.DB, agent string, now time.Time, pause *pacmanPause) []pacmanAchievement {
	achievements := []pacmanAchievement{}
	closedEvents, err := loadClosedEvents(db, agent)
	if err != nil {
		return achievements
	}

	if len(closedEvents) > 0 {
		achievements = append(achievements, pacmanAchievement{ID: "first-blood", Label: "First Blood"})
	}

	closedToday := 0
	for _, event := range closedEvents {
		if sameDay(event.CreatedAt, now) {
			closedToday++
		}
	}
	if closedToday >= 5 {
		achievements = append(achievements, pacmanAchievement{ID: "streak-5", Label: "Streak 5"})
	}

	if hasGhostBuster(db, closedEvents) {
		achievements = append(achievements, pacmanAchievement{ID: "ghost-buster", Label: "Ghost Buster"})
	}

	if hasAssistMaster(db, closedEvents) {
		achievements = append(achievements, pacmanAchievement{ID: "assist-master", Label: "Assist Master"})
	}

	if pause != nil {
		if ts, err := time.Parse(time.RFC3339, pause.TS); err == nil {
			for _, event := range closedEvents {
				if event.CreatedAt.After(ts) {
					achievements = append(achievements, pacmanAchievement{ID: "comeback", Label: "Comeback"})
					break
				}
			}
		}
	}

	return achievements
}

type pacmanClosedEvent struct {
	IssueID   string
	CreatedAt time.Time
}

func loadClosedEvents(db *sql.DB, agent string) ([]pacmanClosedEvent, error) {
	rows, err := db.Query(`
		SELECT issue_id, created_at
		FROM events
		WHERE event_type = ? AND actor = ?
	`, types.EventClosed, agent)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var events []pacmanClosedEvent
	for rows.Next() {
		var event pacmanClosedEvent
		if err := rows.Scan(&event.IssueID, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func hasGhostBuster(db *sql.DB, closed []pacmanClosedEvent) bool {
	for _, event := range closed {
		var count int
		err := db.QueryRow(`
			SELECT COUNT(*)
			FROM events
			WHERE issue_id = ? AND event_type = ?
			  AND (LOWER(new_value) = ? OR new_value LIKE ?)
		`, event.IssueID, types.EventStatusChanged, string(types.StatusBlocked), "%blocked%").Scan(&count)
		if err == nil && count > 0 {
			return true
		}
	}
	return false
}

func hasAssistMaster(db *sql.DB, closed []pacmanClosedEvent) bool {
	for _, event := range closed {
		var count int
		err := db.QueryRow(`
			SELECT COUNT(*)
			FROM dependencies d
			JOIN issues i ON i.id = d.issue_id
			WHERE d.depends_on_id = ?
			  AND i.status IN ('open', 'in_progress')
		`, event.IssueID).Scan(&count)
		if err == nil && count > 0 {
			return true
		}
	}
	return false
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
