package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	Agent       string             `json:"agent"`
	Score       int                `json:"score"`
	Dots        []reflectIssueInfo `json:"dots,omitempty"`
	Blockers    []pacmanBlocker    `json:"blockers,omitempty"`
	Paused      *pacmanPause       `json:"paused,omitempty"`
	Leaderboard []pacmanLeader     `json:"leaderboard,omitempty"`
}

type pacmanBlocker struct {
	ID        string   `json:"id"`
	BlockedBy []string `json:"blocked_by,omitempty"`
}

type pacmanLeader struct {
	Name string `json:"name"`
	Dots int    `json:"dots"`
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
	rootCmd.AddCommand(pacmanCmd)
}

func runPacman(cmd *cobra.Command, args []string) {
	eatID, _ := cmd.Flags().GetString("eat")
	pauseReason, _ := cmd.Flags().GetString("pause")
	resumePause, _ := cmd.Flags().GetBool("resume")
	joinAgent, _ := cmd.Flags().GetBool("join")
	agent := getPacmanAgentName()

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

	state.Leaderboard = buildLeaderboard(scoreboard)
	return state, nil
}

func renderPacmanState(state pacmanState) {
	fmt.Println(ui.RenderCategory("Pacman Mode"))
	fmt.Println(ui.RenderSeparator())
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
		return
	}
	for i, entry := range state.Leaderboard {
		fmt.Printf("  #%d %s  %d pts\n", i+1, entry.Name, entry.Dots)
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
	leaders := make([]pacmanLeader, 0, len(scoreboard.Scores))
	for name, score := range scoreboard.Scores {
		leaders = append(leaders, pacmanLeader{Name: name, Dots: score.Dots})
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
