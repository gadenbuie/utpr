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
		return ui.Die(err.Error())
	}

	var branch string
	if len(args) > 0 {
		branch = args[0]
		if err := git.ValidateBranchName(branch); err != nil {
			return ui.Die(err.Error())
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
		return ui.Die(err.Error())
	}

	tracking := git.GetTrackingBranch()
	if tracking != "" {
		err := ui.Spin("Pulling from "+tracking+"...", func() error {
			_, pullErr := git.Run("pull")
			return pullErr
		})
		if err != nil {
			ui.Warn("Could not pull latest changes. Run 'git pull' to update.")
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

	var items []ui.BranchPickerItem
	for _, b := range strings.Split(branchOutput, "\n") {
		b = strings.TrimSpace(b)
		if b != "" && b != defaultBranch {
			items = append(items, ui.BranchPickerItem{
				Name:        b,
				HasWorktree: git.GetBranchWorktreePath(b) != "",
			})
		}
	}

	if len(items) == 0 {
		return "", ui.Die("No branches to select.")
	}

	opts := ui.FormatBranchPickerOptions(items)
	selected, err := ui.ChooseWithOptions(header, opts)
	if err != nil {
		return "", err
	}
	if selected == "" {
		return "", ui.Die("No branch selected.")
	}

	return selected, nil
}
