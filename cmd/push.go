package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

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

		// Interactive PR creation via gh
		ghCmd := exec.Command("gh", "pr", "create")
		ghCmd.Stdin = os.Stdin
		ghCmd.Stdout = os.Stderr // all user output goes to stderr
		ghCmd.Stderr = os.Stderr
		exitCode := 0
		if err := ghCmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				return ui.Die("Failed to create pull request.")
			}
		}
		if exitCode == 130 {
			ui.Info("Cancelled.")
			return nil
		}
		if exitCode != 0 {
			return ui.Die("Failed to create pull request.")
		}

		// Store the PR URL
		prURL, _ := ghGetPRURL()
		if prURL != "" {
			git.SetBranchPRURL(current, prURL)
			ui.Successf("PR created: %s", prURL)
		}
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
	cmd := exec.Command("gh", "pr", "view", "--json", "url", "--jq", ".url")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
