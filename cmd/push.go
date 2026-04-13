package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push branch and create/update PR",
	Long:  "Push the current branch and create or update the associated PR.",
	RunE:  runPush,
}

var pushEditMode string
var pushForce bool

func init() {
	pushCmd.Flags().StringVar(&pushEditMode, "edit", "terminal",
		"PR creation mode: 'terminal' (default) or 'browser'")
	pushCmd.Flags().BoolVar(&pushForce, "force", false,
		"Force push using --force-with-lease (safe force push after rebase)")
}

func runPush(cmd *cobra.Command, args []string) error {
	cfg, err := remote.Detect()
	if err != nil {
		return ui.Die(err.Error())
	}

	onDefault, err := git.IsOnBranch(cfg.DefaultBranch)
	if err != nil {
		return err
	}
	if onDefault {
		return ui.Die("Cannot push from the default branch. Create a PR branch first with 'utpr init'.")
	}

	if err := challengeUncommittedChanges(); err != nil {
		return err
	}

	if pushEditMode != "terminal" && pushEditMode != "browser" {
		return ui.Dief("Unknown edit mode: %s (expected 'browser' or 'terminal')", pushEditMode)
	}

	current, err := git.GetCurrentBranch()
	if err != nil {
		return err
	}
	tracking := git.GetTrackingBranch()

	if tracking == "" {
		// First push
		err := ui.Spin(
			fmt.Sprintf("Pushing '%s' to %s...", current, cfg.PushRemote),
			func() error {
				return git.Push(cfg.PushRemote, true)
			},
		)
		if err != nil {
			return ui.Die(err.Error())
		}
		ui.Successf("Pushed '%s' to %s.", current, cfg.PushRemote)

		if pushEditMode == "browser" {
			return openCompareURL(cfg, current)
		}

		return promptCreatePR(cfg, current)
	}

	// Subsequent push — check if behind remote
	if !pushForce {
		behind, err := git.RevListCount("HEAD..@{u}")
		if err != nil {
			behind = 0
		}
		if behind > 0 {
			ui.Warnf("Your branch is %d commit(s) behind the remote.", behind)
			ui.Warn("Pull first with 'utpr pull'.")
			return fmt.Errorf("branch is behind remote")
		}
	}

	pushArgs := []string{"push"}
	if pushForce {
		pushArgs = append(pushArgs, "--force-with-lease")
	}
	err = ui.Spin(fmt.Sprintf("Pushing to %s...", tracking), func() error {
		_, pushErr := git.Run(pushArgs...)
		return pushErr
	})
	if err != nil {
		return ui.Die(err.Error())
	}
	ui.Successf("Pushed to %s.", tracking)

	// Check if there's already a PR for this branch
	if git.GetBranchPRURL(current) != "" {
		return nil
	}

	pr := ghGetPRForCurrentBranch()
	if pr != nil {
		// PR exists on GitHub but not stored locally — save it
		_ = git.SetBranchPRURL(current, pr.HTMLURL)
		return nil
	}

	// No PR exists — offer to create one
	createPR, err := ui.Confirm("No PR found for this branch. Create one?", true)
	if err != nil || !createPR {
		return nil
	}

	if pushEditMode == "browser" {
		return openCompareURL(cfg, current)
	}

	return promptCreatePR(cfg, current)
}

func promptCreatePR(cfg *remote.Config, current string) error {
	// Get ownerRepo for PR creation
	pushURL, err := git.Run("remote", "get-url", cfg.PushRemote)
	if err != nil {
		return ui.Die(err.Error())
	}
	pushOwnerRepo, err := remote.ParseRepoSpec(pushURL)
	if err != nil {
		return ui.Die(err.Error())
	}

	// Determine target repo for PR creation (source repo in fork layout)
	targetRepo := pushOwnerRepo
	if cfg.Layout == "fork" {
		sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote)
		if err != nil {
			return ui.Die(err.Error())
		}
		sourceOwnerRepo, err := remote.ParseRepoSpec(sourceURL)
		if err != nil {
			return ui.Die(err.Error())
		}
		targetRepo = determinePRTarget(cfg.Layout, pushOwnerRepo, sourceOwnerRepo)
	}

	// Pre-fill title from first commit on the branch
	var title, body string
	subjects, err := git.Run("log", cfg.DefaultBranch+"..HEAD", "--format=%s", "--reverse")
	if err == nil && subjects != "" {
		title = strings.Split(subjects, "\n")[0]
	}
	// Get body from first commit only
	firstCommit, err := git.Run("log", cfg.DefaultBranch+"..HEAD", "--format=%H", "--reverse")
	if err == nil && firstCommit != "" {
		hash := strings.Split(firstCommit, "\n")[0]
		commitBody, err := git.Run("log", "-1", "--format=%b", hash)
		if err == nil {
			body = strings.TrimSpace(commitBody)
		}
	}

	ready := true

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("PR Title").Value(&title),
			huh.NewText().Title("PR Body").Value(&body).Lines(6),
			huh.NewConfirm().Title("Ready for review?").Value(&ready).
				Affirmative("Yes").Negative("Create as draft"),
		),
	)
	if err := form.Run(); err != nil {
		ui.Info("Cancelled.")
		return nil
	}

	title = strings.TrimSpace(title)
	if title == "" {
		return ui.Die("PR title cannot be empty.")
	}

	head := buildPRHeadRef(cfg.Layout, pushOwnerRepo, current)

	pr, err := gh.CreatePR(targetRepo, gh.CreatePRParams{
		Title: title,
		Body:  body,
		Head:  head,
		Base:  cfg.DefaultBranch,
		Draft: !ready,
	})
	if err != nil {
		return ui.Dief("Failed to create pull request: %v", err)
	}

	if err := git.SetBranchPRURL(current, pr.HTMLURL); err != nil {
		ui.Warnf("Could not store PR URL in git config: %v", err)
	}
	ui.Successf("PR created: %s", pr.HTMLURL)
	return nil
}

func openCompareURL(cfg *remote.Config, branch string) error {
	pushURL, err := git.Run("remote", "get-url", cfg.PushRemote)
	if err != nil {
		return ui.Die(err.Error())
	}
	pushOwnerRepo, err := remote.ParseRepoSpec(pushURL)
	if err != nil {
		return ui.Die(err.Error())
	}

	sourceOwnerRepo := pushOwnerRepo
	if cfg.Layout == "fork" {
		sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote)
		if err != nil {
			return ui.Die(err.Error())
		}
		sourceOwnerRepo, err = remote.ParseRepoSpec(sourceURL)
		if err != nil {
			return ui.Die(err.Error())
		}
	}

	compareURL := buildCompareURL(cfg.Layout, pushOwnerRepo, sourceOwnerRepo, cfg.DefaultBranch, branch)
	return openURL(compareURL)
}

// determinePRTarget returns the repo to create the PR against.
// In fork layout, PRs target the source (upstream) repo.
func determinePRTarget(layout, pushRepo, sourceRepo string) string {
	if layout == "fork" {
		return sourceRepo
	}
	return pushRepo
}

// buildPRHeadRef returns the head ref for PR creation.
// In fork layout, qualifies with the fork owner prefix.
func buildPRHeadRef(layout, pushOwnerRepo, branch string) string {
	if layout == "fork" {
		forkOwner := strings.Split(pushOwnerRepo, "/")[0]
		return forkOwner + ":" + branch
	}
	return branch
}

// buildCompareURL returns the GitHub compare URL for browser-mode push.
func buildCompareURL(layout, pushOwnerRepo, sourceOwnerRepo, defaultBranch, branch string) string {
	if layout == "fork" {
		forkOwner := strings.Split(pushOwnerRepo, "/")[0]
		return fmt.Sprintf("https://github.com/%s/compare/%s...%s:%s?expand=1",
			sourceOwnerRepo, defaultBranch, forkOwner, branch)
	}
	return fmt.Sprintf("https://github.com/%s/compare/%s...%s?expand=1",
		pushOwnerRepo, defaultBranch, branch)
}
