package cmd

import (
	"fmt"
	"strings"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var fetchCmd = &cobra.Command{
	Use:   "fetch [pr-number]",
	Short: "Fetch a PR from GitHub",
	Long:  "Fetch a PR from GitHub and check it out locally.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runFetch,
}

var flagFetchWorktree bool

func init() {
	fetchCmd.Flags().BoolVar(&flagFetchWorktree, "worktree", false, "Check out the PR in a new git worktree")
}

func runFetch(cmd *cobra.Command, args []string) error {
	cfg, err := remote.Detect()
	if err != nil {
		return ui.Die(err.Error())
	}

	var prNumber int
	if len(args) > 0 {
		prNumber, err = parsePRNumber("#" + args[0])
		if err != nil {
			return ui.Dief("Invalid PR number: %s", args[0])
		}
	} else {
		prNumber, err = pickPR("Select a PR to fetch:")
		if err != nil {
			return err
		}
		if prNumber == 0 {
			return nil
		}
	}

	sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote)
	if err != nil {
		return ui.Die(err.Error())
	}
	sourceRepo, err := remote.ParseRepoSpec(sourceURL)
	if err != nil {
		return ui.Die(err.Error())
	}

	pr, err := gh.GetPR(sourceRepo, prNumber)
	if err != nil {
		return ui.Dief("Failed to fetch PR #%d details.", prNumber)
	}

	headRef := pr.Head.Ref
	remoteName, isForkPR := determineFetchRemote(pr.Head.Repo.FullName, pr.Base.Repo.FullName, cfg.SourceRemote)

	if isForkPR {
		// Add remote if needed
		if _, err := git.Run("remote", "get-url", remoteName); err != nil {
			ui.Infof("Adding remote '%s' for '%s'.", remoteName, pr.Head.Repo.FullName)
			_, _ = git.Run("remote", "add", remoteName, pr.Head.Repo.CloneURL)
			_ = git.MarkRemoteCreatedByUtpr(remoteName)
		}
	}

	existingTracking := git.FindBranchByUpstream(remoteName, headRef)
	currentUser, _ := gh.GetLogin()
	localBranch := determineFetchBranchName(existingTracking, currentUser, pr.User.Login, prNumber, headRef)

	err = ui.Spin(
		fmt.Sprintf("Fetching '%s' from %s...", headRef, remoteName),
		func() error {
			return git.Fetch(remoteName, headRef)
		},
	)
	if err != nil {
		return ui.Die(err.Error())
	}

	fetchSHA, err := git.RevParse("FETCH_HEAD")
	if err != nil {
		return ui.Die("Could not resolve FETCH_HEAD after fetch.")
	}

	if flagFetchWorktree {
		if git.BranchExists(localBranch) {
			wtPath := git.GetBranchWorktreePath(localBranch)
			if wtPath != "" {
				// git refuses to update a ref that is checked out in another worktree;
				// merge directly inside that worktree instead.
				ui.Infof("Branch '%s' is checked out in a worktree.", localBranch)
				if err := git.RunInteractiveInDir(wtPath, "merge", fetchSHA); err != nil {
					ui.Warn("Merge had conflicts. Resolve them before continuing.")
				}
				configureFetchedBranch(localBranch, remoteName, headRef, pr.HTMLURL)
				ui.Successf("Fetched PR #%d → '%s'.", prNumber, localBranch)
				offerWorktreeNavigation(wtPath)
				return nil
			}
			if git.IsBranchInMainWorktree(localBranch) {
				if err := freeUpCurrentBranch(cfg); err != nil {
					return err
				}
			}
			if _, err := git.Run("fetch", ".", "FETCH_HEAD:"+localBranch); err != nil {
				return ui.Dief("Failed to update branch '%s'.", localBranch)
			}
		} else {
			if _, err := git.Run("branch", localBranch, "FETCH_HEAD"); err != nil {
				return ui.Dief("Failed to create branch '%s'.", localBranch)
			}
		}
		configureFetchedBranch(localBranch, remoteName, headRef, pr.HTMLURL)
		ui.Successf("Fetched PR #%d → '%s'.", prNumber, localBranch)

		wtPath := git.GetBranchWorktreePath(localBranch)
		if wtPath != "" {
			ui.Infof("Branch '%s' already has a worktree.", localBranch)
			offerWorktreeNavigation(wtPath)
			return nil
		}
		return initWorktree(localBranch)
	}

	// Non-worktree mode
	if git.BranchExists(localBranch) {
		wtPath := git.GetBranchWorktreePath(localBranch)
		if wtPath != "" {
			ui.Infof("Branch '%s' is checked out in a worktree.", localBranch)
			if err := git.RunInteractiveInDir(wtPath, "merge", fetchSHA); err != nil {
				ui.Warn("Merge had conflicts. Resolve them before continuing.")
			}
			configureFetchedBranch(localBranch, remoteName, headRef, pr.HTMLURL)
			ui.Successf("Fetched PR #%d → '%s'.", prNumber, localBranch)
			offerWorktreeNavigation(wtPath)
			return nil
		}
		if err := git.SwitchBranch(localBranch); err != nil {
			return ui.Die(err.Error())
		}
		if err := git.RunInteractive("merge", "FETCH_HEAD"); err != nil {
			ui.Warn("Merge had conflicts. Resolve them before continuing.")
		}
	} else {
		if _, err := git.Run("switch", "-c", localBranch, "FETCH_HEAD"); err != nil {
			return ui.Dief("Failed to create branch '%s'.", localBranch)
		}
	}

	configureFetchedBranch(localBranch, remoteName, headRef, pr.HTMLURL)
	ui.Successf("Fetched PR #%d → '%s'.", prNumber, localBranch)
	return nil
}

// determineFetchRemote returns the remote name to fetch from and whether the PR is from a fork.
func determineFetchRemote(prHeadRepo, prBaseRepo, sourceRemote string) (remoteName string, isFork bool) {
	isFork = prHeadRepo != prBaseRepo
	if isFork {
		remoteName = strings.Split(prHeadRepo, "/")[0]
	} else {
		remoteName = sourceRemote
	}
	return
}

// determineFetchBranchName returns the local branch name for a fetched PR.
func determineFetchBranchName(existingTracking, currentUser, prAuthor string, prNumber int, headRef string) string {
	if existingTracking != "" {
		return existingTracking
	}
	if currentUser != prAuthor {
		return fmt.Sprintf("pr/%d-%s-%s", prNumber, prAuthor, headRef)
	}
	return headRef
}

func configureFetchedBranch(localBranch, remoteName, headRef, prURL string) {
	_ = git.SetBranchUpstream(localBranch, remoteName+"/"+headRef)
	_ = git.MarkBranchCreatedByUtpr(localBranch)
	_ = git.SetBranchPRURL(localBranch, prURL)
}

func pickPR(header string) (int, error) {
	cfg := remote.Require()
	sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote)
	if err != nil {
		return 0, ui.Die("Could not determine remote URL.")
	}
	ownerRepo, err := remote.ParseRepoSpec(sourceURL)
	if err != nil {
		return 0, ui.Die("Could not parse repository from remote URL.")
	}

	prs, err := ui.SpinWithResult("Getting open PRs...", func() ([]gh.PRInfo, error) {
		return gh.ListPRs(ownerRepo, "open")
	})
	if err != nil {
		return 0, ui.Die("Failed to list PRs.")
	}
	if len(prs) == 0 {
		return 0, ui.Die("No open PRs found.")
	}

	currentUser, _ := gh.GetLogin()
	items := make([]ui.PRPickerItem, 0, len(prs))
	for _, pr := range prs {
		isCross := pr.Head.Repo.FullName != pr.Base.Repo.FullName
		items = append(items, ui.PRPickerItem{
			Number:      pr.Number,
			Title:       pr.Title,
			Author:      pr.User.Login,
			Branch:      pr.Head.Ref,
			IsCrossRepo: isCross,
			IsHighlight: currentUser != "" && pr.User.Login == currentUser,
		})
	}

	opts := ui.FormatPRPickerOptions(items, ui.PickerFetch)
	selected, err := ui.ChooseWithOptions(header, opts)
	if err != nil {
		ui.Info("Cancelled.")
		return 0, nil
	}
	return selected, nil
}
