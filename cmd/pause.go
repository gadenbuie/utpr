package cmd

import (
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var pauseCmd = &cobra.Command{
	Use:   "pause",
	Short: "Switch back to default branch",
	Long:  "Switch back to the default branch, stashing or committing any in-progress work.",
	RunE:  runPause,
}

func runPause(cmd *cobra.Command, args []string) error {
	cfg, err := remote.Detect()
	if err != nil {
		return err
	}

	if git.IsInWorktree() {
		mainRoot, err := git.GetMainRepoRoot()
		if err != nil {
			return err
		}
		ui.Info("You're in a worktree. Main repo is at:")
		ui.Infof("cd \"%s\"", mainRoot)
		return nil
	}

	onDefault, err := git.IsOnBranch(cfg.DefaultBranch)
	if err != nil {
		return err
	}
	if onDefault {
		return ui.Dief("Already on the default branch (%s). Nothing to pause.", cfg.DefaultBranch)
	}

	current, err := git.GetCurrentBranch()
	if err != nil {
		return err
	}

	wtPath := git.GetBranchWorktreePath(current)
	if wtPath != "" {
		ui.Infof("Branch '%s' has a worktree at: %s", current, wtPath)
		ui.Info("Navigate there to continue working, or use 'utpr forget' to clean up.")
		return nil
	}

	// Check if the PR for this branch has been merged
	prState, _ := ghPRViewState()
	if prState == "MERGED" {
		ui.Infof("The PR for branch '%s' has already been merged.", current)
		confirmed, err := ui.Confirm("Finish this PR instead?", true)
		if err != nil {
			return err
		}
		if confirmed {
			return runFinish(finishCmd, nil)
		}
	}

	if err := challengeUncommittedChanges(); err != nil {
		return err
	}
	if err := challengeUnpushedCommits(); err != nil {
		return err
	}
	if err := challengeBranchBehindRemote(); err != nil {
		return err
	}

	if err := git.SwitchBranch(cfg.DefaultBranch); err != nil {
		return err
	}
	if err := pullDefaultBranch(cfg); err != nil {
		return err
	}

	ui.Successf("Paused '%s'. Switched to %s.", current, cfg.DefaultBranch)
	return nil
}
