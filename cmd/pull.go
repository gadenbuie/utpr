package cmd

import (
	"fmt"
	"strings"

	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var pullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull latest changes",
	Long:  "Pull the latest changes for the current branch from GitHub.",
	RunE:  runPull,
}

func runPull(cmd *cobra.Command, args []string) error {
	cfg, err := remote.Detect()
	if err != nil {
		return err
	}

	onDefault, err := git.IsOnBranch(cfg.DefaultBranch)
	if err != nil {
		return err
	}

	if onDefault {
		err := ui.Spin(
			fmt.Sprintf("Pulling %s from %s...", cfg.DefaultBranch, cfg.SourceRemote),
			func() error {
				return git.Pull(cfg.SourceRemote, cfg.DefaultBranch)
			},
		)
		if err != nil {
			return err
		}
		ui.Successf("Pulled latest %s.", cfg.DefaultBranch)
		return nil
	}

	tracking := git.GetTrackingBranch()
	if tracking == "" {
		ui.Warn("No tracking branch configured. Nothing to pull.")
		return nil
	}

	// Extract remote name from tracking ref (e.g., "origin/branch" -> "origin")
	remoteName := strings.SplitN(tracking, "/", 2)[0]

	err = ui.Spin(fmt.Sprintf("Fetching from %s...", remoteName), func() error {
		return git.Fetch(remoteName)
	})
	if err != nil {
		return err
	}

	ahead, err := git.RevListCount("@{u}..HEAD")
	if err != nil {
		return err
	}
	behind, err := git.RevListCount("HEAD..@{u}")
	if err != nil {
		return err
	}

	if behind == 0 {
		ui.Success("Already up to date.")
		return nil
	}

	if ahead > 0 {
		ui.Warnf("The remote branch has diverged from your local branch (%d local commit(s) not on remote).", ahead)
		ui.Warn("This usually means the PR author force-pushed. A force-update will discard your local commits.")

		confirmed, err := ui.Confirm(
			fmt.Sprintf("Force-update local branch to match %s?", tracking),
			false,
		)
		if err != nil {
			return err
		}
		if confirmed {
			if err := git.RunInteractive("reset", "--hard", "@{u}"); err != nil {
				return ui.Die("Failed to force-update.")
			}
			ui.Successf("Force-updated to match %s.", tracking)
		} else {
			ui.Info("To manually update, run:")
			ui.Info("  git fetch")
			ui.Info("  git reset --hard @{u}")
			return fmt.Errorf("cancelled")
		}
	} else {
		err = ui.Spin(fmt.Sprintf("Pulling from %s...", tracking), func() error {
			_, pullErr := git.Run("pull")
			return pullErr
		})
		if err != nil {
			return err
		}
		ui.Success("Pulled latest changes.")
	}
	return nil
}
