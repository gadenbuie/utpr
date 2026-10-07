package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status [pr-or-issue-number]",
	Short: "Show quick status of a PR or issue",
	Long: `Show a quick status summary of a PR or issue as a table: state, CI checks,
reviews, unresolved review threads, and whether the local branch is in sync
with the remote head. Titles, authors, branches, and checks link to GitHub
in a rich terminal.

Give a number to target a specific PR or issue (the type is detected
automatically). Without a number, defaults to the current branch's PR.
Use --json for machine-readable output.`,
	RunE: runStatus,
}

var (
	flagStatusJSON  bool
	flagStatusIssue string
)

func init() {
	statusCmd.Flags().BoolVar(&flagStatusJSON, "json", false, "Output status as JSON")
	statusCmd.Flags().StringVar(&flagStatusIssue, "issue", "", "Get status for an issue instead of a PR (optionally specify number)")
	statusCmd.Flags().Lookup("issue").NoOptDefVal = " "
	// Shares flagViewState with the view command; pickForView uses it as the
	// picker state filter.
	statusCmd.Flags().StringVar(&flagViewState, "state", "open", "Filter picker by state: open, closed, merged, all")
}

func runStatus(cmd *cobra.Command, args []string) error {
	_, err := remote.Detect()
	if err != nil {
		return ui.Die(err.Error())
	}
	cfg := remote.Require()

	sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote)
	if err != nil {
		return ui.Dief("Could not determine remote URL for '%s'.", cfg.SourceRemote)
	}
	ownerRepo, err := remote.ParseRepoSpec(sourceURL)
	if err != nil {
		return ui.Dief("Could not parse repository from remote URL: %s", sourceURL)
	}

	numberArg := parseNumberArg(args, flagStatusIssue)

	explicitIssue := cmd.Flags().Changed("issue")
	viewType := "pr"
	if explicitIssue {
		viewType = "issue"
	}

	// Auto-detect PR vs issue when given a number.
	var cachedIssue *gh.IssueInfo
	if numberArg != "" && !explicitIssue {
		isPR, issue, err := detectPRorIssue(ownerRepo, numberArg)
		if err != nil {
			return err
		}
		if isPR {
			viewType = "pr"
		} else {
			viewType = "issue"
			cachedIssue = issue
		}
	}

	// Validate state and dispatch.
	if err := validateViewState(viewType, flagViewState); err != nil {
		return err
	}

	if viewType == "issue" {
		return issueStatusReport(ownerRepo, numberArg, cachedIssue)
	}
	return prStatusReport(ownerRepo, numberArg, cfg)
}

// statusCheck is a single CI check run in a status report.
type statusCheck struct {
	Name       string `json:"name"`
	Status     string `json:"status"`               // queued, in_progress, completed
	Conclusion string `json:"conclusion,omitempty"` // success, failure, ...
	Duration   string `json:"duration,omitempty"`   // e.g. "4m 12s"
	URL        string `json:"url,omitempty"`
}

// statusChecks summarizes the CI checks reported for a PR head.
type statusChecks struct {
	Overall string        `json:"overall"` // success, failure, pending, none, unknown
	Total   int           `json:"total"`
	Passed  int           `json:"passed"`
	Failed  int           `json:"failed"`
	Pending int           `json:"pending"`
	Skipped int           `json:"skipped"` // skipped, cancelled, neutral
	Items   []statusCheck `json:"items"`
}

// statusReview is the latest review from one reviewer.
type statusReview struct {
	Reviewer    string `json:"reviewer"`
	State       string `json:"state"` // APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED
	SubmittedAt string `json:"submitted_at"`
}

// statusReviews summarizes the reviews on a PR.
type statusReviews struct {
	Summary      string         `json:"summary"`                 // approved, changes_requested, none, unknown
	Approved     int            `json:"approved"`                // reviewers whose latest review approves
	Total        int            `json:"total"`                   // reviewers with a review or a pending request
	Items        []statusReview `json:"items"`                   // latest review per reviewer
	Pending      []string       `json:"pending"`                 // requested users who have not reviewed
	PendingTeams []string       `json:"pending_teams,omitempty"` // requested teams that have not reviewed
}

// statusLocalSync compares a local branch with the PR's remote head.
type statusLocalSync struct {
	Branch   string `json:"branch"`
	Status   string `json:"status"` // in_sync, ahead, behind, diverged, unknown
	AheadBy  int    `json:"ahead_by"`
	BehindBy int    `json:"behind_by"`
}

// prStatus is the status report for a pull request.
type prStatus struct {
	Type              string           `json:"type"`
	Number            int              `json:"number"`
	Title             string           `json:"title"`
	URL               string           `json:"url"`
	State             string           `json:"state"`
	Draft             bool             `json:"draft"`
	Merged            bool             `json:"merged"`
	Author            string           `json:"author"`
	HeadBranch        string           `json:"head_branch"`
	HeadRepo          string           `json:"head_repo"`
	HeadSHA           string           `json:"head_sha"`
	BaseBranch        string           `json:"base_branch"`
	BaseRepo          string           `json:"base_repo"`
	MergeableState    string           `json:"mergeable_state,omitempty"`
	Checks            statusChecks     `json:"checks"`
	Reviews           statusReviews    `json:"reviews"`
	UnresolvedThreads int              `json:"unresolved_threads"` // -1 when unavailable
	LocalSync         *statusLocalSync `json:"local_sync,omitempty"`
}

// issueStatus is the status report for an issue.
type issueStatus struct {
	Type      string   `json:"type"`
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	URL       string   `json:"url"`
	Repo      string   `json:"repo"`
	State     string   `json:"state"`
	Author    string   `json:"author"`
	CreatedAt string   `json:"created_at,omitempty"`
	Labels    []string `json:"labels"`
	Assignees []string `json:"assignees"`
	Comments  int      `json:"comments"`
}

// statusRow is one row in the status table. Continuation rows (individual
// checks, reviewers) leave the label empty and carry their content in the
// value column. tone selects the value color in styled output: good, bad,
// warn, info, muted, or "" for the default foreground.
type statusRow struct {
	label string
	value string
	tone  string
}

func prStatusReport(ownerRepo, numberArg string, cfg *remote.Config) error {
	prNumber, err := resolvePRNumber(ownerRepo, numberArg, cfg)
	if err != nil {
		return err
	}
	if prNumber == 0 {
		return nil
	}

	// ui.Spin is skipped in plain mode, so piped or agent output stays clean.
	var status *prStatus
	spinErr := ui.Spin(fmt.Sprintf("Fetching status of PR #%d...", prNumber), func() error {
		pr, err := gh.GetPR(ownerRepo, prNumber)
		if err != nil {
			return err
		}
		status = buildPRStatus(ownerRepo, pr, cfg)
		return nil
	})
	if spinErr != nil {
		return ui.Dief("Failed to fetch PR #%d.", prNumber)
	}

	if flagStatusJSON {
		return printStatusJSON(status)
	}
	renderPRStatus(status, ownerRepo)
	return nil
}

func issueStatusReport(ownerRepo, numberArg string, cachedIssue *gh.IssueInfo) error {
	issueNumber := 0
	if numberArg != "" {
		n, err := strconv.Atoi(numberArg)
		if err != nil {
			return ui.Dief("Invalid issue number: %s", numberArg)
		}
		issueNumber = n
	} else {
		n, err := pickForView(ownerRepo, "issue")
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		issueNumber = n
	}

	issue := cachedIssue
	if issue == nil || issue.Number != issueNumber {
		spinErr := ui.Spin(fmt.Sprintf("Fetching status of issue #%d...", issueNumber), func() error {
			fetched, err := gh.GetIssue(ownerRepo, issueNumber)
			if err != nil {
				return err
			}
			issue = fetched
			return nil
		})
		if spinErr != nil {
			return ui.Dief("Failed to fetch issue #%d.", issueNumber)
		}
	}
	if issue.PullRequest != nil {
		return ui.Dief("#%d is a pull request; drop --issue to see its status.", issueNumber)
	}

	status := buildIssueStatus(issue, ownerRepo)

	if flagStatusJSON {
		return printStatusJSON(status)
	}
	renderIssueStatus(status)
	return nil
}

// buildPRStatus assembles the status report for a pull request, fetching CI
// checks, reviews, and unresolved review threads. Failures in those optional
// sections degrade to "unknown" markers rather than failing the report.
func buildPRStatus(ownerRepo string, pr *gh.PRInfo, cfg *remote.Config) *prStatus {
	s := &prStatus{
		Type:              "pull_request",
		Number:            pr.Number,
		Title:             pr.Title,
		URL:               pr.HTMLURL,
		State:             pr.State,
		Draft:             pr.Draft,
		Merged:            pr.Merged,
		Author:            pr.User.Login,
		HeadBranch:        pr.Head.Ref,
		HeadRepo:          pr.Head.Repo.FullName,
		HeadSHA:           pr.Head.SHA,
		BaseBranch:        pr.Base.Ref,
		BaseRepo:          pr.Base.Repo.FullName,
		MergeableState:    pr.MergeableState,
		UnresolvedThreads: -1,
	}

	if checks, err := gh.GetCheckRuns(ownerRepo, pr.Head.SHA); err == nil {
		s.Checks = summarizeChecks(checks)
	} else {
		s.Checks = statusChecks{Overall: "unknown", Items: []statusCheck{}}
	}

	if reviews, err := gh.ListPRReviews(ownerRepo, pr.Number); err == nil {
		s.Reviews = summarizeReviews(reviews, requestedReviewerLogins(pr), requestedTeamSlugs(pr))
	} else {
		s.Reviews = statusReviews{Summary: "unknown", Items: []statusReview{}, Pending: []string{}, PendingTeams: []string{}}
	}

	if threads, err := gh.ListUnresolvedPRReviewComments(ownerRepo, pr.Number); err == nil {
		s.UnresolvedThreads = countUnresolvedThreads(threads)
	}

	s.LocalSync = localSyncStatus(pr, cfg)
	return s
}

// summarizeChecks reduces check runs to counts plus an overall verdict.
func summarizeChecks(checks []gh.CheckRun) statusChecks {
	s := statusChecks{Items: make([]statusCheck, 0, len(checks))}
	for _, c := range checks {
		s.Items = append(s.Items, statusCheck{
			Name:       c.Name,
			Status:     c.Status,
			Conclusion: c.Conclusion,
			Duration:   checkDuration(c),
			URL:        c.HTMLURL,
		})
		switch {
		case c.Status != "completed":
			s.Pending++
		case c.Conclusion == "success":
			s.Passed++
		case isFailedConclusion(c.Conclusion):
			s.Failed++
		default:
			s.Skipped++
		}
	}
	s.Total = len(checks)
	switch {
	case s.Total == 0:
		s.Overall = "none"
	case s.Failed > 0:
		s.Overall = "failure"
	case s.Pending > 0:
		s.Overall = "pending"
	default:
		s.Overall = "success"
	}
	return s
}

// requestedReviewerLogins extracts the logins of reviewers requested on a PR.
func requestedReviewerLogins(pr *gh.PRInfo) []string {
	var logins []string
	for _, u := range pr.RequestedReviewers {
		logins = append(logins, u.Login)
	}
	return logins
}

// requestedTeamSlugs extracts the slugs of teams requested to review a PR.
func requestedTeamSlugs(pr *gh.PRInfo) []string {
	var slugs []string
	for _, t := range pr.RequestedTeams {
		slugs = append(slugs, t.Slug)
	}
	return slugs
}

// countUnresolvedThreads counts distinct unresolved review threads. The gh
// helper returns every comment in every unresolved thread, so thread roots
// (comments with no reply-to) are counted instead of raw comments.
func countUnresolvedThreads(comments []gh.ReviewComment) int {
	n := 0
	for _, c := range comments {
		if c.InReplyToID == 0 {
			n++
		}
	}
	return n
}

// summarizeReviews reduces reviews to the latest state per reviewer, plus
// requested reviewers who have not yet reviewed. A comment-only review does
// not replace an earlier actionable state, matching how GitHub computes
// review decisions. Changes requested take precedence in the summary.
func summarizeReviews(reviews []gh.PRReview, requestedUsers, requestedTeams []string) statusReviews {
	s := statusReviews{Items: []statusReview{}, Pending: []string{}, PendingTeams: []string{}}
	latest := map[string]statusReview{}
	var order []string
	for _, r := range reviews {
		switch r.State {
		case "APPROVED", "CHANGES_REQUESTED", "COMMENTED", "DISMISSED":
		default:
			continue // skip PENDING
		}
		login := r.User.Login
		existing, seen := latest[login]
		if seen && r.State == "COMMENTED" && existing.State != "COMMENTED" {
			// A comment-only review doesn't override an actionable state.
			continue
		}
		if _, seen := latest[login]; !seen {
			order = append(order, login)
		}
		latest[login] = statusReview{Reviewer: login, State: r.State, SubmittedAt: r.SubmittedAt}
	}
	changesRequested := false
	for _, login := range order {
		r := latest[login]
		s.Items = append(s.Items, r)
		if r.State == "APPROVED" {
			s.Approved++
		}
		if r.State == "CHANGES_REQUESTED" {
			changesRequested = true
		}
	}

	// Requested users who have not left a review yet are pending. Requested
	// teams are kept as reported: GitHub removes a team request once the team
	// reviews, and team members' reviews don't carry the team identity.
	reviewed := map[string]bool{}
	for _, r := range s.Items {
		reviewed[r.Reviewer] = true
	}
	for _, login := range requestedUsers {
		if !reviewed[login] {
			s.Pending = append(s.Pending, login)
		}
	}
	s.PendingTeams = append(s.PendingTeams, requestedTeams...)
	s.Total = len(s.Items) + len(s.Pending) + len(s.PendingTeams)

	switch {
	case changesRequested:
		s.Summary = "changes_requested"
	case s.Approved > 0:
		s.Summary = "approved"
	default:
		s.Summary = "none"
	}
	return s
}

// localSyncStatus finds the local branch for a PR (the plain head ref or
// utpr's pr/{number}-{author}-{branch} schemes) and compares it with the
// PR's remote head. Returns nil when no local branch exists.
func localSyncStatus(pr *gh.PRInfo, cfg *remote.Config) *statusLocalSync {
	local := findLocalBranchForPR(pr.Number, pr.Head.Ref, pr.User.Login, cfg.DefaultBranch, pr.Base.Ref)
	if local == "" {
		return nil
	}
	ref := "refs/heads/" + local
	ls := &statusLocalSync{Branch: local}

	localSHA, err := git.RevParse(ref)
	if err != nil {
		return nil
	}
	if localSHA == pr.Head.SHA {
		ls.Status = "in_sync"
		return ls
	}

	ahead, errA := git.RevListCount(pr.Head.SHA + ".." + ref)
	behind, errB := git.RevListCount(ref + ".." + pr.Head.SHA)
	if errA != nil || errB != nil {
		ls.Status = "unknown"
		return ls
	}
	ls.AheadBy = ahead
	ls.BehindBy = behind
	switch {
	case ahead > 0 && behind > 0:
		ls.Status = "diverged"
	case ahead > 0:
		ls.Status = "ahead"
	case behind > 0:
		ls.Status = "behind"
	default:
		// Same commit set but different SHAs (e.g. after a rebase).
		ls.Status = "in_sync"
	}
	return ls
}

// buildIssueStatus assembles the status report for an issue.
func buildIssueStatus(issue *gh.IssueInfo, ownerRepo string) *issueStatus {
	s := &issueStatus{
		Type:      "issue",
		Number:    issue.Number,
		Title:     issue.Title,
		URL:       issue.HTMLURL,
		Repo:      ownerRepo,
		State:     issue.State,
		Author:    issue.User.Login,
		CreatedAt: issue.CreatedAt,
		Labels:    []string{},
		Assignees: []string{},
		Comments:  issue.Comments,
	}
	for _, l := range issue.Labels {
		s.Labels = append(s.Labels, l.Name)
	}
	for _, a := range issue.Assignees {
		s.Assignees = append(s.Assignees, a.Login)
	}
	return s
}

// renderPRStatus prints the human-readable PR status table.
func renderPRStatus(s *prStatus, ownerRepo string) {
	rows := prStatusRows(s, ownerRepo)
	title := fmt.Sprintf("#%d %s", s.Number, s.Title)

	if ui.PlainMode() {
		fmt.Println("# " + mdLink(s.URL, title))
		fmt.Println()
		fmt.Print(mdStatusTable(rows))
		return
	}

	fmt.Println()
	fmt.Println(statusTitleStyle(stateTone(s)).Render(ui.Hyperlink(s.URL, title)))
	fmt.Println()
	fmt.Println(renderStatusTable(rows))
}

// renderIssueStatus prints the human-readable issue status table.
func renderIssueStatus(s *issueStatus) {
	rows := issueStatusRows(s)
	title := fmt.Sprintf("#%d %s", s.Number, s.Title)

	if ui.PlainMode() {
		fmt.Println("# " + mdLink(s.URL, title))
		fmt.Println()
		fmt.Print(mdStatusTable(rows))
		return
	}

	fmt.Println()
	fmt.Println(statusTitleStyle(issueStateTone(s.State)).Render(ui.Hyperlink(s.URL, title)))
	fmt.Println()
	fmt.Println(renderStatusTable(rows))
}

// toneColors maps tone names to the ANSI colors used across status output:
// good (green), bad (red), warn (yellow), info (cyan), muted (gray).
var toneColors = map[string]string{
	"good":  "2",
	"bad":   "1",
	"warn":  "3",
	"info":  "6",
	"muted": "8",
}

// statusTitleStyle styles a status title with bold text in the color used
// for the given tone, so the title matches the State row of the table.
func statusTitleStyle(tone string) lipgloss.Style {
	style := lipgloss.NewStyle().Bold(true)
	if c, ok := toneColors[tone]; ok {
		style = style.Foreground(lipgloss.Color(c))
	}
	return style
}

// issueStateTone returns the tone for an issue state.
func issueStateTone(state string) string {
	if state == "open" {
		return "info"
	}
	if state != "" {
		return "muted"
	}
	return ""
}

// prStatusRows builds the label/value rows of the PR status table.
// Individual checks and reviewers are continuation rows: the label column
// stays empty and the icon, name, and detail share the value column.
func prStatusRows(s *prStatus, ownerRepo string) []statusRow {
	rows := []statusRow{
		{"State", prStateText(s), stateTone(s)},
		{"Author", statusLink(githubUserURL(s.Author), s.Author), ""},
		{"Branch", fmt.Sprintf("%s → %s",
			statusLink(githubBranchURL(s.HeadRepo, s.HeadBranch), s.HeadBranch),
			statusLink(githubBranchURL(s.BaseRepo, s.BaseBranch), s.BaseBranch)), ""},
	}

	switch s.Checks.Overall {
	case "none":
		rows = append(rows, statusRow{"Checks", "none reported", "muted"})
	case "unknown":
		rows = append(rows, statusRow{"Checks", "unavailable", "muted"})
	default:
		rows = append(rows, statusRow{"Checks", checkCountsText(&s.Checks), checksTone(s.Checks.Overall)})
		for _, c := range s.Checks.Items {
			rows = append(rows, statusRow{"", checkLine(c), checkTone(c)})
		}
	}

	switch {
	case s.Reviews.Summary == "unknown":
		rows = append(rows, statusRow{"Reviewers", "unavailable", "muted"})
	case s.Reviews.Total == 0:
		rows = append(rows, statusRow{"Reviewers", "none", "muted"})
	default:
		rows = append(rows, statusRow{"Reviewers", reviewsSummaryText(&s.Reviews), reviewsTone(&s.Reviews)})
		for _, r := range s.Reviews.Items {
			rows = append(rows, statusRow{"", reviewLine(r), reviewTone(r.State)})
		}
		for _, login := range s.Reviews.Pending {
			rows = append(rows, statusRow{"", pendingReviewLine(login), "info"})
		}
		for _, team := range s.Reviews.PendingTeams {
			rows = append(rows, statusRow{"", pendingTeamLine(team, orgFromRepo(ownerRepo)), "info"})
		}
	}

	if s.UnresolvedThreads >= 0 {
		tone := "muted"
		if s.UnresolvedThreads > 0 {
			tone = "warn"
		}
		rows = append(rows, statusRow{"Unresolved threads", strconv.Itoa(s.UnresolvedThreads), tone})
	} else {
		rows = append(rows, statusRow{"Unresolved threads", "unavailable", "muted"})
	}

	if s.MergeableState != "" && s.MergeableState != "unknown" {
		rows = append(rows, statusRow{"Mergeable", s.MergeableState, mergeableTone(s.MergeableState)})
	}

	if s.LocalSync != nil {
		rows = append(rows, statusRow{"Local branch", localSyncText(s.LocalSync), localSyncTone(s.LocalSync.Status)})
	}

	return rows
}

// issueStatusRows builds the label/value rows of the issue status table.
func issueStatusRows(s *issueStatus) []statusRow {
	rows := []statusRow{
		{"State", s.State, issueStateTone(s.State)},
		{"Author", statusLink(githubUserURL(s.Author), s.Author), ""},
		{"Created", shortDate(s.CreatedAt), ""},
	}
	if len(s.Labels) > 0 {
		var links []string
		for _, l := range s.Labels {
			links = append(links, statusLink(githubLabelURL(s.Repo, l), l))
		}
		rows = append(rows, statusRow{"Labels", strings.Join(links, ", "), ""})
	}
	if len(s.Assignees) > 0 {
		var links []string
		for _, a := range s.Assignees {
			links = append(links, statusLink(githubUserURL(a), a))
		}
		rows = append(rows, statusRow{"Assignees", strings.Join(links, ", "), ""})
	}
	rows = append(rows, statusRow{"Comments", strconv.Itoa(s.Comments), ""})
	return rows
}

// renderStatusTable renders rows as a lipgloss table with a light border,
// muted labels, and tone-colored values.
func renderStatusTable(rows []statusRow) string {
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Padding(0, 1)
	valueStyle := lipgloss.NewStyle().Padding(0, 1)
	tones := map[string]lipgloss.Style{}
	for tone, color := range toneColors {
		tones[tone] = valueStyle.Foreground(lipgloss.Color(color))
	}
	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("8"))).
		BorderRow(false).
		StyleFunc(func(row, col int) lipgloss.Style {
			if col == 0 {
				return labelStyle
			}
			if row >= 0 && row < len(rows) {
				if s, ok := tones[rows[row].tone]; ok {
					return s
				}
			}
			return valueStyle
		})
	for _, r := range rows {
		t.Row(r.label, r.value)
	}
	return t.Render()
}

// mdStatusTable renders rows as a GitHub-flavored markdown table.
func mdStatusTable(rows []statusRow) string {
	var b strings.Builder
	b.WriteString("| Field | Value |\n")
	b.WriteString("|-------|-------|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %s |\n",
			strings.ReplaceAll(r.label, "|", "\\|"),
			strings.ReplaceAll(r.value, "|", "\\|"))
	}
	return b.String()
}

func prStateText(s *prStatus) string {
	state := s.State
	if s.Merged {
		state = "merged"
	}
	if s.Draft {
		state += " (draft)"
	}
	return state
}

// statusLink links text to a URL: an OSC 8 hyperlink in styled output, a
// markdown link in plain output.
func statusLink(url, text string) string {
	if ui.PlainMode() {
		return mdLink(url, text)
	}
	return ui.Hyperlink(url, text)
}

func mdLink(url, text string) string {
	if url == "" {
		return text
	}
	return "[" + text + "](" + url + ")"
}

func githubUserURL(login string) string {
	if login == "" {
		return ""
	}
	return "https://github.com/" + login
}

func githubBranchURL(repo, branch string) string {
	if repo == "" || branch == "" {
		return ""
	}
	return "https://github.com/" + repo + "/tree/" + branch
}

func githubLabelURL(repo, label string) string {
	if repo == "" || label == "" {
		return ""
	}
	return "https://github.com/" + repo + "/labels/" + url.PathEscape(label)
}

// orgFromRepo extracts the owner from an "owner/repo" spec.
func orgFromRepo(ownerRepo string) string {
	owner, _, _ := strings.Cut(ownerRepo, "/")
	return owner
}

func githubTeamURL(org, slug string) string {
	if org == "" || slug == "" {
		return ""
	}
	return "https://github.com/orgs/" + org + "/teams/" + slug
}

// statusCheckIcon returns the icon for a check, colored in styled output.
func statusCheckIcon(c statusCheck) string {
	if ui.PlainMode() {
		switch {
		case c.Status != "completed":
			return "…"
		case c.Conclusion == "success":
			return "✓"
		case isFailedConclusion(c.Conclusion):
			return "✗"
		default:
			return "○"
		}
	}
	return statusIcon(c.Status, c.Conclusion)
}

// statusReviewIcon returns the icon for a review, colored in styled output.
func statusReviewIcon(state string) string {
	if ui.PlainMode() {
		switch state {
		case "APPROVED":
			return "✓"
		case "CHANGES_REQUESTED":
			return "✗"
		default:
			return "○"
		}
	}
	return reviewStateIcon(state)
}

// checkResultText describes a check's outcome, e.g. "success · 1m 12s".
func checkResultText(c statusCheck) string {
	detail := checkDetail(c)
	if c.Duration != "" {
		return detail + " · " + c.Duration
	}
	return detail
}

// checkLine renders one CI check as a continuation row, e.g.
// "✓ Lint (success · 24s)".
func checkLine(c statusCheck) string {
	return statusCheckIcon(c) + " " + statusLink(c.URL, c.Name) + " (" + checkResultText(c) + ")"
}

// reviewLine renders one review as a continuation row, e.g.
// "✓ alice (approved · 2026-10-02)".
func reviewLine(r statusReview) string {
	return statusReviewIcon(r.State) + " " + statusLink(githubUserURL(r.Reviewer), r.Reviewer) +
		" (" + reviewStateLabel(r.State) + " · " + shortDate(r.SubmittedAt) + ")"
}

// pendingReviewLine renders a requested reviewer who has not reviewed yet.
func pendingReviewLine(login string) string {
	return pendingReviewIcon() + " " + statusLink(githubUserURL(login), login) + " (requested)"
}

// pendingTeamLine renders a requested team that has not reviewed yet.
func pendingTeamLine(team, org string) string {
	return pendingReviewIcon() + " " + statusLink(githubTeamURL(org, team), team) + " (requested)"
}

func pendingReviewIcon() string {
	if ui.PlainMode() {
		return "…"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render("…")
}

// reviewsSummaryText renders the reviewer rollup, e.g. "1 / 3 approved",
// leading with changes requested when present.
func reviewsSummaryText(r *statusReviews) string {
	text := fmt.Sprintf("%d / %d approved", r.Approved, r.Total)
	if r.Summary == "changes_requested" {
		return "changes requested · " + text
	}
	return text
}

// checkCountsText renders pass/fail/pending counts, e.g. "7 passed, 1 failed".
func checkCountsText(c *statusChecks) string {
	var parts []string
	if c.Passed > 0 {
		parts = append(parts, fmt.Sprintf("%d passed", c.Passed))
	}
	if c.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", c.Failed))
	}
	if c.Pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", c.Pending))
	}
	if c.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", c.Skipped))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d checks", c.Total)
	}
	return strings.Join(parts, ", ")
}

// checkDetail describes a check's state for the check list.
func checkDetail(c statusCheck) string {
	if c.Status != "completed" {
		return strings.ReplaceAll(c.Status, "_", " ") // queued, in progress
	}
	return c.Conclusion
}

// checkDuration formats a check's run time, e.g. "1m 30s". Running checks
// report the time elapsed since they started.
func checkDuration(c gh.CheckRun) string {
	if c.StartedAt == "" {
		return ""
	}
	started, err := time.Parse(time.RFC3339, c.StartedAt)
	if err != nil {
		return ""
	}
	var d time.Duration
	if c.Status != "completed" || c.CompletedAt == "" {
		d = time.Since(started)
	} else {
		completed, err := time.Parse(time.RFC3339, c.CompletedAt)
		if err != nil {
			return ""
		}
		d = completed.Sub(started)
	}
	return ciFormatDuration(d)
}

func reviewStateIcon(state string) string {
	switch state {
	case "APPROVED":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("✓")
	case "CHANGES_REQUESTED":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("✗")
	default:
		return ui.StyleMuted.Render("○")
	}
}

func reviewStateLabel(state string) string {
	switch state {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "changes requested"
	case "COMMENTED":
		return "commented"
	case "DISMISSED":
		return "dismissed"
	default:
		return state
	}
}

// Tone helpers pick the value color in styled output: good (green), bad
// (red), warn (yellow), info (cyan), muted (gray), or "" for default.

func stateTone(s *prStatus) string {
	switch {
	case s.Merged:
		return "good"
	case s.State == "open" && s.Draft:
		return "warn"
	case s.State == "open":
		return "info"
	default:
		return "bad"
	}
}

func checksTone(overall string) string {
	switch overall {
	case "success":
		return "good"
	case "failure":
		return "bad"
	case "pending":
		return "info"
	default:
		return "muted"
	}
}

func checkTone(c statusCheck) string {
	switch {
	case c.Status != "completed":
		return "info"
	case c.Conclusion == "success":
		return "good"
	case isFailedConclusion(c.Conclusion):
		return "bad"
	default:
		return "muted"
	}
}

func reviewsTone(r *statusReviews) string {
	switch {
	case r.Summary == "changes_requested":
		return "bad"
	case r.Approved > 0 && r.Approved == r.Total:
		return "good"
	default:
		return ""
	}
}

func reviewTone(state string) string {
	switch state {
	case "APPROVED":
		return "good"
	case "CHANGES_REQUESTED":
		return "bad"
	default:
		return "muted"
	}
}

func mergeableTone(state string) string {
	switch state {
	case "clean":
		return "good"
	case "behind", "unstable", "has_hooks":
		return "warn"
	case "dirty", "blocked":
		return "bad"
	default:
		return ""
	}
}

func localSyncTone(status string) string {
	switch status {
	case "in_sync":
		return "good"
	case "ahead", "behind":
		return "warn"
	case "diverged":
		return "bad"
	default:
		return "muted"
	}
}

// localSyncText describes the local-vs-remote sync state of a branch,
// e.g. "up to date" or "2 commits behind".
func localSyncText(ls *statusLocalSync) string {
	switch ls.Status {
	case "in_sync":
		return "up to date"
	case "ahead":
		return pluralCommits(ls.AheadBy) + " ahead"
	case "behind":
		return pluralCommits(ls.BehindBy) + " behind"
	case "diverged":
		return fmt.Sprintf("diverged: %s ahead, %s behind", pluralCommits(ls.AheadBy), pluralCommits(ls.BehindBy))
	default:
		return "unknown"
	}
}

func pluralCommits(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}

func shortDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// printStatusJSON writes the status report as indented JSON to stdout.
func printStatusJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
