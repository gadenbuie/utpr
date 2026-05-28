package cmd

import (
	"fmt"
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
	flagViewIssue    string
	flagViewState    string
	flagViewComments string
)

func init() {
	viewCmd.Flags().BoolVarP(&flagViewWeb, "web", "w", false, "Open in the browser")
	viewCmd.Flags().BoolVar(&flagViewSummary, "summary", false, "Show brief summary (no comments)")
	viewCmd.Flags().StringVar(&flagViewIssue, "issue", "", "View an issue instead of a PR (optionally specify number)")
	viewCmd.Flags().Lookup("issue").NoOptDefVal = " "
	viewCmd.Flags().StringVar(&flagViewState, "state", "open", "Filter picker by state: open, closed, merged, all")
	viewCmd.Flags().StringVar(&flagViewComments, "comments", "regular", "Comment display mode: regular (default), reviews (unresolved review comments), all (all comments including resolved)")
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

	if flagViewSummary {
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
	case "regular", "reviews", "all":
	default:
		return ui.Dief("Invalid --comments value: '%s' (expected: regular, reviews, all)", flagViewComments)
	}

	pr, err := gh.GetPR(ownerRepo, prNumber)
	if err != nil {
		return ui.Dief("Failed to fetch PR #%d.", prNumber)
	}

	if flagViewSummary {
		return renderPRSummary(pr)
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

	return renderPRWithComments(pr, comments, reviewComments)
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
		prs, err := ui.SpinWithResult(fmt.Sprintf("Getting %s %s...", flagViewState, label), func() ([]gh.PRInfo, error) {
			return gh.ListPRs(ownerRepo, apiState)
		})
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
	rendered, err := ui.RenderMarkdown(md)
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

func renderPRSummary(pr *gh.PRInfo) error {
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
	md := fmt.Sprintf("# #%d %s\n\n**Author:** %s · **State:** %s%s\n\n%s",
		pr.Number, pr.Title, pr.User.Login, state,
		formatLabelNames(labelNames), body)
	rendered, err := ui.RenderMarkdown(md)
	if err != nil {
		return err
	}
	fmt.Print(rendered)
	return nil
}

func renderPRWithComments(pr *gh.PRInfo, comments []gh.Comment, reviewComments []gh.ReviewComment) error {
	if err := renderPRSummary(pr); err != nil {
		return err
	}
	if err := renderComments(comments); err != nil {
		return err
	}
	return renderReviewComments(reviewComments)
}

func renderComments(comments []gh.Comment) error {
	for _, c := range comments {
		date := c.CreatedAt
		if len(date) >= 10 {
			date = date[:10]
		}
		commentMD := fmt.Sprintf("---\n**@%s** commented on %s:\n\n%s",
			c.Author.Login, date, c.Body)
		rendered, err := ui.RenderMarkdown(commentMD)
		if err != nil {
			return err
		}
		fmt.Print(rendered)
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

	for _, rootID := range rootOrder {
		t := threads[rootID]

		root := t.root
		date := root.CreatedAt
		if len(date) >= 10 {
			date = date[:10]
		}
		md := fmt.Sprintf("---\n**@%s** reviewed %s %s:\n\n%s",
			root.Author.Login, reviewCommentLocation(root), reviewCommentAtDate(root, date), root.Body)
		rendered, err := ui.RenderMarkdown(md)
		if err != nil {
			return err
		}
		fmt.Print(rendered)

		for _, reply := range t.replies {
			date := reply.CreatedAt
			if len(date) >= 10 {
				date = date[:10]
			}
			md := fmt.Sprintf("↳ **@%s** replied on %s:\n\n%s",
				reply.Author.Login, date, reply.Body)
			rendered, err := ui.RenderMarkdown(md)
			if err != nil {
				return err
			}
			lines := strings.Split(rendered, "\n")
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
