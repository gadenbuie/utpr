package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var worktreeCmd = &cobra.Command{
	Use:   "worktree",
	Short: "Manage git worktrees",
}

var worktreeListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List git worktrees",
	RunE:    runWorktreeList,
}

var worktreeRemoveCmd = &cobra.Command{
	Use:     "remove [branch]",
	Aliases: []string{"rm"},
	Short:   "Remove a branch's worktree",
	Long:    "Remove a branch's worktree, optionally deleting the branch afterward.",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runWorktreeRemove,
}

var worktreeOpenCmd = &cobra.Command{
	Use:   "open [branch]",
	Short: "Navigate to a branch's existing worktree",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorktreeOpen,
}

func init() {
	worktreeCmd.AddCommand(worktreeListCmd)
	worktreeCmd.AddCommand(worktreeRemoveCmd)
	worktreeCmd.AddCommand(worktreeOpenCmd)
}

func runWorktreeList(cmd *cobra.Command, args []string) error {
	worktrees, err := git.WorktreeList()
	if err != nil {
		return ui.Die(err.Error())
	}
	if len(worktrees) == 0 {
		ui.Info("No worktrees found.")
		return nil
	}

	type row struct {
		branch string
		path   string
		tag    string
		prURL  string
	}

	rows := make([]row, len(worktrees))
	branchWidth := 0
	for i, wt := range worktrees {
		branch := strings.TrimPrefix(wt.Branch, "refs/heads/")
		r := row{branch: branch, path: wt.Path}
		if i == 0 {
			r.tag = "main"
		} else {
			r.prURL = git.GetBranchPRURL(branch)
		}
		rows[i] = r
		if w := len([]rune(branch)); w > branchWidth {
			branchWidth = w
		}
	}

	for _, r := range rows {
		line := ui.PadRight(ui.StyleBranchName(r.branch), branchWidth) + "  " + r.path
		if r.tag != "" {
			line += "  " + ui.StyleMuted.Render("["+r.tag+"]")
		}
		if r.prURL != "" {
			line += "  " + ui.StyleMuted.Render(r.prURL)
		}
		fmt.Println(line)
	}
	return nil
}

func runWorktreeRemove(cmd *cobra.Command, args []string) error {
	if git.IsInWorktree() {
		mainRoot, _ := git.GetMainRepoRoot()
		ui.Warn("Navigate to the main repo first:")
		fmt.Fprintf(os.Stderr, "  cd \"%s\"\n", mainRoot)
		fmt.Fprintf(os.Stderr, "  utpr worktree remove\n")
		return fmt.Errorf("cannot run from a worktree")
	}

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
		branch, err = pickWorktreeBranch("Select a worktree to remove:")
		if err != nil {
			return err
		}
	}

	if git.GetBranchWorktreePath(branch) == "" {
		return ui.Dief("Branch '%s' does not have a worktree.", branch)
	}

	if err := removeWorktree(branch); err != nil {
		return err
	}

	deleteBranch, err := ui.Confirm("Also delete local branch '"+branch+"'?", false)
	if err != nil {
		return err
	}
	if !deleteBranch {
		return nil
	}

	if branch == cfg.DefaultBranch {
		return ui.Dief("Cannot delete the default branch '%s'.", cfg.DefaultBranch)
	}
	if _, err := challengeLocalBranchDelete(branch); err != nil {
		return err
	}
	if err := git.DeleteBranch(branch); err != nil {
		return ui.Die(err.Error())
	}
	ui.Successf("Deleted local branch '%s'.", branch)
	remote.CleanupUtprRemotes()
	return nil
}

func runWorktreeOpen(cmd *cobra.Command, args []string) error {
	var branch string
	var err error
	if len(args) > 0 {
		branch = args[0]
		if err := git.ValidateBranchName(branch); err != nil {
			return ui.Die(err.Error())
		}
	} else {
		branch, err = pickWorktreeBranch("Select a worktree to open:")
		if err != nil {
			return err
		}
	}

	wtPath := git.GetBranchWorktreePath(branch)
	if wtPath == "" {
		return ui.Dief(
			"Branch '%s' does not have a worktree. Use 'utpr resume %s --worktree' or 'utpr fetch --worktree' to create one.",
			branch, branch,
		)
	}

	offerWorktreeNavigation(wtPath)
	return nil
}

// pickWorktreeBranch shows an interactive picker of branches that have a worktree.
func pickWorktreeBranch(header string) (string, error) {
	worktrees, err := git.WorktreeList()
	if err != nil {
		return "", ui.Die(err.Error())
	}

	var items []ui.BranchPickerItem
	for i, wt := range worktrees {
		if i == 0 {
			continue
		}
		items = append(items, ui.BranchPickerItem{
			Name:        strings.TrimPrefix(wt.Branch, "refs/heads/"),
			HasWorktree: true,
		})
	}
	if len(items) == 0 {
		return "", ui.Die("No worktrees found.")
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
