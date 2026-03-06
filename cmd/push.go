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

func init() {
	pushCmd.Flags().StringVar(&pushEditMode, "edit", "terminal",
		"PR creation mode: 'terminal' (default) or 'browser'")
}

func runPush(cmd *cobra.Command, args []string) error {
	cfg, err := remote.Detect()
	if err != nil {
		return err
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
			return err
		}
		ui.Successf("Pushed '%s' to %s.", current, cfg.PushRemote)

		if pushEditMode == "browser" {
			return openCompareURL(cfg, current)
		}

		// Get ownerRepo for PR creation
		pushURL, err := git.Run("remote", "get-url", cfg.PushRemote)
		if err != nil {
			return err
		}
		pushOwnerRepo, err := remote.ParseRepoSpec(pushURL)
		if err != nil {
			return err
		}

		// Determine target repo for PR creation (source repo in fork layout)
		targetRepo := pushOwnerRepo
		if cfg.Layout == "fork" {
			sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote)
			if err != nil {
				return err
			}
			targetRepo, err = remote.ParseRepoSpec(sourceURL)
			if err != nil {
				return err
			}
		}

		// Pre-fill title from first commit on the branch
		var title, body string
		subjects, err := git.Run("log", cfg.DefaultBranch+"..HEAD", "--format=%s", "--reverse")
		if err == nil && subjects != "" {
			title = strings.Split(subjects, "\n")[0]
		}
		bodies, err := git.Run("log", cfg.DefaultBranch+"..HEAD", "--format=%b", "--reverse")
		if err == nil && bodies != "" {
			firstBody := strings.Split(bodies, "\n\n")[0]
			body = strings.TrimSpace(firstBody)
		}

		var draft bool

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().Title("PR Title").Value(&title),
				huh.NewText().Title("PR Body").Value(&body).Lines(6),
				huh.NewConfirm().Title("Create as draft?").Value(&draft).
					Affirmative("Yes").Negative("No"),
			),
		)
		if err := form.Run(); err != nil {
			ui.Info("Cancelled.")
			return nil
		}

		// Build head ref: for forks, qualify with fork owner
		head := current
		if cfg.Layout == "fork" {
			forkOwner := strings.Split(pushOwnerRepo, "/")[0]
			head = forkOwner + ":" + current
		}

		pr, err := gh.CreatePR(targetRepo, gh.CreatePRParams{
			Title: title,
			Body:  body,
			Head:  head,
			Base:  cfg.DefaultBranch,
			Draft: draft,
		})
		if err != nil {
			return ui.Dief("Failed to create pull request: %v", err)
		}

		git.SetBranchPRURL(current, pr.HTMLURL)
		ui.Successf("PR created: %s", pr.HTMLURL)
		return nil
	}

	// Subsequent push — check if behind remote
	behind, err := git.RevListCount("HEAD..@{u}")
	if err != nil {
		behind = 0
	}
	if behind > 0 {
		ui.Warnf("Your branch is %d commit(s) behind the remote.", behind)
		ui.Warn("Pull first with 'utpr pull'.")
		return fmt.Errorf("branch is behind remote")
	}

	err = ui.Spin(fmt.Sprintf("Pushing to %s...", tracking), func() error {
		_, pushErr := git.Run("push")
		return pushErr
	})
	if err != nil {
		return err
	}
	ui.Successf("Pushed to %s.", tracking)
	return nil
}

func openCompareURL(cfg *remote.Config, branch string) error {
	pushURL, err := git.Run("remote", "get-url", cfg.PushRemote)
	if err != nil {
		return err
	}
	ownerRepo, err := remote.ParseRepoSpec(pushURL)
	if err != nil {
		return err
	}

	var compareURL string
	if cfg.Layout == "fork" {
		sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote)
		if err != nil {
			return err
		}
		sourceRepo, err := remote.ParseRepoSpec(sourceURL)
		if err != nil {
			return err
		}
		forkOwner := strings.Split(ownerRepo, "/")[0]
		compareURL = fmt.Sprintf("https://github.com/%s/compare/%s...%s:%s?expand=1",
			sourceRepo, cfg.DefaultBranch, forkOwner, branch)
	} else {
		compareURL = fmt.Sprintf("https://github.com/%s/compare/%s...%s?expand=1",
			ownerRepo, cfg.DefaultBranch, branch)
	}

	return openURL(compareURL)
}

func ghGetPRURL() (string, error) {
	cfg, err := remote.Detect()
	if err != nil {
		return "", err
	}

	sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote)
	if err != nil {
		return "", err
	}
	ownerRepo, err := remote.ParseRepoSpec(sourceURL)
	if err != nil {
		return "", err
	}

	branch, err := git.GetCurrentBranch()
	if err != nil {
		return "", err
	}

	pr, err := gh.GetPRForBranch(ownerRepo, branch, "open")
	if err != nil {
		return "", err
	}
	if pr == nil {
		return "", nil
	}
	return pr.HTMLURL, nil
}
