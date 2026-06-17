package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [branch | issue-number]",
	Short: "Create a new PR branch",
	Long:  "Create a new PR branch. Can start from a GitHub issue or a branch name.",
	RunE:  runInit,
}

var (
	flagInitWorktree bool
	flagInitBase     string
)

func init() {
	initCmd.Flags().BoolVar(&flagInitWorktree, "worktree", false, "Create branch in a new git worktree")
	initCmd.Flags().StringVar(&flagInitBase, "base", "", "Base branch or ref to create from")
}

func runInit(cmd *cobra.Command, args []string) error {
	var branch string
	var issueNumber int

	for _, arg := range args {
		if strings.HasPrefix(arg, "#") {
			n, err := strconv.Atoi(arg[1:])
			if err == nil {
				issueNumber = n
				continue
			}
		}
		n, err := strconv.Atoi(arg)
		if err == nil {
			issueNumber = n
			continue
		}
		branch = arg
	}

	cfg, err := remote.Detect()
	if err != nil {
		return ui.Die(err.Error())
	}

	// If no args, show issue picker (or ask for branch name if offline)
	if branch == "" && issueNumber == 0 {
		if !gh.IsReachable() {
			ui.Warn("No network connection. Enter a branch name to create.")
			branch, err = ui.Input("Branch name:", "", "branch name")
			if err != nil || branch == "" {
				return ui.Die("Branch name required.")
			}
		} else {
			issueNumber, err = pickIssue()
			if err != nil {
				return err
			}
			if issueNumber == 0 {
				ui.Info("Cancelled.")
				return nil
			}
		}
	}

	// Look up issue and prompt for branch name
	if issueNumber > 0 {
		branch, err = handleIssue(issueNumber, branch)
		if err != nil {
			return err
		}
	}

	if branch == "" {
		return ui.Die("Branch name required.")
	}

	if err := git.ValidateBranchName(branch); err != nil {
		return ui.Die(err.Error())
	}

	// If branch already exists, handle accordingly
	if git.BranchExists(branch) {
		wtPath := git.GetBranchWorktreePath(branch)
		if wtPath != "" {
			ui.Infof("Branch '%s' already has a worktree.", branch)
			offerWorktreeNavigation(wtPath)
			return nil
		}
		if flagInitWorktree {
			ui.Infof("Creating worktree for existing branch '%s'.", branch)
			return initWorktree(branch)
		}
		ui.Infof("Branch '%s' already exists locally. Resuming.", branch)
		return runResume(resumeCmd, []string{branch})
	}

	// Default base to current branch
	baseRef := flagInitBase
	if baseRef == "" {
		baseRef, err = git.GetCurrentBranch()
		if err != nil {
			return err
		}
	}

	// Fetch and update the base ref
	if err := fetchAndUpdateBase(baseRef, cfg); err != nil {
		ui.Warnf("Could not update base ref '%s': %v", baseRef, err)
	}

	if flagInitWorktree {
		if _, err := git.Run("branch", branch, baseRef); err != nil {
			return ui.Dief("Failed to create branch '%s' from '%s'.", branch, baseRef)
		}
		_ = git.MarkBranchCreatedByUtpr(branch)
		ui.Successf("Created branch '%s' from '%s'.", branch, baseRef)
		return initWorktree(branch)
	}

	if _, err := git.Run("switch", "-c", branch, baseRef); err != nil {
		return ui.Dief("Failed to create branch '%s' from '%s'.", branch, baseRef)
	}
	_ = git.MarkBranchCreatedByUtpr(branch)
	ui.Successf("Created branch '%s' from '%s'.", branch, baseRef)
	return nil
}

func pickIssue() (int, error) {
	cfg := remote.Require()
	sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote)
	if err != nil {
		return 0, ui.Die("Could not determine remote URL.")
	}
	ownerRepo, err := remote.ParseRepoSpec(sourceURL)
	if err != nil {
		return 0, ui.Die("Could not parse repository from remote URL.")
	}

	issues, err := ui.SpinWithResult("Getting open issues...", func() ([]gh.IssueInfo, error) {
		return gh.ListIssues(ownerRepo, "open")
	})
	if err != nil {
		return 0, ui.Die("Failed to list issues.")
	}
	if len(issues) == 0 {
		return 0, ui.Die("No open issues found. Provide a branch name to proceed.")
	}

	currentUser, _ := gh.GetLogin()
	items := make([]ui.PRPickerItem, 0, len(issues))
	for _, issue := range issues {
		var assignees []string
		for _, a := range issue.Assignees {
			assignees = append(assignees, a.Login)
		}
		var labels []string
		for _, l := range issue.Labels {
			labels = append(labels, l.Name)
		}
		isAssigned := false
		if currentUser != "" {
			for _, a := range assignees {
				if a == currentUser {
					isAssigned = true
					break
				}
			}
		}
		items = append(items, ui.PRPickerItem{
			Number:      issue.Number,
			Title:       issue.Title,
			Author:      issue.User.Login,
			Assignees:   strings.Join(assignees, ","),
			Labels:      strings.Join(labels, ", "),
			IsHighlight: isAssigned,
		})
	}

	opts := ui.FormatPRPickerOptions(items, ui.PickerIssue)
	selected, err := ui.ChooseWithOptions("Select an issue to work on:", opts)
	if err != nil {
		return 0, nil
	}
	return selected, nil
}

func handleIssue(issueNumber int, existingBranch string) (string, error) {
	cfg := remote.Require()
	sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote)
	if err != nil {
		return "", ui.Dief("Could not determine remote URL for '%s'.", cfg.SourceRemote)
	}
	ownerRepo, err := remote.ParseRepoSpec(sourceURL)
	if err != nil {
		return "", ui.Dief("Could not parse repository from remote URL: %s", sourceURL)
	}

	issue, err := gh.GetIssue(ownerRepo, issueNumber)
	if err != nil {
		return "", ui.Dief("Failed to fetch issue #%d.", issueNumber)
	}

	ui.Infof("Issue #%d: %s", issueNumber, issue.Title)

	if err := previewIssue(issue); err != nil {
		return "", err
	}

	if existingBranch == "" {
		suggested := issueToSlug(issueNumber, issue.Title)
		existingBranch, err = ui.Input("Branch name:", suggested, "branch name")
		if err != nil {
			return "", err
		}
		if existingBranch == "" {
			return "", ui.Die("Branch name required.")
		}
	}

	login, _ := gh.GetLogin()
	if login != "" {
		isAssigned := false
		for _, a := range issue.Assignees {
			if a.Login == login {
				isAssigned = true
				break
			}
		}
		if isAssigned {
			ui.Infof("You are already assigned to issue #%d.", issueNumber)
		} else {
			confirmed, err := ui.Confirm(fmt.Sprintf("Assign yourself to issue #%d?", issueNumber), true)
			if err != nil {
				return "", err
			}
			if confirmed {
				if err := gh.AddIssueAssignee(ownerRepo, issueNumber, login); err != nil {
					ui.Warnf("Could not assign you to issue #%d.", issueNumber)
				} else {
					ui.Successf("You were assigned to issue #%d.", issueNumber)
				}
			}
		}
	}

	return existingBranch, nil
}

func previewIssue(issue *gh.IssueInfo) error {
	confirmed, err := ui.Confirm(fmt.Sprintf("Preview issue #%d?", issue.Number), false)
	if err != nil {
		return err
	}
	if !confirmed {
		return nil
	}

	createdDate := issue.CreatedAt
	if len(createdDate) >= 10 {
		createdDate = createdDate[:10]
	}
	body := issue.Body
	if body == "" {
		body = "*No description provided.*"
	}

	md := fmt.Sprintf("# #%d %s\n\n**State:** %s · **Author:** @%s · **Created:** %s\n\n%s\n",
		issue.Number, issue.Title, issue.State, issue.User.Login, createdDate, body)

	rendered, err := ui.RenderMarkdown(md)
	if err != nil {
		fmt.Fprint(os.Stderr, md)
		return nil
	}

	_ = ui.Pager(rendered)
	return nil
}

func issueToSlug(number int, title string) string {
	re := regexp.MustCompile(`[^a-z0-9]+`)
	slug := re.ReplaceAllString(strings.ToLower(title), "-")
	slug = strings.Trim(slug, "-")
	return fmt.Sprintf("fix/%d-%s", number, slug)
}

func fetchAndUpdateBase(baseRef string, cfg *remote.Config) error {
	if strings.Contains(baseRef, "/") {
		parts := strings.SplitN(baseRef, "/", 2)
		return ui.Spin(fmt.Sprintf("Fetching %s...", baseRef), func() error {
			return git.Fetch(parts[0], parts[1])
		})
	}

	upstream, _ := git.Run("rev-parse", "--abbrev-ref", baseRef+"@{upstream}")
	if upstream == "" {
		return nil
	}

	parts := strings.SplitN(upstream, "/", 2)
	if len(parts) != 2 {
		return nil
	}

	err := ui.Spin(fmt.Sprintf("Fetching latest %s...", baseRef), func() error {
		return git.Fetch(parts[0], parts[1])
	})
	if err != nil {
		return err
	}

	current, _ := git.GetCurrentBranch()
	if current == baseRef {
		return ui.Spin(fmt.Sprintf("Pulling %s...", baseRef), func() error {
			return git.Pull(parts[0], parts[1])
		})
	}

	localSHA, _ := git.RevParse(baseRef)
	remoteSHA, _ := git.RevParse(upstream)
	if localSHA != "" && remoteSHA != "" && localSHA != remoteSHA {
		mergeBase, _ := git.Run("merge-base", baseRef, upstream)
		if mergeBase == localSHA {
			_, _ = git.Run("update-ref", fmt.Sprintf("refs/heads/%s", baseRef), remoteSHA)
			ui.Successf("Fast-forwarded '%s' to '%s'.", baseRef, upstream)
		} else {
			ui.Warnf("'%s' has diverged from '%s'.", baseRef, upstream)
			ui.Infof("You may want to update '%s' manually before continuing.", baseRef)
		}
	}
	return nil
}

func parsePRNumber(choice string) (int, error) {
	plain := ui.StripANSI(choice)
	re := regexp.MustCompile(`^#(\d+)`)
	matches := re.FindStringSubmatch(plain)
	if matches == nil {
		return 0, fmt.Errorf("could not parse PR number from: %s", choice)
	}
	return strconv.Atoi(matches[1])
}
