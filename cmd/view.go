package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var viewCmd = &cobra.Command{
	Use:   "view [pr-or-issue-number]",
	Short: "View PR details and comments",
	Long:  "View a PR or issue. Shows full details with comments by default.",
	RunE:  runView,
}

var (
	flagViewWeb      bool
	flagViewSummary  bool
	flagViewAgent    bool
	flagViewIssue    string
	flagViewState    string
	flagViewComments string
)

func init() {
	viewCmd.Flags().BoolVarP(&flagViewWeb, "web", "w", false, "Open in the browser")
	viewCmd.Flags().BoolVar(&flagViewSummary, "summary", false, "Show brief summary (no comments)")
	viewCmd.Flags().BoolVar(&flagViewAgent, "agent", false, "Show raw Markdown output for agent consumption")
	viewCmd.Flags().StringVar(&flagViewIssue, "issue", "", "View an issue instead of a PR (optionally specify number)")
	viewCmd.Flags().Lookup("issue").NoOptDefVal = " "
	viewCmd.Flags().StringVar(&flagViewState, "state", "open", "Filter picker by state: open, closed, merged, all")
	viewCmd.Flags().StringVar(&flagViewComments, "comments", "reviews", "Comment display mode: reviews (default, unresolved review comments), regular (issue comments only), all (all comments including resolved), none (no comments), only/only-reviews (unresolved review comments only), only-regular (regular comments only)")
	viewCmd.Flags().Lookup("comments").NoOptDefVal = "reviews"
}

func runView(cmd *cobra.Command, args []string) error {
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

	viewType := "pr"
	viewTypeExplicit := cmd.Flags().Changed("issue")
	if viewTypeExplicit {
		viewType = "issue"
	}

	var numberArg string
	if len(args) > 0 {
		numberArg = args[0]
	}
	// --issue=N case
	if flagViewIssue != "" {
		if _, err := strconv.Atoi(flagViewIssue); err == nil {
			numberArg = flagViewIssue
		}
	}

	// Auto-detect PR vs issue when given a number.
	// We cache the fetched issue to avoid a double API call when viewType == "issue".
	var cachedIssue *gh.IssueInfo
	if numberArg != "" && !viewTypeExplicit {
		n, convErr := strconv.Atoi(numberArg)
		if convErr != nil {
			return ui.Dief("Invalid number: %s", numberArg)
		}
		issue, err := gh.GetIssue(ownerRepo, n)
		if err != nil {
			return ui.Dief("Could not find issue or PR #%s.", numberArg)
		}
		if issue.PullRequest != nil {
			viewType = "pr"
		} else {
			viewType = "issue"
			cachedIssue = issue
		}
	}

	if viewType == "issue" {
		return viewIssue(ownerRepo, numberArg, cachedIssue)
	}
	return viewPR(ownerRepo, numberArg, cfg)
}

func viewIssue(ownerRepo, numberArg string, cachedIssue *gh.IssueInfo) error {
	if commentOnlyMode() != "" {
		return ui.Die("--comments only-* is only valid for pull requests")
	}

	// Validate state
	switch flagViewState {
	case "open", "closed", "all":
	case "merged":
		return ui.Die("--state merged is not valid for issues (expected: open, closed, all)")
	default:
		return ui.Dief("Invalid --state value: '%s' (expected: open, closed, all)", flagViewState)
	}

	var issueNumber int
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

	if flagViewWeb {
		return openURL(fmt.Sprintf("https://github.com/%s/issues/%d", ownerRepo, issueNumber))
	}

	// Use cached issue from auto-detect if available
	issue := cachedIssue
	if issue == nil || issue.Number != issueNumber {
		var err error
		issue, err = gh.GetIssue(ownerRepo, issueNumber)
		if err != nil {
			return ui.Dief("Failed to fetch issue #%d.", issueNumber)
		}
	}

	if flagViewSummary || flagViewComments == "none" {
		return renderIssueSummary(issue)
	}

	comments, err := gh.ListIssueComments(ownerRepo, issueNumber)
	if err != nil {
		return ui.Dief("Failed to fetch comments for issue #%d.", issueNumber)
	}
	return renderIssueWithComments(issue, comments)
}

func viewPR(ownerRepo, numberArg string, cfg *remote.Config) error {
	switch flagViewState {
	case "open", "closed", "merged", "all":
	default:
		return ui.Dief("Invalid --state value: '%s' (expected: open, closed, merged, all)", flagViewState)
	}

	var prNumber int
	if numberArg != "" {
		n, err := strconv.Atoi(numberArg)
		if err != nil {
			return ui.Dief("Invalid PR number: %s", numberArg)
		}
		prNumber = n
	} else {
		onDefault, _ := git.IsOnBranch(cfg.DefaultBranch)
		if !onDefault && flagViewState == "open" {
			branch, err := git.GetCurrentBranch()
			if err == nil {
				if prURL := git.GetBranchPRURL(branch); prURL != "" {
					prNumber = prNumberFromURL(prURL)
				}
				if prNumber == 0 {
					lookup := branch
					if ref := git.GetBranchMergeRef(branch); ref != "" && ref != branch {
						lookup = ref
					}
					pr, err := gh.GetPRForBranch(ownerRepo, lookup, "open")
					if err == nil && pr != nil {
						prNumber = pr.Number
					}
				}
			}
		}
		if prNumber == 0 {
			n, err := pickForView(ownerRepo, "pr")
			if err != nil {
				return err
			}
			if n == 0 {
				return nil
			}
			prNumber = n
		}
	}

	if flagViewWeb {
		return openURL(fmt.Sprintf("https://github.com/%s/pull/%d", ownerRepo, prNumber))
	}

	switch flagViewComments {
	case "regular", "reviews", "all", "none", "only", "only-reviews", "only-regular":
	default:
		return ui.Dief("Invalid --comments value: '%s' (expected: reviews, regular, all, none, only, only-reviews, only-regular)", flagViewComments)
	}

	pr, err := gh.GetPR(ownerRepo, prNumber)
	if err != nil {
		return ui.Dief("Failed to fetch PR #%d.", prNumber)
	}

	if onlyMode := commentOnlyMode(); onlyMode != "" {
		if flagViewSummary {
			return ui.Die("--summary cannot be combined with --comments only-*")
		}

		switch onlyMode {
		case "reviews":
			reviewComments, err := gh.ListUnresolvedPRReviewComments(ownerRepo, prNumber)
			if err != nil {
				return ui.Dief("Failed to fetch review comments for PR #%d.", prNumber)
			}
			return renderPRWithOnlyReviewComments(pr, reviewComments)
		case "regular":
			comments, err := gh.ListIssueComments(ownerRepo, prNumber)
			if err != nil {
				return ui.Dief("Failed to fetch comments for PR #%d.", prNumber)
			}
			return renderPRWithOnlyRegularComments(pr, comments)
		}
	}

	reviews, err := gh.ListPRReviews(ownerRepo, prNumber)
	if err != nil {
		return ui.Dief("Failed to fetch reviews for PR #%d.", prNumber)
	}

	if flagViewSummary || flagViewComments == "none" {
		return renderPRSummary(pr, reviews)
	}

	comments, err := gh.ListIssueComments(ownerRepo, prNumber)
	if err != nil {
		return ui.Dief("Failed to fetch comments for PR #%d.", prNumber)
	}

	var reviewComments []gh.ReviewComment
	switch flagViewComments {
	case "reviews":
		reviewComments, err = gh.ListUnresolvedPRReviewComments(ownerRepo, prNumber)
		if err != nil {
			return ui.Dief("Failed to fetch review comments for PR #%d.", prNumber)
		}
	case "all":
		reviewComments, err = gh.ListPRReviewComments(ownerRepo, prNumber)
		if err != nil {
			return ui.Dief("Failed to fetch review comments for PR #%d.", prNumber)
		}
	}

	return renderPRWithComments(pr, reviews, comments, reviewComments)
}

// pickForView lists issues or PRs and lets the user pick one.
// entity is "issue" or "pr".
func pickForView(ownerRepo, entity string) (int, error) {
	label := "issues"
	if entity == "pr" {
		label = "PRs"
	}

	// Map "merged" to "closed" for the API (REST doesn't support "merged" state).
	apiState := flagViewState
	if apiState == "merged" {
		apiState = "closed"
	}

	currentUser, _ := gh.GetLogin()
	showState := flagViewState != "open"

	var items []ui.PRPickerItem
	if entity == "issue" {
		issues, err := ui.SpinWithResult(fmt.Sprintf("Getting %s %s...", flagViewState, label), func() ([]gh.IssueInfo, error) {
			return gh.ListIssues(ownerRepo, apiState)
		})
		if err != nil {
			return 0, ui.Dief("Failed to list %s.", label)
		}
		if len(issues) == 0 {
			return 0, ui.Dief("No %s %s found.", flagViewState, label)
		}
		for _, issue := range issues {
			items = append(items, ui.PRPickerItem{
				Number:      issue.Number,
				Title:       issue.Title,
				Author:      issue.User.Login,
				State:       strings.ToLower(issue.State),
				IsHighlight: currentUser != "" && issue.User.Login == currentUser,
			})
		}
	} else {
		var prs []gh.PRInfo
		var err error
		if flagViewAgent {
			prs, err = gh.ListPRs(ownerRepo, apiState)
		} else {
			prs, err = ui.SpinWithResult(fmt.Sprintf("Getting %s %s...", flagViewState, label), func() ([]gh.PRInfo, error) {
				return gh.ListPRs(ownerRepo, apiState)
			})
		}
		if err != nil {
			return 0, ui.Dief("Failed to list %s.", label)
		}
		// Filter for merged if requested
		if flagViewState == "merged" {
			var filtered []gh.PRInfo
			for _, pr := range prs {
				if pr.Merged {
					filtered = append(filtered, pr)
				}
			}
			prs = filtered
		}
		if len(prs) == 0 {
			return 0, ui.Dief("No %s %s found.", flagViewState, label)
		}
		if flagViewAgent {
			fmt.Fprint(os.Stdout, formatViewAgentPRChoices(prs))
			return 0, fmt.Errorf("a PR number is required")
		}
		for _, pr := range prs {
			state := strings.ToLower(pr.State)
			if pr.Merged {
				state = "merged"
			}
			items = append(items, ui.PRPickerItem{
				Number:      pr.Number,
				Title:       pr.Title,
				Author:      pr.User.Login,
				State:       state,
				IsHighlight: currentUser != "" && pr.User.Login == currentUser,
			})
		}
	}

	mode := ui.PickerDefault
	if showState {
		mode = ui.PickerWithState
	}
	opts := ui.FormatPRPickerOptions(items, mode)

	header := fmt.Sprintf("Select %s to view:", addArticle(entity))
	selected, err := ui.ChooseWithOptions(header, opts)
	if err != nil {
		ui.Info("Cancelled.")
		return 0, nil
	}
	return selected, nil
}

func formatViewAgentPRChoices(prs []gh.PRInfo) string {
	var b strings.Builder
	b.WriteString("Multiple PRs found. Choose one by rerunning with its number:\n")
	for _, pr := range prs {
		state := strings.ToLower(pr.State)
		if pr.Merged {
			state = "merged"
		}
		fmt.Fprintf(&b, "#%d\t%s\t%s\t@%s\n", pr.Number, state, pr.Title, pr.User.Login)
	}
	return b.String()
}

func prNumberFromURL(url string) int {
	url = strings.TrimRight(url, "/")
	idx := strings.LastIndex(url, "/")
	if idx < 0 {
		return 0
	}
	n, err := strconv.Atoi(url[idx+1:])
	if err != nil {
		return 0
	}
	return n
}

func addArticle(entity string) string {
	if entity == "issue" {
		return "an issue"
	}
	return "a PR"
}

func formatLabelNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return " · **Labels:** " + strings.Join(names, ", ")
}

// commentOnlyMode maps the shorthand "only" to unresolved review comments.
// It returns "reviews", "regular", or "".
func commentOnlyMode() string {
	switch flagViewComments {
	case "only", "only-reviews":
		return "reviews"
	case "only-regular":
		return "regular"
	default:
		return ""
	}
}

// renderViewMarkdown returns raw Markdown for agent consumers and terminal-
// formatted Markdown for interactive users.
func renderViewMarkdown(content string) (string, error) {
	if flagViewAgent {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		return content, nil
	}
	return ui.RenderMarkdown(content)
}

func renderIssueSummary(issue *gh.IssueInfo) error {
	body := issue.Body
	if body == "" {
		body = "*No description provided.*"
	}
	var labelNames []string
	for _, l := range issue.Labels {
		labelNames = append(labelNames, l.Name)
	}
	md := fmt.Sprintf("# #%d %s\n\n**Author:** %s · **State:** %s%s\n\n%s",
		issue.Number, issue.Title, issue.User.Login, issue.State,
		formatLabelNames(labelNames), body)
	rendered, err := renderViewMarkdown(md)
	if err != nil {
		return err
	}
	fmt.Print(rendered)
	return nil
}

func renderIssueWithComments(issue *gh.IssueInfo, comments []gh.Comment) error {
	if err := renderIssueSummary(issue); err != nil {
		return err
	}
	return renderComments(comments)
}

func renderPRSummary(pr *gh.PRInfo, reviews []gh.PRReview) error {
	body := pr.Body
	if body == "" {
		body = "*No description provided.*"
	}
	var labelNames []string
	for _, l := range pr.Labels {
		labelNames = append(labelNames, l.Name)
	}
	state := pr.State
	if pr.Merged {
		state = "merged"
	}
	reviewSection := formatReviewSection(reviews)
	md := fmt.Sprintf("# #%d %s\n\n**Author:** %s · **State:** %s%s%s\n\n%s",
		pr.Number, pr.Title, pr.User.Login, state,
		formatLabelNames(labelNames), reviewSection, body)
	rendered, err := renderViewMarkdown(md)
	if err != nil {
		return err
	}
	fmt.Print(rendered)
	return nil
}

func renderPRCommentHeader(pr *gh.PRInfo) error {
	header := fmt.Sprintf("# #%d %s", pr.Number, pr.Title)
	rendered, err := renderViewMarkdown(header)
	if err != nil {
		return err
	}
	fmt.Print(rendered)
	return nil
}

func renderPRWithOnlyReviewComments(pr *gh.PRInfo, reviewComments []gh.ReviewComment) error {
	if err := renderPRCommentHeader(pr); err != nil {
		return err
	}
	return renderReviewComments(reviewComments)
}

func renderPRWithOnlyRegularComments(pr *gh.PRInfo, comments []gh.Comment) error {
	if err := renderPRCommentHeader(pr); err != nil {
		return err
	}
	return renderComments(comments)
}

func renderPRWithComments(pr *gh.PRInfo, reviews []gh.PRReview, comments []gh.Comment, reviewComments []gh.ReviewComment) error {
	if err := renderPRSummary(pr, reviews); err != nil {
		return err
	}
	if err := renderComments(comments); err != nil {
		return err
	}
	return renderReviewComments(reviewComments)
}

// formatReviewSection returns a markdown snippet for the PR's latest reviews per reviewer.
// Returns "" if there are no actionable reviews.
func formatReviewSection(reviews []gh.PRReview) string {
	if len(reviews) == 0 {
		return ""
	}
	type reviewEntry struct {
		login       string
		state       string
		association string
		date        string
	}
	order := []string{}
	latest := map[string]reviewEntry{}
	for _, r := range reviews {
		if r.State != "APPROVED" && r.State != "CHANGES_REQUESTED" {
			continue
		}
		login := r.User.Login
		if _, seen := latest[login]; !seen {
			order = append(order, login)
		}
		date := r.SubmittedAt
		if len(date) >= 10 {
			date = date[:10]
		}
		latest[login] = reviewEntry{
			login:       login,
			state:       r.State,
			association: formatAssociation(r.AuthorAssociation),
			date:        date,
		}
	}
	if len(order) == 0 {
		return ""
	}
	var lines []string
	for _, login := range order {
		e := latest[login]
		icon := "✓"
		stateLabel := "approved"
		if e.state == "CHANGES_REQUESTED" {
			icon = "✗"
			stateLabel = "changes requested"
		}
		assoc := ""
		if e.association != "" {
			assoc = " (" + e.association + ")"
		}
		lines = append(lines, fmt.Sprintf("- %s %s %s%s · %s", icon, login, stateLabel, assoc, e.date))
	}
	return "\n\n**Reviews**\n" + strings.Join(lines, "\n")
}

func formatAssociation(a string) string {
	switch a {
	case "OWNER":
		return "Owner"
	case "MEMBER":
		return "Member"
	case "COLLABORATOR":
		return "Collaborator"
	case "CONTRIBUTOR":
		return "Contributor"
	case "FIRST_TIME_CONTRIBUTOR":
		return "First-time contributor"
	case "FIRST_TIMER":
		return "First-timer"
	default:
		return ""
	}
}

func renderComments(comments []gh.Comment) error {
	showIDs := !ui.IsStdoutTTY()
	for _, c := range comments {
		date := c.CreatedAt
		if len(date) >= 10 {
			date = date[:10]
		}
		header, err := renderViewMarkdown(fmt.Sprintf("---\n**@%s** commented on %s:", c.Author.Login, date))
		if err != nil {
			return err
		}
		fmt.Print(header)
		if showIDs {
			fmt.Printf("%s<!-- comment_id:%d -->\n", renderedIndent(header), c.ID)
		}
		body, err := renderViewMarkdown(c.Body)
		if err != nil {
			return err
		}
		fmt.Print(body)
	}
	return nil
}

func reviewCommentLocation(c gh.ReviewComment) string {
	line := c.OriginalLine
	if c.Line != nil {
		line = *c.Line
	}
	return fmt.Sprintf("`%s:%d`", c.Path, line)
}

func reviewCommentAtDate(c gh.ReviewComment, date string) string {
	if c.Line == nil && len(c.CommitID) >= 7 {
		return fmt.Sprintf("at %s on %s", c.CommitID[:7], date)
	}
	return "on " + date
}

func renderReviewComments(comments []gh.ReviewComment) error {
	if len(comments) == 0 {
		return nil
	}

	type thread struct {
		root    gh.ReviewComment
		replies []gh.ReviewComment
	}

	var rootOrder []int
	threads := map[int]*thread{}
	for _, c := range comments {
		if c.InReplyToID == 0 {
			rootOrder = append(rootOrder, c.ID)
			threads[c.ID] = &thread{root: c}
		}
	}
	for _, c := range comments {
		if c.InReplyToID != 0 {
			if t, ok := threads[c.InReplyToID]; ok {
				t.replies = append(t.replies, c)
			}
		}
	}

	showIDs := !ui.IsStdoutTTY()
	for _, rootID := range rootOrder {
		t := threads[rootID]

		root := t.root
		date := root.CreatedAt
		if len(date) >= 10 {
			date = date[:10]
		}
		header, err := renderViewMarkdown(fmt.Sprintf("---\n**@%s** reviewed %s %s:",
			root.Author.Login, reviewCommentLocation(root), reviewCommentAtDate(root, date)))
		if err != nil {
			return err
		}
		fmt.Print(header)
		if showIDs {
			threadID := root.ThreadID
			if threadID == 0 {
				threadID = root.ID
			}
			fmt.Printf("%s<!-- thread_id:%d comment_id:%d -->\n", renderedIndent(header), threadID, root.ID)
		}
		body, err := renderViewMarkdown(root.Body)
		if err != nil {
			return err
		}
		fmt.Print(body)

		for _, reply := range t.replies {
			date := reply.CreatedAt
			if len(date) >= 10 {
				date = date[:10]
			}
			replyHeader, err := renderViewMarkdown(fmt.Sprintf("↳ **@%s** replied on %s:", reply.Author.Login, date))
			if err != nil {
				return err
			}
			lines := strings.Split(replyHeader, "\n")
			for i, line := range lines {
				if line != "" {
					lines[i] = "  " + line
				}
			}
			fmt.Print(strings.Join(lines, "\n"))
			if showIDs {
				fmt.Printf("%s<!-- comment_id:%d -->\n", "  "+renderedIndent(replyHeader), reply.ID)
			}
			replyBody, err := renderViewMarkdown(reply.Body)
			if err != nil {
				return err
			}
			lines = strings.Split(replyBody, "\n")
			for i, line := range lines {
				if line != "" {
					lines[i] = "  " + line
				}
			}
			fmt.Print(strings.Join(lines, "\n"))
		}
	}
	return nil
}

// renderedIndent returns the leading whitespace of the last non-empty line in
// a glamour-rendered string, so HTML comment annotations can match the indent.
func renderedIndent(rendered string) string {
	lines := strings.Split(rendered, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if strings.TrimSpace(line) != "" {
			return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		}
	}
	return ""
}
