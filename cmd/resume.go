package cmd

import (
	"errors"
	"strings"

	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var resumeCmd = &cobra.Command{
	Use:   "resume [pr-number-or-branch]",
	Short: "Resume work on a PR branch",
	Long:  "Resume work on a PR branch. Accepts a branch name or a PR number ('461' or '#461') of a utpr-tracked branch. If no argument is given, an interactive picker is shown.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runResume,
}

var (
	flagResumeWorktree bool
	flagResumeYes      bool
)

func init() {
	resumeCmd.Flags().BoolVar(&flagResumeWorktree, "worktree", false, "Move the branch into a new git worktree")
	resumeCmd.Flags().BoolVar(&flagResumeYes, "yes", false, "Assume yes for confirmation prompts")
}

func runResume(cmd *cobra.Command, args []string) error {
	// Resuming a local branch works without a remote; remote operations
	// below skip or warn when no remote is available.
	cfg, err := remote.Detect()
	if err != nil {
		if errors.Is(err, ui.ErrCancelled) {
			return err
		}
		cfg = nil
		ui.Warnf("Continuing without remote operations: %v", err)
	}

	var branch string
	if len(args) > 0 {
		branch = args[0]
		// A numeric argument is a PR number, unless a local branch literally
		// bears that name — the branch wins. Prefixing with '#' always means
		// the PR number.
		if n, ok := parsePRNumberArg(branch); ok && !git.BranchExists(branch) {
			resolved, rerr := resolveBranchForPRNumber(n, cfg)
			if rerr != nil {
				return rerr
			}
			branch = resolved
		} else if err := git.ValidateBranchName(branch); err != nil {
			return ui.Die(err.Error())
		}
	} else {
		defaultBranch := ""
		if cfg != nil {
			defaultBranch = cfg.DefaultBranch
		} else {
			defaultBranch = git.GetLocalDefaultBranch()
		}
		branch, err = pickBranch(defaultBranch, "Select a branch to resume:")
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

	if flagResumeWorktree {
		if err := promoteBranchToWorktree(cfg, branch); err != nil {
			return err
		}
		wtDir, err := git.GetWorktreeDir(branch)
		if err != nil {
			return ui.Die(err.Error())
		}
		tracking := git.GetTrackingBranchInDir(wtDir)
		if tracking != "" {
			err := ui.Spin("Pulling from "+tracking+"...", func() error {
				_, pullErr := git.RunInDir(wtDir, "pull")
				return pullErr
			})
			if err != nil {
				ui.Warn("Could not pull latest changes. Run 'git pull' to update.")
			}
		}
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

// resolveBranchForPRNumber resolves a PR number to the local branch whose
// stored PR URL references that PR. Only utpr-tracked branches carry this
// URL (stored in git config by init/fetch/push). cfg may be nil, in which
// case branches from any repository are considered. Returns an error for
// zero or multiple matches.
func resolveBranchForPRNumber(prNumber int, cfg *remote.Config) (string, error) {
	sourceRepo := ""
	if cfg != nil {
		if sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote); err == nil {
			sourceRepo, _ = remote.ParseRepoSpec(sourceURL)
		}
	}
	matches, err := branchesWithPRNumber(prNumber, sourceRepo)
	if err != nil {
		return "", err
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", ui.Dief("No local branch found for PR #%d (only utpr-tracked branches can be resumed by number). Use 'utpr fetch %d' to fetch it from GitHub, or pass a branch name.", prNumber, prNumber)
	default:
		return "", ui.Dief("Multiple local branches reference PR #%d (%s). Pass a branch name to choose one.", prNumber, strings.Join(matches, ", "))
	}
}

// pickBranch shows an interactive branch picker, excluding the default branch.
func pickBranch(defaultBranch, header string) (string, error) {
	if err := requireInteractiveTTY("pass a branch name as an argument"); err != nil {
		return "", err
	}

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
