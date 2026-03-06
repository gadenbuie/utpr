package cmd

import (
	"strings"

	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var resumeCmd = &cobra.Command{
	Use:   "resume [branch]",
	Short: "Resume work on a PR branch",
	Long:  "Resume work on a PR branch. If no branch is given, an interactive picker is shown.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runResume,
}

func runResume(cmd *cobra.Command, args []string) error {
	cfg, err := remote.Detect()
	if err != nil {
		return err
	}

	var branch string
	if len(args) > 0 {
		branch = args[0]
		if err := git.ValidateBranchName(branch); err != nil {
			return err
		}
	} else {
		branch, err = pickBranch(cfg.DefaultBranch, "Select a branch to resume:")
		if err != nil {
			return err
		}
	}

	if !git.BranchExists(branch) {
		return ui.Dief("Branch '%s' does not exist locally.", branch)
	}

	wtPath := git.GetBranchWorktreePath(branch)
	if wtPath != "" {
		offerWorktreeNavigation(wtPath)
		return nil
	}

	if err := challengeUncommittedChanges(); err != nil {
		return err
	}

	if err := git.SwitchBranch(branch); err != nil {
		return err
	}

	tracking := git.GetTrackingBranch()
	if tracking != "" {
		err := ui.Spin("Pulling from "+tracking+"...", func() error {
			_, pullErr := git.Run("pull")
			return pullErr
		})
		if err != nil {
			ui.Warn("Pull failed (you may have merge conflicts to resolve).")
			return err
		}
	}

	ui.Successf("Resumed '%s'.", branch)
	return nil
}

// pickBranch shows an interactive branch picker, excluding the default branch.
func pickBranch(defaultBranch, header string) (string, error) {
	branchOutput, err := ui.SpinWithResult("Looking up local branches...", func() (string, error) {
		return git.ForEachRef("%(refname:short)", "-committerdate", "refs/heads/")
	})
	if err != nil {
		return "", err
	}

	var branches []string
	for _, b := range strings.Split(branchOutput, "\n") {
		b = strings.TrimSpace(b)
		if b != "" && b != defaultBranch {
			branches = append(branches, b)
		}
	}

	if len(branches) == 0 {
		return "", ui.Die("No branches to select.")
	}

	// Build styled display list with worktree annotations
	var displayItems []string
	for _, b := range branches {
		wtPath := git.GetBranchWorktreePath(b)
		if wtPath != "" {
			displayItems = append(displayItems, ui.StyleBranchName(b)+"  [worktree]")
		} else {
			displayItems = append(displayItems, ui.StyleBranchName(b))
		}
	}

	selected, err := ui.Choose(header, displayItems)
	if err != nil {
		return "", err
	}
	if selected == "" {
		return "", ui.Die("No branch selected.")
	}

	// Strip ANSI codes and worktree annotation
	plain := ui.StripANSI(selected)
	plain = strings.TrimSuffix(plain, "  [worktree]")
	return plain, nil
}
