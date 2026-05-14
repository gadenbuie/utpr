package cmd

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var ciCmd = &cobra.Command{
	Use:   "ci [#pr | @branch | number | branch]",
	Short: "View CI check status",
	Long: `Show CI check run status for the current branch or a specific PR or branch.

  utpr ci           current branch
  utpr ci 123       PR #123
  utpr ci #123      PR #123 (explicit)
  utpr ci main      branch 'main'
  utpr ci @main     branch 'main' (explicit)`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCI,
}

var ciLogsCmd = &cobra.Command{
	Use:   "logs [#pr | @branch | number | branch]",
	Short: "Show logs for failed CI jobs",
	Long:  "Show the last N lines of logs for failed CI jobs. Accepts the same target forms as 'utpr ci'.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runCILogs,
}

var (
	flagCIWeb    bool
	flagCIWatch  bool
	flagCIFailed bool
)

var (
	flagCILogsWeb        bool
	flagCILogsLines      int
	flagCILogsTimestamps bool
	flagCILogsAll        bool
	flagCILogsJob        string
)

func init() {
	ciCmd.Flags().BoolVarP(&flagCIWeb, "web", "w", false, "Open checks in the browser")
	ciCmd.Flags().BoolVar(&flagCIWatch, "watch", false, "Poll until all checks complete")
	ciCmd.Flags().BoolVar(&flagCIFailed, "failed", false, "Show only failed checks")

	ciLogsCmd.Flags().BoolVarP(&flagCILogsWeb, "web", "w", false, "Open failed job in the browser")
	ciLogsCmd.Flags().IntVarP(&flagCILogsLines, "lines", "n", 100, "Number of log lines to show per job (0 = all)")
	ciLogsCmd.Flags().BoolVar(&flagCILogsTimestamps, "timestamps", false, "Show timestamps on log lines")
	ciLogsCmd.Flags().BoolVar(&flagCILogsAll, "all", false, "Show logs for all jobs, not just failed")
	ciLogsCmd.Flags().StringVar(&flagCILogsJob, "job", "", "Show logs for a specific job by name (substring match)")

	ciCmd.AddCommand(ciLogsCmd)
}

// resolveCISHA returns ownerRepo, the HEAD commit SHA, and the PR URL (empty
// if not from a PR number). If args[0] is a PR number, uses that PR's head SHA.
// Otherwise uses the local HEAD SHA.
func resolveCISHA(cfg *remote.Config, args []string) (ownerRepo, sha, prURL string, err error) {
	sourceURL, runErr := git.Run("remote", "get-url", cfg.SourceRemote)
	if runErr != nil {
		return "", "", "", ui.Dief("Could not determine remote URL for '%s'.", cfg.SourceRemote)
	}
	ownerRepo, err = remote.ParseRepoSpec(sourceURL)
	if err != nil {
		return "", "", "", ui.Dief("Could not parse repository from remote URL: %s", sourceURL)
	}

	if len(args) > 0 {
		arg := args[0]

		// #123 → explicit PR number
		// @main → explicit branch name
		// 123   → PR number (numeric)
		// main  → branch name (non-numeric string)
		switch {
		case strings.HasPrefix(arg, "#"):
			n, convErr := strconv.Atoi(arg[1:])
			if convErr != nil {
				return "", "", "", ui.Dief("Invalid PR number: %s", arg)
			}
			pr, prErr := gh.GetPR(ownerRepo, n)
			if prErr != nil {
				return "", "", "", ui.Dief("Could not fetch PR #%d.", n)
			}
			return ownerRepo, pr.Head.SHA, pr.HTMLURL, nil

		case strings.HasPrefix(arg, "@"):
			branch := arg[1:]
			sha, shaErr := gh.GetBranchSHA(ownerRepo, branch)
			if shaErr != nil {
				return "", "", "", ui.Dief("Could not find branch '%s'.", branch)
			}
			return ownerRepo, sha, "", nil

		default:
			if n, convErr := strconv.Atoi(arg); convErr == nil {
				pr, prErr := gh.GetPR(ownerRepo, n)
				if prErr != nil {
					return "", "", "", ui.Dief("Could not fetch PR #%d.", n)
				}
				return ownerRepo, pr.Head.SHA, pr.HTMLURL, nil
			}
			sha, shaErr := gh.GetBranchSHA(ownerRepo, arg)
			if shaErr != nil {
				return "", "", "", ui.Dief("Could not find branch '%s'.", arg)
			}
			return ownerRepo, sha, "", nil
		}
	}

	sha, err = git.RevParse("HEAD")
	if err != nil {
		return "", "", "", ui.Die("Could not determine HEAD commit.")
	}

	tracking := git.GetTrackingBranch()
	if tracking == "" {
		return "", "", "", ui.Die("Branch has no remote tracking branch. Push first with 'utpr push'.")
	}

	remoteSHA, revErr := git.RevParse("@{u}")
	if revErr != nil {
		return "", "", "", ui.Die("Could not determine remote branch HEAD.")
	}

	if sha != remoteSHA {
		ui.Info("Showing CI for the last pushed commit (HEAD has unpushed changes).")
		sha = remoteSHA
	}

	return ownerRepo, sha, "", nil
}

func runCI(cmd *cobra.Command, args []string) error {
	cfg, err := remote.Detect()
	if err != nil {
		return ui.Die(err.Error())
	}

	ownerRepo, sha, prURL, err := resolveCISHA(cfg, args)
	if err != nil {
		return err
	}

	if flagCIWeb {
		if prURL != "" {
			return openURL(prURL + "/checks")
		}
		return openURL(fmt.Sprintf("https://github.com/%s/commit/%s/checks", ownerRepo, sha))
	}

	if flagCIWatch {
		return watchCI(ownerRepo, sha)
	}

	type ciStatus struct {
		checkRuns    []gh.CheckRun
		workflowRuns []gh.WorkflowRun
	}
	data, err := ui.SpinWithResult("Fetching CI status...", func() (ciStatus, error) {
		checkRuns, crErr := gh.GetCheckRuns(ownerRepo, sha)
		if crErr != nil {
			return ciStatus{}, crErr
		}
		wfRuns, _ := gh.ListWorkflowRunsForSHA(ownerRepo, sha) // best-effort
		return ciStatus{checkRuns: checkRuns, workflowRuns: wfRuns}, nil
	})
	if err != nil {
		return ui.Dief("Could not fetch CI status: %v", err)
	}

	if len(data.checkRuns) == 0 {
		ui.Info("No checks found for this commit.")
		return nil
	}

	runs := data.checkRuns
	if flagCIFailed {
		var failed []gh.CheckRun
		for _, r := range runs {
			if isFailedCheckRun(r) {
				failed = append(failed, r)
			}
		}
		if len(failed) == 0 {
			ui.Success("No failed checks.")
			return nil
		}
		runs = failed
	}

	renderCheckRuns(runs, buildSuiteNameMap(data.workflowRuns), false)
	return nil
}

func watchCI(ownerRepo, sha string) error {
	for {
		checkRuns, err := gh.GetCheckRuns(ownerRepo, sha)
		if err != nil {
			return ui.Dief("Could not fetch CI status: %v", err)
		}
		wfRuns, _ := gh.ListWorkflowRunsForSHA(ownerRepo, sha) // best-effort

		clearScreen()
		renderCheckRuns(checkRuns, buildSuiteNameMap(wfRuns), true)

		allDone := true
		for _, r := range checkRuns {
			if r.Status != "completed" {
				allDone = false
				break
			}
		}
		if allDone || len(checkRuns) == 0 {
			break
		}

		time.Sleep(10 * time.Second)
	}
	return nil
}

func clearScreen() {
	fmt.Fprint(os.Stderr, "\033[2J\033[H")
}

// ciGroup holds a named group of check runs sharing a check suite.
type ciGroup struct {
	Name string
	Runs []gh.CheckRun
}

// buildSuiteNameMap returns a map from check suite ID to workflow run name.
func buildSuiteNameMap(wfRuns []gh.WorkflowRun) map[int64]string {
	m := map[int64]string{}
	for _, r := range wfRuns {
		if r.CheckSuiteID != 0 && r.Name != "" {
			m[r.CheckSuiteID] = r.Name
		}
	}
	return m
}

func groupCheckRuns(runs []gh.CheckRun, suiteNames map[int64]string) []ciGroup {
	var order []int64
	groups := map[int64]*ciGroup{}

	for _, r := range runs {
		sid := r.CheckSuite.ID
		if _, exists := groups[sid]; !exists {
			groups[sid] = &ciGroup{}
			order = append(order, sid)
		}
		groups[sid].Runs = append(groups[sid].Runs, r)
	}

	result := make([]ciGroup, 0, len(order))
	for _, sid := range order {
		g := groups[sid]
		if name, ok := suiteNames[sid]; ok {
			g.Name = name
		} else {
			g.Name = deriveGroupName(g.Runs)
		}
		result = append(result, *g)
	}
	return result
}

// deriveGroupName finds the common "Workflow / " prefix across runs in a group,
// falling back to the app name.
func deriveGroupName(runs []gh.CheckRun) string {
	if len(runs) == 0 {
		return "Checks"
	}
	parts := strings.SplitN(runs[0].Name, " / ", 2)
	if len(parts) < 2 {
		if runs[0].App.Name != "" {
			return runs[0].App.Name
		}
		return "Checks"
	}
	prefix := parts[0]
	for _, r := range runs[1:] {
		p := strings.SplitN(r.Name, " / ", 2)
		if len(p) < 2 || p[0] != prefix {
			if runs[0].App.Name != "" {
				return runs[0].App.Name
			}
			return "Checks"
		}
	}
	return prefix
}

func jobDisplayName(runName, groupName string) string {
	prefix := groupName + " / "
	return strings.TrimPrefix(runName, prefix)
}

func checkRunIcon(r gh.CheckRun) string {
	if r.Status != "completed" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render("…")
	}
	switch r.Conclusion {
	case "success":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("✓")
	case "failure", "timed_out", "action_required":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("✗")
	case "skipped", "cancelled", "neutral":
		return ui.StyleMuted.Render("○")
	default:
		return ui.StyleMuted.Render("?")
	}
}

func isFailedCheckRun(r gh.CheckRun) bool {
	return r.Status == "completed" && isFailedConclusion(r.Conclusion)
}

func isFailedConclusion(conclusion string) bool {
	switch conclusion {
	case "failure", "timed_out", "action_required", "startup_failure":
		return true
	}
	return false
}

func formatCheckDuration(r gh.CheckRun) string {
	if r.StartedAt == "" {
		return ""
	}
	started, err := time.Parse(time.RFC3339, r.StartedAt)
	if err != nil {
		return ""
	}
	var d time.Duration
	if r.Status != "completed" || r.CompletedAt == "" {
		d = time.Since(started)
		return ui.StyleMuted.Render("running " + ciFormatDuration(d))
	}
	completed, err := time.Parse(time.RFC3339, r.CompletedAt)
	if err != nil {
		return ""
	}
	d = completed.Sub(started)
	return ui.StyleMuted.Render(ciFormatDuration(d))
}

func ciFormatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func checkRunSummary(runs []gh.CheckRun) string {
	var passing, failing, running, skipped int
	for _, r := range runs {
		switch {
		case r.Status != "completed":
			running++
		case r.Conclusion == "success":
			passing++
		case isFailedConclusion(r.Conclusion):
			failing++
		default:
			skipped++
		}
	}
	var parts []string
	if passing > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(fmt.Sprintf("%d passing", passing)))
	}
	if failing > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(fmt.Sprintf("%d failing", failing)))
	}
	if running > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render(fmt.Sprintf("%d running", running)))
	}
	if skipped > 0 {
		parts = append(parts, ui.StyleMuted.Render(fmt.Sprintf("%d skipped", skipped)))
	}
	return strings.Join(parts, " · ")
}

func renderCheckRuns(runs []gh.CheckRun, suiteNames map[int64]string, showTimestamp bool) {
	if showTimestamp {
		ts := time.Now().Format("15:04:05")
		fmt.Fprintf(os.Stderr, "%s\n\n", ui.StyleMuted.Render("Checking CI... (updated "+ts+")"))
	}

	groups := groupCheckRuns(runs, suiteNames)
	groupHeaderStyle := lipgloss.NewStyle().Bold(true)
	bulletStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("4"))

	for _, g := range groups {
		fmt.Fprintf(os.Stderr, "%s %s\n",
			bulletStyle.Render("●"),
			groupHeaderStyle.Render(g.Name))

		maxLen := 0
		for _, r := range g.Runs {
			n := len([]rune(jobDisplayName(r.Name, g.Name)))
			if n > maxLen {
				maxLen = n
			}
		}

		for _, r := range g.Runs {
			icon := checkRunIcon(r)
			name := jobDisplayName(r.Name, g.Name)
			dur := formatCheckDuration(r)
			fmt.Fprintf(os.Stderr, "  %s  %s  %s\n",
				icon,
				ui.PadRight(name, maxLen),
				dur)
		}
		fmt.Fprintln(os.Stderr)
	}

	fmt.Fprintf(os.Stderr, "%s\n", checkRunSummary(runs))
}

// --- ci logs ---

var actionsTimestampRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z `)

func processLogLines(raw string, showTimestamps bool, n int) []string {
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")

	processed := make([]string, 0, len(lines))
	for _, line := range lines {
		if showTimestamps {
			loc := actionsTimestampRe.FindStringIndex(line)
			if loc != nil {
				ts := ui.StyleMuted.Render(line[:loc[1]-1])
				processed = append(processed, ts+" "+line[loc[1]:])
			} else {
				processed = append(processed, line)
			}
		} else {
			processed = append(processed, actionsTimestampRe.ReplaceAllString(line, ""))
		}
	}

	if n > 0 && len(processed) > n {
		processed = processed[len(processed)-n:]
	}
	return processed
}

func logSeparator(label string) string {
	w := ui.GetTermWidth()
	prefix := "── " + label + " "
	remaining := w - len([]rune(ui.StripANSI(prefix)))
	if remaining < 2 {
		remaining = 2
	}
	return ui.StyleMuted.Render(prefix + strings.Repeat("─", remaining))
}

// latestRunsPerWorkflow keeps the first (most recent) run per workflow name.
func latestRunsPerWorkflow(runs []gh.WorkflowRun) []gh.WorkflowRun {
	seen := map[string]bool{}
	var result []gh.WorkflowRun
	for _, r := range runs {
		if !seen[r.Name] {
			seen[r.Name] = true
			result = append(result, r)
		}
	}
	return result
}

// jobEntry pairs a workflow job with its parent run name.
type jobEntry struct {
	RunName string
	Job     gh.WorkflowJob
}

func workflowJobIcon(job gh.WorkflowJob) string {
	if job.Status != "completed" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render("…")
	}
	switch job.Conclusion {
	case "success":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("✓")
	case "failure", "timed_out", "action_required":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("✗")
	case "skipped", "cancelled", "neutral":
		return ui.StyleMuted.Render("○")
	default:
		return ui.StyleMuted.Render("?")
	}
}

func runCILogs(cmd *cobra.Command, args []string) error {
	cfg, err := remote.Detect()
	if err != nil {
		return ui.Die(err.Error())
	}

	ownerRepo, sha, _, err := resolveCISHA(cfg, args)
	if err != nil {
		return err
	}

	runs, err := ui.SpinWithResult("Fetching CI runs...", func() ([]gh.WorkflowRun, error) {
		return gh.ListWorkflowRunsForSHA(ownerRepo, sha)
	})
	if err != nil {
		return ui.Dief("Could not fetch CI runs: %v", err)
	}

	if len(runs) == 0 {
		ui.Info("No CI runs found for this commit.")
		return nil
	}

	runs = latestRunsPerWorkflow(runs)

	// interactive = no explicit job filter; show picker instead of dumping all failed
	interactive := !flagCILogsAll && flagCILogsJob == ""

	// Collect completed jobs. In interactive mode (or --all) we want all jobs
	// so the picker can offer them; otherwise only fetch from failed runs.
	var allJobs []jobEntry
	var failedJobs []jobEntry

	for _, run := range runs {
		if run.Status != "completed" {
			continue
		}
		if !interactive && !flagCILogsAll && !isFailedConclusion(run.Conclusion) {
			continue
		}
		jobs, fetchErr := ui.SpinWithResult(
			fmt.Sprintf("Fetching jobs for '%s'...", run.Name),
			func() ([]gh.WorkflowJob, error) {
				return gh.ListWorkflowRunJobs(ownerRepo, run.ID)
			},
		)
		if fetchErr != nil {
			ui.Warnf("Could not fetch jobs for run '%s': %v", run.Name, fetchErr)
			continue
		}
		for _, job := range jobs {
			if job.Status != "completed" {
				continue
			}
			entry := jobEntry{RunName: run.Name, Job: job}
			allJobs = append(allJobs, entry)
			if isFailedConclusion(job.Conclusion) {
				failedJobs = append(failedJobs, entry)
			}
		}
	}

	if len(allJobs) == 0 {
		ui.Info("No completed CI jobs found for this commit.")
		return nil
	}

	// Resolve target jobs and line count.
	lines := flagCILogsLines

	var targetJobs []jobEntry

	if interactive {
		targetJobs, lines, err = pickCILogs(cmd, allJobs, failedJobs, lines)
		if err != nil {
			return err
		}
		if targetJobs == nil {
			return nil // cancelled
		}
	} else {
		for _, entry := range allJobs {
			if flagCILogsJob != "" && !strings.Contains(entry.Job.Name, flagCILogsJob) {
				continue
			}
			if !flagCILogsAll && !isFailedConclusion(entry.Job.Conclusion) {
				continue
			}
			targetJobs = append(targetJobs, entry)
		}
	}

	if len(targetJobs) == 0 {
		if flagCILogsJob != "" {
			ui.Infof("No jobs matching '%s'.", flagCILogsJob)
		} else {
			ui.Success("No failed jobs.")
		}
		return nil
	}

	if flagCILogsWeb {
		return openURL(targetJobs[0].Job.HTMLURL)
	}

	return renderCILogs(ownerRepo, targetJobs, lines)
}

// pickCILogs presents an interactive picker for which jobs to view and how
// many lines to show. Returns (nil, _, nil) when the user cancels.
func pickCILogs(cmd *cobra.Command, allJobs, failedJobs []jobEntry, defaultLines int) ([]jobEntry, int, error) {
	const pickAllFailed = -1
	const pickAllJobs = -2

	// Build job picker options.
	var opts []huh.Option[int]
	if len(failedJobs) > 0 {
		opts = append(opts, huh.NewOption(
			fmt.Sprintf("All failed jobs (%d)", len(failedJobs)),
			pickAllFailed,
		))
	}
	opts = append(opts, huh.NewOption("All jobs", pickAllJobs))

	maxRunLen := 0
	for _, e := range allJobs {
		if l := len([]rune(e.RunName)); l > maxRunLen {
			maxRunLen = l
		}
	}
	for i, entry := range allJobs {
		runLabel := ui.StyleMuted.Render(ui.PadRight(entry.RunName, maxRunLen) + " /")
		label := runLabel + " " + entry.Job.Name + "  " + workflowJobIcon(entry.Job)
		opts = append(opts, huh.NewOption(label, i))
	}

	jobChoice := pickAllFailed
	if len(failedJobs) == 0 {
		jobChoice = pickAllJobs
	}

	jobSelect := huh.NewSelect[int]().
		Title("Select logs to view:").
		Options(opts...).
		Value(&jobChoice).
		Height(ui.SelectHeight(len(opts)))

	linesChoice := defaultLines
	linesSelect := huh.NewSelect[int]().
		Title("How many lines to show per job?").
		Options(
			huh.NewOption("Last 50 lines", 50),
			huh.NewOption("Last 100 lines", 100),
			huh.NewOption("Last 200 lines", 200),
			huh.NewOption("All lines", 0),
		).
		Value(&linesChoice).
		Height(4)

	formGroups := []*huh.Group{huh.NewGroup(jobSelect)}
	if !cmd.Flags().Changed("lines") {
		formGroups = append(formGroups, huh.NewGroup(linesSelect))
	}

	if err := huh.NewForm(formGroups...).WithShowHelp(true).Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			ui.Info("Cancelled.")
			return nil, 0, nil
		}
		return nil, 0, err
	}

	var target []jobEntry
	switch jobChoice {
	case pickAllFailed:
		target = failedJobs
	case pickAllJobs:
		target = allJobs
	default:
		target = []jobEntry{allJobs[jobChoice]}
	}
	return target, linesChoice, nil
}

func renderCILogs(ownerRepo string, targetJobs []jobEntry, lines int) error {
	for i, entry := range targetJobs {
		label := entry.RunName + " / " + entry.Job.Name
		fmt.Fprintln(os.Stderr, logSeparator(label))

		logs, fetchErr := ui.SpinWithResult(
			fmt.Sprintf("Fetching logs for '%s'...", entry.Job.Name),
			func() (string, error) {
				return gh.GetJobLogs(ownerRepo, entry.Job.ID)
			},
		)
		if fetchErr != nil {
			ui.Warnf("Could not fetch logs: %v", fetchErr)
			continue
		}

		processed := processLogLines(logs, flagCILogsTimestamps, lines)
		if lines > 0 && len(processed) == lines {
			fmt.Fprintf(os.Stderr, "%s\n", ui.StyleMuted.Render(fmt.Sprintf("(last %d lines)", lines)))
		}
		fmt.Fprintln(os.Stderr, strings.Join(processed, "\n"))

		if i < len(targetJobs)-1 {
			fmt.Fprintln(os.Stderr)
		}
	}

	noun := "job"
	if len(targetJobs) > 1 {
		noun = "jobs"
	}
	label := fmt.Sprintf("%d failed %s", len(targetJobs), noun)
	if flagCILogsAll {
		label = fmt.Sprintf("%d %s", len(targetJobs), noun)
	}
	fmt.Fprintln(os.Stderr, logSeparator(label))
	return nil
}
