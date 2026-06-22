package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var prMergeCmd = &cobra.Command{
	Use:   "merge",
	Short: "Merge the current PR and clean up",
	Long:  "Merge the current branch's pull request on GitHub, then switch to the default branch, pull, and clean up the local branch.\n\nDefaults to squash merge if no strategy flag is given.",
	Args:  cobra.NoArgs,
	RunE:  runPRMerge,
}

var (
	prMergeFlagMerge  bool
	prMergeFlagSquash bool
	prMergeFlagRebase bool
)

func init() {
	prMergeCmd.Flags().BoolVarP(&prMergeFlagMerge, "merge", "m", false, "Create a merge commit")
	prMergeCmd.Flags().BoolVarP(&prMergeFlagSquash, "squash", "s", false, "Squash commits into one commit and merge")
	prMergeCmd.Flags().BoolVarP(&prMergeFlagRebase, "rebase", "r", false, "Rebase commits onto the base branch and merge")
	prMergeCmd.MarkFlagsMutuallyExclusive("merge", "squash", "rebase")
}

func runPRMerge(cmd *cobra.Command, args []string) error {
	if git.IsInWorktree() {
		mainRoot, _ := git.GetMainRepoRoot()
		ui.Warn("Navigate to the main repo first:")
		fmt.Fprintf(os.Stderr, "  cd \"%s\"\n", mainRoot)
		return fmt.Errorf("cannot run from a worktree")
	}

	cfg, err := remote.Detect()
	if err != nil {
		return ui.Die(err.Error())
	}

	onDefault, _ := git.IsOnBranch(cfg.DefaultBranch)
	if onDefault {
		return ui.Die("Already on the default branch. Switch to a PR branch first.")
	}

	if err := challengeUncommittedChanges(); err != nil {
		return err
	}
	if err := challengeUnpushedCommits(); err != nil {
		return err
	}

	pr := ghGetPRForCurrentBranch()
	if pr == nil {
		branch, _ := git.GetCurrentBranch()
		return ui.Dief("No PR found for branch '%s'.", branch)
	}
	if pr.State != "open" {
		return ui.Dief("PR #%d is not open (state: %s). Use 'utpr finish' to clean up.", pr.Number, pr.State)
	}

	strategyFlag := "--squash"
	strategyLabel := "squash commit"
	switch {
	case prMergeFlagMerge:
		strategyFlag = "--merge"
		strategyLabel = "merge commit"
	case prMergeFlagRebase:
		strategyFlag = "--rebase"
		strategyLabel = "rebase"
	}

	if err := ui.MustConfirm(
		fmt.Sprintf("Merge PR #%d '%s' as %s?", pr.Number, pr.Title, strategyLabel),
		true,
	); err != nil {
		if err == ui.ErrCancelled {
			ui.Info("Cancelled.")
		}
		return nil
	}

	sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote)
	if err != nil {
		return ui.Die(err.Error())
	}
	sourceRepo, err := remote.ParseRepoSpec(sourceURL)
	if err != nil {
		return ui.Die(err.Error())
	}

	ghCmd := exec.Command("gh", "pr", "merge", fmt.Sprintf("%d", pr.Number), strategyFlag)
	ghCmd.Stdin = os.Stdin
	ghCmd.Stdout = os.Stdout
	ghCmd.Stderr = os.Stderr
	if err := ghCmd.Run(); err != nil {
		return ui.Die("Failed to merge PR. See output above for details.")
	}

	if err := git.SwitchBranch(cfg.DefaultBranch); err != nil {
		return err
	}
	if err := pullDefaultBranch(cfg); err != nil {
		return err
	}

	return finishOnePR(cfg, sourceRepo, pr.Number)
}
