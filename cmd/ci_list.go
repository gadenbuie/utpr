package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var ciListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent GitHub Actions runs",
	Long: `List the most recent GitHub Actions runs, any status.

Shows the most recent workflow runs (any status) for the current
branch, up to --limit (default 10); --all lists recent runs for the
whole repository. Each row shows the workflow, branch (--all only),
short commit SHA, associated PR, and either elapsed time (in-flight)
or total duration (completed).

Use --watch to poll until no runs are running or queued; the final view
shows each run's conclusion. --watch is a status view and exits 0 when
the queue drains ('utpr ci --wait' remains the pass/fail gate); --limit
does not apply to --watch.`,
	Args: cobra.NoArgs,
	RunE: runCIList,
}

var (
	flagCIListAll   bool
	flagCIListWatch bool
	flagCIListAgent bool
	flagCIListLimit int
)

// GitHub API and git seams for the list view, kept as package-level vars
// so tests can swap in fakes.
var (
	ghListRunningWorkflowRuns = gh.ListRunningWorkflowRuns
	ghListRecentWorkflowRuns  = gh.ListRecentWorkflowRuns
	ghListOpenPRs             = gh.ListPRs
	ciListCurrentBranch       = git.GetCurrentBranch
	ciListRepoFromConfig      = ciListOwnerRepo
)

func init() {
	ciListCmd.Flags().BoolVar(&flagCIListAll, "all", false, "List recent runs for the whole repository, not just the current branch")
	ciListCmd.Flags().BoolVar(&flagCIListWatch, "watch", false, "Poll until no runs are running or queued; the final view shows each conclusion")
	ciListCmd.Flags().IntVar(&flagCIListLimit, "limit", 10, "Number of recent runs to show (not used with --watch)")
	ciListCmd.Flags().BoolVar(&flagCIListAgent, "agent", false, "Show unstyled output for agent consumption")
	ciListCmd.Flags().BoolVar(&flagCIPretty, "pretty", false, "Force styled output even when stdout is not a terminal")
	ciCmd.AddCommand(ciListCmd)
}

// ciListOwnerRepo resolves the owner/repo to list runs for, following the
// same source remote resolution as 'utpr ci'.
func ciListOwnerRepo(cfg *remote.Config) (string, error) {
	sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote)
	if err != nil {
		return "", fmt.Errorf("could not determine remote URL for '%s'", cfg.SourceRemote)
	}
	ownerRepo, err := remote.ParseRepoSpec(sourceURL)
	if err != nil {
		return "", fmt.Errorf("could not parse repository from remote URL: %s", sourceURL)
	}
	return ownerRepo, nil
}

func runCIList(cmd *cobra.Command, args []string) error {
	cfg, err := ciRemoteDetect()
	if err != nil {
		return ui.Die(err.Error())
	}
	ownerRepo, err := ciListRepoFromConfig(cfg)
	if err != nil {
		return ui.Die(err.Error())
	}

	branch := ""
	if !flagCIListAll {
		b, berr := ciListCurrentBranch()
		if berr != nil {
			return ui.Die("Could not determine the current branch. Use --all to list runs for the whole repository.")
		}
		branch = b
	}

	if flagCIListWatch {
		if cmd != nil && cmd.Flags().Changed("limit") {
			return ui.Dief("--limit cannot be combined with --watch")
		}
		return watchCIList(ownerRepo, branch)
	}

	if flagCIListLimit < 1 {
		return ui.Dief("--limit must be at least 1")
	}

	runs, rerr := spinCIWithResult("Fetching CI runs...", func() ([]gh.WorkflowRun, error) {
		return ghListRecentWorkflowRuns(ownerRepo, branch, flagCIListLimit)
	})
	if rerr != nil {
		return ui.Dief("Could not fetch CI runs: %v", rerr)
	}
	if len(runs) == 0 {
		printCIInfo(ciAgentMode(), ciListEmptyMessage(ownerRepo, branch))
		return nil
	}
	prBySHA, _ := ciListFetchPRs(ownerRepo) // best-effort
	frame := renderCIListFrame(runs, prBySHA, flagCIListAll, time.Now())
	ciListPrintFrame(frame)
	return nil
}

// ciListEmptyMessage describes the empty state for the one-shot list in
// the active mode.
func ciListEmptyMessage(ownerRepo, branch string) string {
	if branch == "" {
		return fmt.Sprintf("No CI runs found in %s.", ownerRepo)
	}
	return fmt.Sprintf("No CI runs found on branch '%s'.", branch)
}

// ciWatchEmptyMessage describes the watch empty state (no in-flight runs).
func ciWatchEmptyMessage(ownerRepo, branch string) string {
	if branch == "" {
		return fmt.Sprintf("No running CI runs in %s.", ownerRepo)
	}
	return fmt.Sprintf("No running CI runs on branch '%s'.", branch)
}

// ciListFetchPRs fetches open PRs once and indexes them by head SHA, so
// fork PRs are matched with a single API call regardless of branch count.
func ciListFetchPRs(ownerRepo string) (map[string]gh.PRInfo, error) {
	prs, err := ghListOpenPRs(ownerRepo, "open")
	if err != nil {
		return nil, err
	}
	bySHA := make(map[string]gh.PRInfo, len(prs))
	for _, pr := range prs {
		bySHA[pr.Head.SHA] = pr
	}
	return bySHA, nil
}

// ciListFrame holds the rendered list view and the state key used to
// detect changes between watch polls.
type ciListFrame struct {
	content string
	state   string
}

// renderCIListFrame renders the table for runs. includeBranch adds the
// branch column (repo-wide --all mode). Completed rows show their total
// duration; in-flight rows show elapsed time as of now.
func renderCIListFrame(runs []gh.WorkflowRun, prBySHA map[string]gh.PRInfo, includeBranch bool, now time.Time) ciListFrame {
	type row struct {
		icon      string
		workflow  string
		branch    string
		sha       string
		pr        string
		elapsed   string
		statePart string
	}
	rows := make([]row, 0, len(runs))
	for _, r := range runs {
		prLabel := "—"
		if pr, ok := prBySHA[r.HeadSHA]; ok {
			prLabel = fmt.Sprintf("#%d %s", pr.Number, truncateRunes(pr.Title, 40))
		}
		var elapsed string
		if r.Status == "completed" {
			elapsed = ciListRunDuration(r)
		} else {
			elapsed = ciListRunElapsed(r, now)
		}
		icon := ciListRunIcon(r.Status, r.Conclusion)
		rows = append(rows, row{
			icon:      icon,
			workflow:  r.Name,
			branch:    r.HeadBranch,
			sha:       shortSHA(r.HeadSHA),
			pr:        prLabel,
			elapsed:   elapsed,
			statePart: fmt.Sprintf("%d:%s:%s", r.ID, r.Status, r.Conclusion),
		})
	}

	widths := make([]int, 6)
	for _, r := range rows {
		widths[0] = max(widths[0], lipgloss.Width(r.icon))
		widths[1] = max(widths[1], lipgloss.Width(r.workflow))
		if includeBranch {
			widths[2] = max(widths[2], lipgloss.Width(r.branch))
		}
		widths[3] = max(widths[3], lipgloss.Width(r.sha))
		widths[4] = max(widths[4], lipgloss.Width(r.pr))
		widths[5] = max(widths[5], lipgloss.Width(r.elapsed))
	}

	var state strings.Builder
	for _, r := range rows {
		state.WriteString(r.statePart)
		state.WriteByte(';')
	}

	var lines []string
	for _, r := range rows {
		pad := func(s string, w int) string {
			if s == "" {
				return s
			}
			return s + strings.Repeat(" ", w-lipgloss.Width(s))
		}
		cells := []string{pad(r.icon, widths[0]), pad(r.workflow, widths[1])}
		if includeBranch {
			cells = append(cells, pad(r.branch, widths[2]))
		}
		cells = append(cells, pad(r.sha, widths[3]), pad(r.pr, widths[4]), r.elapsed)
		lines = append(lines, strings.Join(cells, "  "))
	}

	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return ciListFrame{content: b.String(), state: state.String()}
}

// ciListRunIcon returns the row icon: … for in_progress, ○ for queued,
// and the standard statusIcon conclusion glyphs for completed runs.
func ciListRunIcon(status, conclusion string) string {
	if status == "completed" {
		return statusIcon(status, conclusion)
	}
	if status == "queued" {
		return ui.StyleMuted.Render("○")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render("…")
}

// shortSHA abbreviates a full SHA to the conventional 7 characters.
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// truncateRunes shortens s to max runes, appending an ellipsis.
func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// ciListRunStart parses when a run started, falling back to when it was
// created (queued runs may have an empty run_started_at).
func ciListRunStart(r gh.WorkflowRun) (time.Time, bool) {
	stamp := r.RunStartedAt
	if stamp == "" {
		stamp = r.CreatedAt
	}
	if stamp == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// ciListRunElapsed renders time elapsed since the run started, e.g. "42s",
// "4m", "1h 05m".
func ciListRunElapsed(r gh.WorkflowRun, now time.Time) string {
	start, ok := ciListRunStart(r)
	if !ok {
		return "—"
	}
	return formatCIListElapsed(now.Sub(start))
}

// ciListRunDuration renders a completed run's total duration from
// run_started_at to updated_at, falling back to elapsed time.
func ciListRunDuration(r gh.WorkflowRun) string {
	start, ok := ciListRunStart(r)
	if !ok {
		return "—"
	}
	end := r.UpdatedAt
	if end == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339, end)
	if err != nil {
		return "—"
	}
	return formatCIListElapsed(t.Sub(start))
}

// formatCIListElapsed renders a duration as "42s", "4m", "1h 05m".
func formatCIListElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return strconv.Itoa(int(d.Seconds())) + "s"
	}
	if d < time.Hour {
		return strconv.Itoa(int(d.Minutes())) + "m"
	}
	return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}

// ciListPrintFrame writes a rendered frame, styled to stderr or plain to
// stdout depending on agent mode.
func ciListPrintFrame(frame ciListFrame) {
	if ciAgentMode() {
		_, _ = fmt.Fprint(os.Stdout, ui.StripANSI(frame.content))
	} else {
		_, _ = fmt.Fprint(os.Stderr, frame.content)
	}
}

// watchCIList polls running runs until the queue drains, re-rendering the
// list. Styled interactive frames overwrite in place; piped/agent frames
// print only when the run set or a run's status/conclusion changes. The
// final frame shows each watched run's conclusion. Exits nil when the
// queue drains.
func watchCIList(ownerRepo, branch string) error {
	agent := ciAgentMode()
	interactive := !agent && term.IsTerminal(int(os.Stderr.Fd()))

	var prevLines int    // interactive: rows of the previous frame
	var lastState string // piped: state key of the last printed frame
	prBySHA := map[string]gh.PRInfo{}
	prKnown := map[string]bool{} // SHAs already looked up, with or without a PR
	seenOrder := []int64{}
	seen := map[int64]gh.WorkflowRun{}

	printFrame := func(frame ciListFrame, leadingBlank bool) {
		if agent {
			if leadingBlank {
				_, _ = fmt.Fprintln(os.Stdout)
			}
			_, _ = fmt.Fprint(os.Stdout, ui.StripANSI(frame.content))
			return
		}
		if interactive {
			if prevLines > 0 {
				fmt.Fprintf(os.Stderr, "\033[%dA\033[J", prevLines)
			}
			_, _ = fmt.Fprint(os.Stderr, frame.content)
			prevLines = countTerminalRows(frame.content, ui.GetTermWidth())
			return
		}
		if leadingBlank {
			_, _ = fmt.Fprintln(os.Stderr)
		}
		_, _ = fmt.Fprint(os.Stderr, frame.content)
	}

	for {
		runs, err := ghListRunningWorkflowRuns(ownerRepo, branch)
		if err != nil {
			return ui.Dief("Could not fetch CI runs: %v", err)
		}
		for _, r := range runs {
			if _, ok := seen[r.ID]; !ok {
				seenOrder = append(seenOrder, r.ID)
			}
			seen[r.ID] = r
		}

		// Refresh the PR cache only when a run's SHA has never been looked
		// up; SHAs covered by a previous fetch keep their answer (including
		// "no PR") across polls.
		missing := false
		for _, r := range runs {
			if !prKnown[r.HeadSHA] {
				missing = true
				break
			}
		}
		if missing {
			if fetched, ferr := ciListFetchPRs(ownerRepo); ferr == nil {
				for sha, pr := range fetched {
					prBySHA[sha] = pr
					prKnown[sha] = true
				}
				for _, r := range runs {
					prKnown[r.HeadSHA] = true
				}
			}
		}

		if len(runs) == 0 {
			// Queue drained: render the final frame with conclusions.
			if len(seenOrder) == 0 {
				printCIInfo(agent, ciWatchEmptyMessage(ownerRepo, branch))
				return nil
			}
			finalRuns := ciListConcludedRuns(ownerRepo, branch, seenOrder, seen)
			frame := renderCIListFrame(finalRuns, prBySHA, flagCIListAll, time.Now())
			printFrame(frame, lastState != "")
			return nil
		}

		frame := renderCIListFrame(runs, prBySHA, flagCIListAll, time.Now())
		if agent || !interactive {
			if frame.state != lastState {
				printFrame(frame, lastState != "")
				lastState = frame.state
			}
		} else {
			printFrame(frame, false)
		}
		time.Sleep(ciPollInterval)
	}
}

// ciListConcludedRuns returns the watched runs, ordered first appearance,
// with their final status and conclusion. Conclusions come from one recent
// runs fetch; a watched run missing from it is marked completed with an
// unknown conclusion rather than shown as still running.
func ciListConcludedRuns(ownerRepo, branch string, seenOrder []int64, seen map[int64]gh.WorkflowRun) []gh.WorkflowRun {
	concluded := seen
	if recent, err := ghListRecentWorkflowRuns(ownerRepo, branch, 100); err == nil {
		byID := make(map[int64]gh.WorkflowRun, len(recent))
		for _, r := range recent {
			byID[r.ID] = r
		}
		concluded = byID
	}
	runs := make([]gh.WorkflowRun, 0, len(seenOrder))
	for _, id := range seenOrder {
		final, ok := concluded[id]
		if !ok || final.Status != "completed" {
			// The run finished but its conclusion is unavailable (missing
			// from the recent page, or still listed in-flight by eventual
			// consistency); render it as unknown, not as still running.
			final = seen[id]
			final.Status = "completed"
			final.Conclusion = ""
			final.UpdatedAt = ""
		}
		runs = append(runs, final)
	}
	return runs
}
