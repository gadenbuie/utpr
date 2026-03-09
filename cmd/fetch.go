package cmd

import (
	"fmt"

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
		return err
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
		return err
	}
	sourceRepo, err := remote.ParseRepoSpec(sourceURL)
	if err != nil {
		return err
	}

	pr, err := gh.GetPR(sourceRepo, prNumber)
	if err != nil {
		return ui.Dief("Failed to fetch PR #%d details.", prNumber)
	}

	headRef := pr.Head.Ref
	isForkPR := pr.Head.Repo.FullName != pr.Base.Repo.FullName
	remoteName := cfg.SourceRemote
	localBranch := headRef

	if isForkPR {
		remoteName = pr.Head.Repo.Owner.Login
		// Add remote if needed
		if _, err := git.Run("remote", "get-url", remoteName); err != nil {
			ui.Infof("Adding remote '%s' for '%s'.", remoteName, pr.Head.Repo.FullName)
			git.Run("remote", "add", remoteName, pr.Head.Repo.CloneURL)
			git.MarkRemoteCreatedByUtpr(remoteName)
		}
	}

	// If a local branch already tracks this remote ref, reuse it.
	if existing := git.FindBranchByUpstream(remoteName, headRef); existing != "" {
		localBranch = existing
	} else {
		// Use a pr/{number}-{author}-{branch} local name when the current user
		// is not the PR author, so it's clear whose work you're looking at.
		currentUser, _ := gh.GetLogin()
		isOwnPR := currentUser != "" && pr.User.Login == currentUser
		if !isOwnPR {
			localBranch = fmt.Sprintf("pr/%d-%s-%s", prNumber, pr.User.Login, headRef)
		}
	}

	err = ui.Spin(
		fmt.Sprintf("Fetching '%s' from %s...", headRef, remoteName),
		func() error {
			return git.Fetch(remoteName, headRef)
		},
	)
	if err != nil {
		return err
	}

	if flagFetchWorktree {
		if git.BranchExists(localBranch) {
			git.Run("fetch", ".", "FETCH_HEAD:"+localBranch)
		} else {
			git.Run("branch", localBranch, "FETCH_HEAD")
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
		if err := git.SwitchBranch(localBranch); err != nil {
			return err
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

func configureFetchedBranch(localBranch, remoteName, headRef, prURL string) {
	git.SetBranchUpstream(localBranch, remoteName+"/"+headRef)
	git.MarkBranchCreatedByUtpr(localBranch)
	git.SetBranchPRURL(localBranch, prURL)
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
