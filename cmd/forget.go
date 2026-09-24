package cmd

import (
	"errors"
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

var flagForgetYes bool

func init() {
	forgetCmd.Flags().BoolVar(&flagForgetYes, "yes", false, "Assume yes for confirmation prompts")
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
		if errors.Is(err, ui.ErrCancelled) {
			return err
		}
		cfg = nil
		ui.Warnf("Continuing without remote operations: %v", err)
	}
	// Without a remote config, guess the default branch from local branches.
	defaultBranch := ""
	if cfg != nil {
		defaultBranch = cfg.DefaultBranch
	} else {
		defaultBranch = git.GetLocalDefaultBranch()
	}

	var target string
	if len(args) > 0 {
		target = args[0]
		if err := git.ValidateBranchName(target); err != nil {
			return ui.Die(err.Error())
		}
		if target == defaultBranch {
			return ui.Dief("Cannot forget the default branch '%s'.", defaultBranch)
		}
		if !git.BranchExists(target) {
			return ui.Dief("Branch '%s' does not exist locally.", target)
		}
	} else {
		onDefault, err := git.IsOnBranch(defaultBranch)
		if err != nil {
			return err
		}

		if onDefault {
			target, err = pickBranch(defaultBranch, "Select a branch to forget:")
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
			if err := ui.MustConfirm("Abandon branch '"+target+"' and switch to "+defaultBranch+"?", true); err != nil {
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
		if err := git.SwitchBranch(defaultBranch); err != nil {
			return ui.Die(err.Error())
		}
		if cfg != nil {
			if err := pullDefaultBranch(cfg); err != nil {
				ui.Warnf("Could not pull latest %s. Run 'git pull' to update.", cfg.DefaultBranch)
			}
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
	var err error
	confirmed := true
	if !assumeYes() {
		confirmed, err = ui.Confirm("Remove worktree?", true)
		if err != nil {
			return err
		}
	}
	if !confirmed {
		return nil
	}

	err = git.WorktreeRemove(wtPath, false)
	if err != nil {
		ui.Warn("Worktree has uncommitted changes.")
		if assumeYes() {
			return ui.Die("Worktree has uncommitted changes. Re-run without --yes to decide whether to force-remove it.")
		}
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
