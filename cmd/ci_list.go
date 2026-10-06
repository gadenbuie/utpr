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
	Long: `List the most recent GitHub Actions runs, any status, grouped by
commit so jobs from the same push appear together.

By default runs for the current branch are shown, up to --limit
(default 10); --all lists recent runs for the whole repository. Groups
touched by the limit window are always shown complete. Each group
heading shows the short SHA, the branch (unless a status heading
already states it), the associated PR, and when the group started;
each row shows the workflow and either elapsed time (in-flight) or
total duration (completed).

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
// so tests can swap in fakes. Group completion reuses the per-SHA seam
// from ci_reasons.go (ghListWorkflowRunsForSHA).
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

	type ciListData struct {
		groups []ciListGroup
		total  int
	}
	data, derr := spinCIWithResult("Fetching CI runs...", func() (ciListData, error) {
		window, total, err := ghListRecentWorkflowRuns(ownerRepo, branch, flagCIListLimit)
		if err != nil {
			return ciListData{}, err
		}
		return ciListData{groups: completeCIListGroups(ownerRepo, branch, window), total: total}, nil
	})
	if derr != nil {
		return ui.Dief("Could not fetch CI runs: %v", derr)
	}
	if len(data.groups) == 0 {
		printCIInfo(ciAgentMode(), ciListEmptyMessage(ownerRepo, branch))
		return nil
	}
	prBySHA, _ := ciListFetchPRs(ownerRepo) // best-effort

	shown := 0
	for _, g := range data.groups {
		shown += len(g.runs)
	}

	// The status heading appears on feature branches only; on the default
	// branch and with --all the group headings carry the branch instead.
	topHeading := ciListTopHeading(cfg, branch, data.total, shown)
	frame := renderCIListGroups(data.groups, prBySHA, topHeading == "", topHeading, time.Now())
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

// ciListTopHeading returns the feature-branch status heading stating the
// branch and the total number of its runs; empty on the default branch
// and with --all, where group headings carry the branch instead.
func ciListTopHeading(cfg *remote.Config, branch string, total, shown int) string {
	if branch == "" || branch == cfg.DefaultBranch {
		return ""
	}
	label := lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("branch '%s'", branch))
	count := fmt.Sprintf("%d %s", total, pluralRuns(total))
	if total == shown {
		return label + " " + ui.StyleMuted.Render("— "+count)
	}
	return label + " " + ui.StyleMuted.Render(fmt.Sprintf("— %s, showing latest %d", count, shown))
}

func pluralRuns(n int) string {
	if n == 1 {
		return "run"
	}
	return "runs"
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

// ciListGroup is the set of workflow runs for one commit, newest first.
type ciListGroup struct {
	sha  string
	runs []gh.WorkflowRun
}

// branch returns the group's branch name from its newest run.
func (g ciListGroup) branch() string {
	for _, r := range g.runs {
		if r.HeadBranch != "" {
			return r.HeadBranch
		}
	}
	return ""
}

// startedAt returns the group's earliest run start time.
func (g ciListGroup) startedAt() (time.Time, bool) {
	var best time.Time
	found := false
	for _, r := range g.runs {
		if t, ok := ciListRunStart(r); ok && (!found || t.Before(best)) {
			best = t
			found = true
		}
	}
	return best, found
}

// groupRunsBySHA groups runs by head SHA in order of first appearance.
func groupRunsBySHA(runs []gh.WorkflowRun) []ciListGroup {
	var groups []ciListGroup
	idx := map[string]int{}
	for _, r := range runs {
		if i, ok := idx[r.HeadSHA]; ok {
			groups[i].runs = append(groups[i].runs, r)
			continue
		}
		idx[r.HeadSHA] = len(groups)
		groups = append(groups, ciListGroup{sha: r.HeadSHA, runs: []gh.WorkflowRun{r}})
	}
	return groups
}

// completeCIListGroups completes each touched group with all runs for its
// commit, fetched per-SHA; in branch mode fetched runs from other branches
// sharing the commit are excluded. Fetch failures keep the window's runs
// for that group (best-effort).
func completeCIListGroups(ownerRepo, branch string, window []gh.WorkflowRun) []ciListGroup {
	groups := groupRunsBySHA(window)
	for i, g := range groups {
		all, err := ghListWorkflowRunsForSHA(ownerRepo, g.sha)
		if err != nil || len(all) == 0 {
			continue
		}
		if branch != "" {
			if kept := filterRunsByBranch(all, branch); len(kept) > 0 {
				all = kept
			} else {
				continue
			}
		}
		groups[i].runs = all
	}
	return groups
}

// filterRunsByBranch keeps only runs whose head branch matches, so groups
// don't pick up runs that other branches share with the same commit.
func filterRunsByBranch(runs []gh.WorkflowRun, branch string) []gh.WorkflowRun {
	kept := make([]gh.WorkflowRun, 0, len(runs))
	for _, r := range runs {
		if r.HeadBranch == branch {
			kept = append(kept, r)
		}
	}
	return kept
}

// renderCIListGroups renders the grouped listing. topHeading, when
// non-empty, is a status line above the groups; group headings include
// the branch when includeBranchHeading. Completed rows show their total
// duration, in-flight rows elapsed time as of now.
func renderCIListGroups(groups []ciListGroup, prBySHA map[string]gh.PRInfo, includeBranchHeading bool, topHeading string, now time.Time) ciListFrame {
	type row struct {
		icon     string
		workflow string
		elapsed  string
	}
	type renderedGroup struct {
		heading string
		rows    []row
	}

	widths := [3]int{}
	rendered := make([]renderedGroup, len(groups))
	var state strings.Builder
	for i, g := range groups {
		rows := make([]row, 0, len(g.runs))
		for _, r := range g.runs {
			var elapsed string
			if r.Status == "completed" {
				elapsed = ciListRunDuration(r)
			} else {
				elapsed = ciListRunElapsed(r, now)
			}
			rw := row{icon: ciListRunIcon(r.Status, r.Conclusion), workflow: r.Name, elapsed: elapsed}
			rows = append(rows, rw)
			widths[0] = max(widths[0], lipgloss.Width(rw.icon))
			widths[1] = max(widths[1], lipgloss.Width(rw.workflow))
			widths[2] = max(widths[2], lipgloss.Width(rw.elapsed))
			fmt.Fprintf(&state, "%s/%d:%s:%s;", g.sha, r.ID, r.Status, r.Conclusion)
		}
		rendered[i] = renderedGroup{
			heading: ciListGroupHeading(g, prBySHA, includeBranchHeading),
			rows:    rows,
		}
	}

	pad := func(s string, w int) string {
		return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
	}

	var b strings.Builder
	if topHeading != "" {
		b.WriteString(topHeading)
		b.WriteString("\n\n")
	}
	for i, g := range rendered {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(g.heading)
		b.WriteByte('\n')
		for _, r := range g.rows {
			b.WriteString(pad(r.icon, widths[0]) + "  " + pad(r.workflow, widths[1]) + "  " + r.elapsed)
			b.WriteByte('\n')
		}
	}
	return ciListFrame{content: b.String(), state: state.String()}
}

// ciListGroupHeading renders a group heading: short SHA with "on <branch>"
// when includeBranch, then the associated PR title and the group's start
// time as a local clock time, separated by " · ".
func ciListGroupHeading(g ciListGroup, prBySHA map[string]gh.PRInfo, includeBranch bool) string {
	first := lipgloss.NewStyle().Bold(true).Render(shortSHA(g.sha))
	if includeBranch {
		if b := g.branch(); b != "" {
			first += " " + ui.StyleMuted.Render("on "+b)
		}
	}
	parts := []string{first}
	if pr, ok := prBySHA[g.sha]; ok {
		parts = append(parts, fmt.Sprintf("#%d %s", pr.Number, truncateRunes(pr.Title, 40)))
	}
	if started, ok := g.startedAt(); ok {
		parts = append(parts, ui.StyleMuted.Render("started "+ciListStartClock(started)))
	}
	return strings.Join(parts, ui.StyleMuted.Render(" · "))
}

// ciListStartClock renders a start time as a local clock time, including
// the date when it is not from today.
func ciListStartClock(t time.Time) string {
	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04")
	}
	return t.Format("Jan 2 15:04")
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
// grouped view. Frames show the groups containing in-flight runs; the
// final frame shows the complete watched groups with their conclusions.
// Styled interactive frames overwrite in place; piped/agent frames print
// only when the group set or a run's status/conclusion changes, separated
// by blank lines. Exits nil when the queue drains.
func watchCIList(ownerRepo, branch string) error {
	agent := ciAgentMode()
	interactive := !agent && term.IsTerminal(int(os.Stderr.Fd()))

	var prevLines int    // interactive: rows of the previous frame
	var lastState string // piped: state key of the last printed frame
	prBySHA := map[string]gh.PRInfo{}
	prKnown := map[string]bool{} // SHAs already looked up, with or without a PR
	seenOrder := []string{}      // SHA order of first appearance
	seenBySHA := map[string][]gh.WorkflowRun{}

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

		// Remember every in-flight run seen per SHA, in first-seen order,
		// so the final frame can show each watched run even if a per-SHA
		// fetch misses it.
		for _, r := range runs {
			if _, ok := seenBySHA[r.HeadSHA]; !ok {
				seenOrder = append(seenOrder, r.HeadSHA)
				seenBySHA[r.HeadSHA] = nil
			}
			known := false
			for _, s := range seenBySHA[r.HeadSHA] {
				if s.ID == r.ID {
					known = true
					break
				}
			}
			if !known {
				seenBySHA[r.HeadSHA] = append(seenBySHA[r.HeadSHA], r)
			}
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
			finalGroups := completeWatchedGroups(ownerRepo, branch, seenOrder, seenBySHA)
			frame := renderCIListGroups(finalGroups, prBySHA, true, "", time.Now())
			printFrame(frame, lastState != "")
			return nil
		}

		frame := renderCIListGroups(groupRunsBySHA(runs), prBySHA, true, "", time.Now())
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

// completeWatchedGroups returns the complete groups for the watched SHAs
// in first-seen order, fetched per-SHA at drain; in branch mode fetched
// runs from other branches sharing the commit are excluded. Every watched
// run appears in the result, and no run is left in a non-completed state.
func completeWatchedGroups(ownerRepo, branch string, seenOrder []string, seenBySHA map[string][]gh.WorkflowRun) []ciListGroup {
	groups := make([]ciListGroup, 0, len(seenOrder))
	for _, sha := range seenOrder {
		seen := seenBySHA[sha]
		var runs []gh.WorkflowRun
		if all, err := ghListWorkflowRunsForSHA(ownerRepo, sha); err == nil && len(all) > 0 {
			if branch != "" {
				all = filterRunsByBranch(all, branch)
			}
			runs = finalizeWatchedRuns(all, seen)
		} else {
			runs = finalizeWatchedRuns(nil, seen)
		}
		groups = append(groups, ciListGroup{sha: sha, runs: runs})
	}
	return groups
}

// finalizeWatchedRuns normalizes a drained group for the final frame:
// watched runs missing from the fetch are added back completed with an
// unknown conclusion, and every run still in a non-completed state —
// stale in-flight copies returned by eventual consistency — is marked
// the same way. The queue has drained, so nothing can legitimately be
// still running here.
func finalizeWatchedRuns(all, seen []gh.WorkflowRun) []gh.WorkflowRun {
	known := map[int64]bool{}
	for i := range all {
		known[all[i].ID] = true
	}
	for _, r := range seen {
		if !known[r.ID] {
			r.Status = "completed"
			r.Conclusion = ""
			r.UpdatedAt = ""
			all = append(all, r)
			known[r.ID] = true
		}
	}
	for i := range all {
		if all[i].Status != "completed" {
			all[i].Status = "completed"
			all[i].Conclusion = ""
			all[i].UpdatedAt = ""
		}
	}
	return all
}
