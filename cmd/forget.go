package cmd

import (
	"fmt"
	"os"

	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var forgetCmd = &cobra.Command{
	Use:   "forget [branch]",
	Short: "Abandon and delete a local PR branch",
	Long:  "Abandon a local PR branch and delete it. Automatically removes any associated worktree.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runForget,
}

func runForget(cmd *cobra.Command, args []string) error {
	if git.IsInWorktree() {
		mainRoot, _ := git.GetMainRepoRoot()
		branch, _ := git.GetCurrentBranch()
		ui.Warn("Navigate to the main repo first:")
		fmt.Fprintf(os.Stderr, "  cd \"%s\"\n", mainRoot)
		fmt.Fprintf(os.Stderr, "  utpr forget %s\n", branch)
		return fmt.Errorf("cannot run from a worktree")
	}

	cfg, err := remote.Detect()
	if err != nil {
		return ui.Die(err.Error())
	}

	var target string
	if len(args) > 0 {
		target = args[0]
		if err := git.ValidateBranchName(target); err != nil {
			return ui.Die(err.Error())
		}
		if target == cfg.DefaultBranch {
			return ui.Dief("Cannot forget the default branch '%s'.", cfg.DefaultBranch)
		}
		if !git.BranchExists(target) {
			return ui.Dief("Branch '%s' does not exist locally.", target)
		}
	} else {
		onDefault, err := git.IsOnBranch(cfg.DefaultBranch)
		if err != nil {
			return err
		}

		if onDefault {
			target, err = pickBranch(cfg.DefaultBranch, "Select a branch to forget:")
			if err != nil {
				return err
			}
		} else {
			target, err = git.GetCurrentBranch()
			if err != nil {
				return err
			}
		}
	}

	current, err := git.GetCurrentBranch()
	if err != nil {
		return err
	}

	if target == current {
		if err := challengeUncommittedChanges(); err != nil {
			return err
		}
	}
	alreadyConfirmed, err := challengeLocalBranchDelete(target)
	if err != nil {
		return err
	}

	if target == current {
		if !alreadyConfirmed {
			if err := ui.MustConfirm("Abandon branch '"+target+"' and switch to "+cfg.DefaultBranch+"?", true); err != nil {
				if err == ui.ErrCancelled {
					ui.Info("Cancelled.")
					return nil
				}
				return err
			}
		}
		if err := removeWorktree(target); err != nil {
			return err
		}
		if err := git.SwitchBranch(cfg.DefaultBranch); err != nil {
			return ui.Die(err.Error())
		}
		if err := pullDefaultBranch(cfg); err != nil {
			ui.Warnf("Could not pull latest %s. Run 'git pull' to update.", cfg.DefaultBranch)
		}
	} else {
		if !alreadyConfirmed {
			if err := ui.MustConfirm("Delete branch '"+target+"'?", true); err != nil {
				if err == ui.ErrCancelled {
					ui.Info("Cancelled.")
					return nil
				}
				return err
			}
		}
		if err := removeWorktree(target); err != nil {
			return err
		}
	}

	if err := git.DeleteBranch(target); err != nil {
		return ui.Die(err.Error())
	}
	ui.Successf("Deleted local branch '%s'.", target)
	remote.CleanupUtprRemotes()
	return nil
}

// removeWorktree removes a branch's worktree if one exists, prompting the user.
func removeWorktree(branch string) error {
	wtPath := git.GetBranchWorktreePath(branch)
	if wtPath == "" {
		return nil
	}

	ui.Infof("Branch '%s' has a worktree at: %s", branch, wtPath)
	confirmed, err := ui.Confirm("Remove worktree?", true)
	if err != nil {
		return err
	}
	if !confirmed {
		return nil
	}

	err = git.WorktreeRemove(wtPath, false)
	if err != nil {
		ui.Warn("Worktree has uncommitted changes.")
		forceConfirmed, err := ui.Confirm("Force remove worktree?", false)
		if err != nil || !forceConfirmed {
			return ui.Die("Cannot proceed without removing the worktree.")
		}
		if err := git.WorktreeRemove(wtPath, true); err != nil {
			return ui.Die(err.Error())
		}
	}

	ui.Successf("Removed worktree for '%s'.", branch)
	return nil
}
