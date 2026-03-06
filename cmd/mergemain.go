package cmd

import (
	"fmt"
	"strings"

	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var mergeMainCmd = &cobra.Command{
	Use:   "merge-main",
	Short: "Merge or rebase default branch into current branch",
	Long:  "Merge or rebase the default branch into the current PR branch to stay up to date.",
	RunE:  runMergeMain,
}

var mergeMainRebase bool

func init() {
	mergeMainCmd.Flags().BoolVar(&mergeMainRebase, "rebase", false,
		"Rebase onto the default branch instead of merging")
}

func runMergeMain(cmd *cobra.Command, args []string) error {
	cfg, err := remote.Detect()
	if err != nil {
		return err
	}

	onDefault, err := git.IsOnBranch(cfg.DefaultBranch)
	if err != nil {
		return err
	}
	if onDefault {
		return ui.Die("Already on the default branch. Use 'utpr pull' instead.")
	}

	current, err := git.GetCurrentBranch()
	if err != nil {
		return err
	}
	tracking := git.GetTrackingBranch()

	// If --rebase was not explicitly passed, decide strategy
	if !mergeMainRebase {
		if tracking == "" {
			strategy, err := ui.Choose(
				fmt.Sprintf("Branch '%s' hasn't been pushed yet — rebase or merge?", current),
				[]string{
					fmt.Sprintf("Rebase onto %s", cfg.DefaultBranch),
					fmt.Sprintf("Merge %s into %s", cfg.DefaultBranch, current),
				},
			)
			if err != nil {
				return err
			}
			if strategy == fmt.Sprintf("Rebase onto %s", cfg.DefaultBranch) {
				mergeMainRebase = true
			}
		}
	}

	// Warn about rebase + already-pushed
	if mergeMainRebase && tracking != "" {
		ui.Warnf("Branch '%s' has already been pushed to '%s'.", current, tracking)
		ui.Info("Rebasing rewrites history and will require a force push afterwards.")
		if err := ui.MustConfirm("Rebase anyway?", false); err != nil {
			ui.Info("Cancelled. Run 'utpr merge-main' without --rebase to merge instead.")
			return err
		}
	}

	// Fetch default branch
	err = ui.Spin(
		fmt.Sprintf("Fetching %s from %s...", cfg.DefaultBranch, cfg.SourceRemote),
		func() error {
			return git.Fetch(cfg.SourceRemote, cfg.DefaultBranch)
		},
	)
	if err != nil {
		return err
	}

	upstreamRef := fmt.Sprintf("%s/%s", cfg.SourceRemote, cfg.DefaultBranch)

	if mergeMainRebase {
		ui.Infof("Rebasing %s onto %s...", current, upstreamRef)
		if err := git.RunInteractive("rebase", upstreamRef); err != nil {
			// Check if rebase actually started (has conflicts) vs failed to start
			_, verifyErr := git.RevParse("--verify", "REBASE_HEAD")
			if verifyErr != nil {
				return ui.Die("Rebase failed to start. You may have unstaged changes — commit or stash them first.")
			}
			showConflictHelp("rebase", cfg, current, tracking)
			return fmt.Errorf("rebase has conflicts")
		}
		ui.Successf("Rebased %s onto %s.", current, cfg.DefaultBranch)
		if tracking != "" {
			ui.Info("You will need to force push:")
			ui.Infof("  git push --force-with-lease %s %s", cfg.PushRemote, current)
		}
	} else {
		ui.Infof("Merging %s into %s...", upstreamRef, current)
		if err := git.RunInteractive("merge", "--no-edit", upstreamRef); err != nil {
			showConflictHelp("merge", cfg, current, tracking)
			return fmt.Errorf("merge has conflicts")
		}
		ui.Successf("Merged %s into %s.", cfg.DefaultBranch, current)
	}
	return nil
}

func showConflictHelp(mode string, cfg *remote.Config, current, tracking string) {
	conflicts, _ := git.Run("diff", "--name-only", "--diff-filter=U")
	if conflicts != "" {
		ui.Warnf("%s has conflicts. Resolve each file, then stage it:", capitalize(mode))
		for _, f := range splitLines(conflicts) {
			ui.Warnf("  • %s", f)
		}
	}

	if mode == "rebase" {
		ui.Info("After staging all resolved files, continue the rebase:")
		ui.Info("  git add <file>")
		ui.Info("  git rebase --continue")
		ui.Info("Repeat if there are conflicts in subsequent commits. To abort:")
		ui.Info("  git rebase --abort")
		if tracking != "" {
			ui.Info("Once the rebase completes, force push:")
			ui.Infof("  git push --force-with-lease %s %s", cfg.PushRemote, current)
		}
	} else {
		ui.Info("After staging all resolved files, complete the merge:")
		ui.Info("  git add <file>")
		ui.Info("  git merge --continue")
		ui.Info("To abort the merge:")
		ui.Info("  git merge --abort")
	}
}

func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
