package cmd

import (
	"errors"
	"fmt"
	"io"
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
	"golang.org/x/term"
)

var ciCmd = &cobra.Command{
	Use:   "ci [#pr | @branch | number | branch | ref]",
	Short: "View CI check status",
	Long: `Show CI check run status for the current branch or a specific PR, branch, or ref.

  utpr ci           current branch
  utpr ci 123       PR #123
  utpr ci #123      PR #123 (explicit)
  utpr ci main      branch 'main'
  utpr ci @main     branch 'main' (explicit)
  utpr ci HEAD~2    2 commits back from HEAD
  utpr ci abc123    commit ref (local SHA)`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCI,
}

var ciLogsCmd = &cobra.Command{
	Use:   "logs [#pr | @branch | number | branch | ref]",
	Short: "Show logs for failed CI jobs",
	Long:  "Show the last N lines of logs for failed CI jobs. Accepts the same target forms as 'utpr ci'.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runCILogs,
}

var (
	flagCIWeb    bool
	flagCIWatch  bool
	flagCIFailed bool
	flagCIWait   string
	flagCIPick   bool
	flagCIAgent  bool
)

// pickRunsLimit is the number of runs fetched for --pick.
const pickRunsLimit = 20

// autoPickRunsLimit is the number of runs fetched for the automatic
// "no checks found" fallback picker.
const autoPickRunsLimit = 10

var (
	flagCILogsWeb        bool
	flagCILogsLines      int
	flagCILogsTimestamps bool
	flagCILogsAll        bool
	flagCILogsFailed     bool
	flagCILogsJob        string
	flagCILogsPick       bool
	flagCILogsAgent      bool
)

func init() {
	ciCmd.Flags().BoolVarP(&flagCIWeb, "web", "w", false, "Open checks in the browser")
	ciCmd.Flags().BoolVar(&flagCIWatch, "watch", false, "Poll until all checks complete, with live status display")
	ciCmd.Flags().BoolVar(&flagCIFailed, "failed", false, "Show only failed checks")
	ciCmd.Flags().StringVar(&flagCIWait, "wait", "", `Wait for checks: "all" (all complete) or "failed" (stop on first failure); exits 0 on success, 1 on failure`)
	ciCmd.Flags().Lookup("wait").NoOptDefVal = "all"
	ciCmd.Flags().BoolVar(&flagCIPick, "pick", false, fmt.Sprintf("Pick from the last %d CI runs on the branch", pickRunsLimit))
	ciCmd.Flags().BoolVar(&flagCIAgent, "agent", false, "Show unstyled output for agent consumption")

	ciLogsCmd.Flags().BoolVarP(&flagCILogsWeb, "web", "w", false, "Open failed job in the browser")
	ciLogsCmd.Flags().IntVarP(&flagCILogsLines, "lines", "n", 100, "Number of log lines to show per job (0 = all)")
	ciLogsCmd.Flags().BoolVar(&flagCILogsTimestamps, "timestamps", false, "Show timestamps on log lines")
	ciLogsCmd.Flags().BoolVar(&flagCILogsAll, "all", false, "Show logs for all jobs, not just failed")
	ciLogsCmd.Flags().BoolVar(&flagCILogsFailed, "failed", false, "Show logs for all failed jobs without prompting")
	ciLogsCmd.Flags().StringVar(&flagCILogsJob, "job", "", "Show logs for a specific job by name (substring match)")
	ciLogsCmd.Flags().BoolVar(&flagCILogsPick, "pick", false, fmt.Sprintf("Pick from the last %d CI runs on the branch", pickRunsLimit))
	ciLogsCmd.Flags().BoolVar(&flagCILogsAgent, "agent", false, "Show unstyled logs for agent consumption")

	ciCmd.AddCommand(ciLogsCmd)
}

// ciTarget describes a resolved CI target: the commit to show checks for,
// and the owner/repo + branch to use when listing runs to pick from.
type ciTarget struct {
	ownerRepo     string
	sha           string
	prURL         string
	pickOwnerRepo string
	pickBranch    string
}

// gitRefRe matches bare SHA-like refs (short or full commit hashes). 4 is
// git's minimum unambiguous abbreviation length.
var gitRefRe = regexp.MustCompile(`^[0-9a-fA-F]{4,40}$`)

// looksLikeGitRef reports whether arg looks like a local git ref (a commit
// SHA, or a relative ref like HEAD~2 or HEAD^) rather than a branch name.
func looksLikeGitRef(arg string) bool {
	if gitRefRe.MatchString(arg) {
		return true
	}
	if arg == "HEAD" || strings.HasPrefix(arg, "HEAD~") || strings.HasPrefix(arg, "HEAD^") {
		return true
	}
	return strings.ContainsAny(arg, "~^")
}

// resolveCITarget returns the CI target for the current branch or args[0].
// If args[0] is a PR number, uses that PR's head SHA. If it looks like a
// local git ref (e.g. HEAD~2 or a commit SHA), resolves it locally, falling
// back to a remote branch lookup if that fails. Otherwise uses the local
// HEAD SHA.
func resolveCITarget(cfg *remote.Config, args []string, pick bool) (ciTarget, error) {
	sourceURL, runErr := git.Run("remote", "get-url", cfg.SourceRemote)
	if runErr != nil {
		return ciTarget{}, ui.Dief("Could not determine remote URL for '%s'.", cfg.SourceRemote)
	}
	ownerRepo, err := remote.ParseRepoSpec(sourceURL)
	if err != nil {
		return ciTarget{}, ui.Dief("Could not parse repository from remote URL: %s", sourceURL)
	}

	if len(args) > 0 {
		arg := args[0]

		// #123    → explicit PR number
		// @main   → explicit branch name
		// 123     → PR number (numeric)
		// HEAD~2  → local git ref
		// abc123  → local git ref (SHA), falling back to branch name
		// main    → branch name
		switch {
		case strings.HasPrefix(arg, "#"):
			n, convErr := strconv.Atoi(arg[1:])
			if convErr != nil {
				return ciTarget{}, ui.Dief("Invalid PR number: %s", arg)
			}
			pr, prErr := gh.GetPR(ownerRepo, n)
			if prErr != nil {
				return ciTarget{}, ui.Dief("Could not fetch PR #%d.", n)
			}
			return ciTarget{ownerRepo, pr.Head.SHA, pr.HTMLURL, pr.Head.Repo.FullName, pr.Head.Ref}, nil

		case strings.HasPrefix(arg, "@"):
			branch := arg[1:]
			sha, shaErr := gh.GetBranchSHA(ownerRepo, branch)
			if shaErr != nil {
				return ciTarget{}, ui.Dief("Could not find branch '%s'.", branch)
			}
			return ciTarget{ownerRepo, sha, "", ownerRepo, branch}, nil

		default:
			if n, convErr := strconv.Atoi(arg); convErr == nil {
				pr, prErr := gh.GetPR(ownerRepo, n)
				if prErr != nil {
					return ciTarget{}, ui.Dief("Could not fetch PR #%d.", n)
				}
				return ciTarget{ownerRepo, pr.Head.SHA, pr.HTMLURL, pr.Head.Repo.FullName, pr.Head.Ref}, nil
			}

			currentBranch, _ := git.GetCurrentBranch()

			if looksLikeGitRef(arg) {
				if sha, revErr := git.RevParse(arg); revErr == nil {
					return ciTarget{ownerRepo, sha, "", ownerRepo, currentBranch}, nil
				}
			}

			sha, shaErr := gh.GetBranchSHA(ownerRepo, arg)
			if shaErr != nil {
				return ciTarget{}, ui.Dief("Could not resolve '%s' as a commit, ref, or branch.", arg)
			}
			return ciTarget{ownerRepo, sha, "", ownerRepo, arg}, nil
		}
	}

	sha, err := git.RevParse("HEAD")
	if err != nil {
		return ciTarget{}, ui.Die("Could not determine HEAD commit.")
	}

	tracking := git.GetTrackingBranch()
	if tracking == "" {
		return ciTarget{}, ui.Die("Branch has no remote tracking branch. Push first with 'utpr push'.")
	}

	remoteSHA, revErr := git.RevParse("@{u}")
	if revErr != nil {
		return ciTarget{}, ui.Die("Could not determine remote branch HEAD.")
	}

	if sha != remoteSHA {
		if !pick {
			printCIInfo(flagCIAgent, "Showing CI for the last pushed commit (HEAD has unpushed changes).")
		}
		sha = remoteSHA
	}

	currentBranch, _ := git.GetCurrentBranch()
	return ciTarget{ownerRepo, sha, "", ownerRepo, currentBranch}, nil
}

// errNoChecksFound is returned by showCIChecks when no check runs exist for the SHA.
var errNoChecksFound = errors.New("no checks found")

func runCI(cmd *cobra.Command, args []string) error {
	cfg, err := remote.Detect()
	if err != nil {
		return ui.Die(err.Error())
	}

	target, err := resolveCITarget(cfg, args, flagCIPick)
	if err != nil {
		return err
	}
	ownerRepo, sha, prURL := target.ownerRepo, target.sha, target.prURL

	if flagCIPick {
		picked, pickErr := pickRunForBranch(target.pickOwnerRepo, target.pickBranch, pickRunsLimit)
		if pickErr != nil {
			return pickErr
		}
		if picked == nil {
			return nil // cancelled, or no runs found (message already printed)
		}
		ownerRepo, sha, prURL = target.pickOwnerRepo, picked.HeadSHA, ""
	}

	if flagCIWeb {
		if prURL != "" {
			return openURL(prURL + "/checks")
		}
		return openURL(fmt.Sprintf("https://github.com/%s/commit/%s/checks", ownerRepo, sha))
	}

	if flagCIWatch || flagCIWait != "" {
		if flagCIWait != "" && flagCIWait != "all" && flagCIWait != "failed" {
			return ui.Dief(`--wait must be "all" or "failed"`)
		}
		mode := flagCIWait
		if mode == "" {
			mode = "all"
		}
		return waitCI(ownerRepo, sha, mode, flagCIWatch)
	}

	err = showCIChecks(ownerRepo, sha)
	if errors.Is(err, errNoChecksFound) {
		// Only offer the automatic picker fallback when the user didn't
		// already pick a specific target (no args, no explicit --pick).
		if len(args) == 0 && !flagCIPick {
			picked, pickErr := pickRunForBranch(target.pickOwnerRepo, target.pickBranch, autoPickRunsLimit)
			if pickErr != nil {
				return pickErr
			}
			if picked != nil {
				return showCIChecks(target.pickOwnerRepo, picked.HeadSHA)
			}
		}
		printCIInfo(flagCIAgent, "No checks found for this commit.")
		return nil
	}
	return err
}

func printCIInfo(agent bool, msg string) {
	if agent {
		fmt.Fprintln(os.Stdout, msg)
		return
	}
	ui.Info(msg)
}

func printCIInfof(agent bool, format string, args ...any) {
	if agent {
		fmt.Fprintf(os.Stdout, format+"\n", args...)
		return
	}
	ui.Infof(format, args...)
}

func printCISuccess(agent bool, msg string) {
	if agent {
		fmt.Fprintln(os.Stdout, msg)
		return
	}
	ui.Success(msg)
}

// showCIChecks fetches and renders check runs for a commit SHA. Returns
// errNoChecksFound (wrapped) if no check runs exist for the commit.
func showCIChecks(ownerRepo, sha string) error {
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
		return errNoChecksFound
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
			printCISuccess(flagCIAgent, "No failed checks.")
			return nil
		}
		runs = failed
	}

	if flagCIAgent {
		fmt.Fprint(os.Stdout, renderCheckRunsPlain(runs, buildSuiteNameMap(data.workflowRuns), false))
	} else {
		renderCheckRuns(os.Stderr, runs, buildSuiteNameMap(data.workflowRuns), false)
	}
	return nil
}

// pickRunForBranch fetches the most recent workflow runs for a branch and
// prompts the user to pick one. Returns (nil, nil) if the user cancels or
// no runs are found (an informational message is printed in that case).
func pickRunForBranch(ownerRepo, branch string, limit int) (*gh.WorkflowRun, error) {
	if branch == "" {
		printCIInfo(flagCIAgent, "Could not determine a branch to list CI runs for.")
		return nil, nil
	}

	runs, err := ui.SpinWithResult(fmt.Sprintf("Fetching CI runs for '%s'...", branch), func() ([]gh.WorkflowRun, error) {
		return gh.ListWorkflowRunsForBranch(ownerRepo, branch, limit)
	})
	if err != nil {
		return nil, ui.Dief("Could not fetch CI runs: %v", err)
	}
	if len(runs) == 0 {
		printCIInfof(flagCIAgent, "No CI runs found for branch '%s'.", branch)
		return nil, nil
	}

	return pickWorkflowRun(runs)
}

// pickWorkflowRun presents an interactive picker over workflow runs, newest
// first. Returns (nil, nil) if the user cancels.
func pickWorkflowRun(runs []gh.WorkflowRun) (*gh.WorkflowRun, error) {
	var opts []huh.Option[int]
	maxNameLen, maxAgoLen := 0, 0
	agos := make([]string, len(runs))
	for i, r := range runs {
		if l := len([]rune(r.Name)); l > maxNameLen {
			maxNameLen = l
		}
		agos[i] = formatRelativeTime(r.CreatedAt)
		if l := len([]rune(agos[i])); l > maxAgoLen {
			maxAgoLen = l
		}
	}
	for i, r := range runs {
		label := fmt.Sprintf("%s  %s  %s  #%d",
			statusIcon(r.Status, r.Conclusion),
			ui.PadRight(r.Name, maxNameLen),
			ui.StyleMuted.Render(ui.PadRight(agos[i], maxAgoLen)),
			r.RunNumber)
		opts = append(opts, huh.NewOption(label, i))
	}

	choice := 0
	sel := huh.NewSelect[int]().
		Title("Select a CI run:").
		Options(opts...).
		Value(&choice).
		Height(ui.SelectHeight(len(opts)))

	if err := huh.NewForm(huh.NewGroup(sel)).WithShowHelp(true).Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			ui.Info("Cancelled.")
			return nil, nil
		}
		return nil, err
	}

	return &runs[choice], nil
}

// formatRelativeTime formats an RFC3339 timestamp as a short "time ago" string,
// rounded to the two largest applicable units (e.g. "2d 3h ago", "5m 12s ago").
// Once the timestamp is 7 or more days old, it shows an absolute timestamp instead.
func formatRelativeTime(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return ""
	}
	return formatRelativeTimeAt(t, time.Now())
}

// formatRelativeTimeAt formats t relative to now, per formatRelativeTime's rules.
func formatRelativeTimeAt(t, now time.Time) string {
	d := now.Sub(t).Round(time.Second)
	if d >= 7*24*time.Hour {
		return t.Local().Format("2006-01-02 15:04")
	}

	days := int(d / (24 * time.Hour))
	hours := int(d/time.Hour) % 24
	minutes := int(d/time.Minute) % 60
	seconds := int(d/time.Second) % 60

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh ago", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm ago", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm %ds ago", minutes, seconds)
	default:
		return fmt.Sprintf("%ds ago", seconds)
	}
}

// countTerminalRows returns the number of terminal rows that output will
// occupy at the given terminal width, accounting for line wrapping.
func countTerminalRows(output string, termWidth int) int {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if termWidth <= 0 {
		return len(lines)
	}
	rows := 0
	for _, line := range lines {
		dw := len([]rune(ui.StripANSI(line)))
		if dw == 0 {
			rows++
		} else {
			rows += (dw + termWidth - 1) / termWidth
		}
	}
	return rows
}

func waitCI(ownerRepo, sha, mode string, fullDisplay bool) error {
	isInteractive := !flagCIAgent && term.IsTerminal(int(os.Stderr.Fd()))
	var prevLines int       // for fullDisplay mode
	var lastRendered string // for compact interactive: last full rendered msg (with ANSI)
	var lastStatus string   // for compact non-interactive: last stripped status (without timestamp)

	clearLine := func() {
		if isInteractive && lastRendered != "" {
			width := len([]rune(ui.StripANSI(lastRendered)))
			fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", width))
			lastRendered = ""
		}
	}

	for {
		checkRuns, err := gh.GetCheckRuns(ownerRepo, sha)
		if err != nil {
			clearLine()
			return ui.Dief("Could not fetch CI status: %v", err)
		}

		if len(checkRuns) == 0 {
			time.Sleep(10 * time.Second)
			continue
		}

		var running, passing, failing, skipped int
		for _, r := range checkRuns {
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

		anyFailed := failing > 0
		allDone := running == 0

		if fullDisplay {
			wfRuns, _ := gh.ListWorkflowRunsForSHA(ownerRepo, sha) // best-effort
			var buf strings.Builder
			renderCheckRuns(&buf, checkRuns, buildSuiteNameMap(wfRuns), true)
			output := buf.String()
			if flagCIAgent {
				fmt.Fprint(os.Stdout, ui.StripANSI(output))
			} else {
				if prevLines > 0 {
					fmt.Fprintf(os.Stderr, "\033[%dA\033[J", prevLines)
				}
				fmt.Fprint(os.Stderr, output)
				prevLines = countTerminalRows(output, ui.GetTermWidth())
			}
		} else {
			var parts []string
			if running > 0 {
				parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render(fmt.Sprintf("%d running", running)))
			}
			if passing > 0 {
				parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(fmt.Sprintf("%d passing", passing)))
			}
			if failing > 0 {
				parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(fmt.Sprintf("%d failing", failing)))
			}
			if skipped > 0 {
				parts = append(parts, ui.StyleMuted.Render(fmt.Sprintf("%d skipped", skipped)))
			}
			statusMsg := "Waiting for CI: " + strings.Join(parts, " · ")
			ts := time.Now().Format("15:04:05")

			if isInteractive {
				msg := fmt.Sprintf("[%s] %s", ts, statusMsg)
				clearLine()
				fmt.Fprint(os.Stderr, msg)
				lastRendered = msg
			} else {
				stripped := ui.StripANSI(statusMsg)
				if stripped != lastStatus {
					if flagCIAgent {
						fmt.Fprintf(os.Stdout, "[%s] %s\n", ts, stripped)
					} else {
						fmt.Fprintf(os.Stderr, "[%s] %s\n", ts, statusMsg)
					}
					lastStatus = stripped
				}
			}
		}

		shouldStop := allDone || (mode == "failed" && anyFailed)
		if !shouldStop {
			time.Sleep(10 * time.Second)
			continue
		}

		clearLine()
		summary := checkRunSummary(checkRuns)
		if anyFailed {
			if flagCIAgent {
				fmt.Fprintln(os.Stdout, ui.StripANSI(summary))
			} else {
				ui.Error(summary)
			}
			return fmt.Errorf("CI checks failed")
		}
		if flagCIAgent {
			fmt.Fprintln(os.Stdout, ui.StripANSI(summary))
		} else {
			ui.Success(summary)
		}
		return nil
	}
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

// statusIcon returns a colored icon for a check/job/run status+conclusion pair.
func statusIcon(status, conclusion string) string {
	if status != "completed" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render("…")
	}
	switch conclusion {
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

func checkRunIcon(r gh.CheckRun) string {
	return statusIcon(r.Status, r.Conclusion)
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

func renderCheckRuns(w io.Writer, runs []gh.CheckRun, suiteNames map[int64]string, showTimestamp bool) {
	if showTimestamp {
		ts := time.Now().Format("15:04:05")
		_, _ = fmt.Fprintf(w, "%s\n\n", ui.StyleMuted.Render("Checking CI... (updated "+ts+")"))
	}

	groups := groupCheckRuns(runs, suiteNames)
	groupHeaderStyle := lipgloss.NewStyle().Bold(true)
	bulletStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("4"))

	for _, g := range groups {
		_, _ = fmt.Fprintf(w, "%s %s\n",
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
			_, _ = fmt.Fprintf(w, "  %s  %s  %s\n",
				icon,
				ui.PadRight(name, maxLen),
				dur)
		}
		_, _ = fmt.Fprintln(w)
	}

	_, _ = fmt.Fprintf(w, "%s\n", checkRunSummary(runs))
}

func renderCheckRunsPlain(runs []gh.CheckRun, suiteNames map[int64]string, showTimestamp bool) string {
	var buf strings.Builder
	renderCheckRuns(&buf, runs, suiteNames, showTimestamp)
	return ui.StripANSI(buf.String())
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
	return statusIcon(job.Status, job.Conclusion)
}

func runCILogs(cmd *cobra.Command, args []string) error {
	cfg, err := remote.Detect()
	if err != nil {
		return ui.Die(err.Error())
	}

	target, err := resolveCITarget(cfg, args, flagCILogsPick)
	if err != nil {
		return err
	}
	ownerRepo, sha := target.ownerRepo, target.sha

	if flagCILogsPick {
		picked, pickErr := pickRunForBranch(target.pickOwnerRepo, target.pickBranch, pickRunsLimit)
		if pickErr != nil {
			return pickErr
		}
		if picked == nil {
			return nil // cancelled, or no runs found (message already printed)
		}
		ownerRepo, sha = target.pickOwnerRepo, picked.HeadSHA
	}

	runs, err := ui.SpinWithResult("Fetching CI runs...", func() ([]gh.WorkflowRun, error) {
		return gh.ListWorkflowRunsForSHA(ownerRepo, sha)
	})
	if err != nil {
		return ui.Dief("Could not fetch CI runs: %v", err)
	}

	if len(runs) == 0 {
		printCIInfo(flagCILogsAgent, "No CI runs found for this commit.")
		return nil
	}

	runs = latestRunsPerWorkflow(runs)

	// interactive = no explicit job filter; show picker instead of dumping all failed
	interactive := !flagCILogsAll && !flagCILogsFailed && flagCILogsJob == ""

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
		printCIInfo(flagCILogsAgent, "No completed CI jobs found for this commit.")
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
			printCIInfof(flagCILogsAgent, "No jobs matching '%s'.", flagCILogsJob)
		} else {
			printCISuccess(flagCILogsAgent, "No failed jobs.")
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
		if flagCILogsAgent {
			fmt.Fprintf(os.Stdout, "## %s\n", label)
		} else {
			fmt.Fprintln(os.Stderr, logSeparator(label))
		}

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
		if flagCILogsAgent {
			for i := range processed {
				processed[i] = ui.StripANSI(processed[i])
			}
		}
		if lines > 0 && len(processed) == lines {
			if flagCILogsAgent {
				fmt.Fprintf(os.Stdout, "(last %d lines)\n", lines)
			} else {
				fmt.Fprintf(os.Stderr, "%s\n", ui.StyleMuted.Render(fmt.Sprintf("(last %d lines)", lines)))
			}
		}
		if flagCILogsAgent {
			fmt.Fprintln(os.Stdout, strings.Join(processed, "\n"))
		} else {
			fmt.Fprintln(os.Stderr, strings.Join(processed, "\n"))
		}

		if !flagCILogsAgent && i < len(targetJobs)-1 {
			fmt.Fprintln(os.Stderr)
		}
	}

	if flagCILogsAgent {
		return nil
	}

	noun := "job"
	if len(targetJobs) > 1 {
		noun = "jobs"
	}
	allFailed := !flagCILogsAll
	for _, e := range targetJobs {
		if !isFailedConclusion(e.Job.Conclusion) {
			allFailed = false
			break
		}
	}
	label := fmt.Sprintf("%d %s", len(targetJobs), noun)
	if allFailed {
		label = fmt.Sprintf("%d failed %s", len(targetJobs), noun)
	}
	fmt.Fprintln(os.Stderr, logSeparator(label))
	return nil
}
