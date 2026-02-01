package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

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

type pacmanState struct {
	Agent       string             `json:"agent"`
	Score       int                `json:"score"`
	Dots        []reflectIssueInfo `json:"dots,omitempty"`
	Ghosts      []pacmanGhost      `json:"ghosts,omitempty"`
	Leaderboard []pacmanLeader     `json:"leaderboard,omitempty"`
}

type pacmanGhost struct {
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
	rootCmd.AddCommand(pacmanCmd)
}

func runPacman(cmd *cobra.Command, args []string) {
	eatID, _ := cmd.Flags().GetString("eat")
	agent := getPacmanAgentName()

	if eatID != "" {
		CheckReadonly("pacman")
		if err := closeIssueWithRouting(eatID, "Pacman eat"); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		if err := incrementPacmanScore(agent, 1); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		if !jsonOutput {
			fmt.Printf("%s WAKAWAKA: %s\n", ui.RenderPassIcon(), eatID)
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
		state.Ghosts = append(state.Ghosts, pacmanGhost{
			ID:        issue.ID,
			BlockedBy: issue.BlockedBy,
		})
	}

	state.Leaderboard = buildLeaderboard(scoreboard)
	return state, nil
}

func renderPacmanState(state pacmanState) {
	fmt.Println(ui.RenderCategory("Pacman Mode"))
	fmt.Println(ui.RenderSeparator())
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
	fmt.Println("GHOSTS (blockers):")
	if len(state.Ghosts) == 0 {
		fmt.Printf("  %s None\n", ui.RenderMuted(ui.GetStatusIcon(string(types.StatusBlocked))))
	} else {
		for _, ghost := range state.Ghosts {
			if len(ghost.BlockedBy) > 0 {
				fmt.Printf("  %s %s blocked by %s\n", ui.GetStatusIcon(string(types.StatusBlocked)), ghost.ID, ghost.BlockedBy[0])
			} else {
				fmt.Printf("  %s %s blocked\n", ui.GetStatusIcon(string(types.StatusBlocked)), ghost.ID)
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
