package cmd

import (
	"encoding/json"
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

var flagWorktreeListJSON bool

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
	worktreeListCmd.Flags().BoolVar(&flagWorktreeListJSON, "json", false, "Output worktrees as JSON")
	worktreeCmd.AddCommand(worktreeListCmd)
	worktreeCmd.AddCommand(worktreeRemoveCmd)
	worktreeCmd.AddCommand(worktreeOpenCmd)
}

func runWorktreeList(cmd *cobra.Command, args []string) error {
	worktrees, err := git.WorktreeList()
	if err != nil {
		return ui.Die(err.Error())
	}
	if flagWorktreeListJSON {
		return writeWorktreeJSON(worktrees)
	}
	if len(worktrees) == 0 {
		ui.Info("No worktrees found.")
		return nil
	}

	mainBranch := "(detached HEAD)"
	if worktrees[0].Branch != "" {
		mainBranch = ui.StyleBranchName(strings.TrimPrefix(worktrees[0].Branch, "refs/heads/"))
	}
	fmt.Println(ui.StyleBold.Render("Main repo"))
	fmt.Println("  " + ui.StyleLabel.Render("Path:") + " " + shortenHomeDir(worktrees[0].Path))
	fmt.Println("  " + ui.StyleLabel.Render("Branch:") + " " + mainBranch)

	if len(worktrees) == 1 {
		return nil
	}

	type row struct {
		branch string
		path   string
		prURL  string
	}

	rows := make([]row, len(worktrees)-1)
	branchWidth := 0
	for i, wt := range worktrees[1:] {
		branch := strings.TrimPrefix(wt.Branch, "refs/heads/")
		rows[i] = row{
			branch: branch,
			path:   shortenHomeDir(wt.Path),
			prURL:  git.GetBranchPRURL(branch),
		}
		if w := len([]rune(branch)); w > branchWidth {
			branchWidth = w
		}
	}

	fmt.Println()
	fmt.Println(ui.StyleBold.Render("Worktrees:"))
	for _, r := range rows {
		line := "  " + ui.PadRight(ui.StyleBranchName(r.branch), branchWidth) + "  " + r.path
		if r.prURL != "" {
			line += "  " + ui.StyleMuted.Render(r.prURL)
		}
		fmt.Println(line)
	}
	return nil
}

type worktreeJSONEntry struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Head   string `json:"head"`
	PRURL  string `json:"pr_url,omitempty"`
	IsMain bool   `json:"is_main"`
}

func writeWorktreeJSON(worktrees []git.Worktree) error {
	entries := worktreeJSONEntries(worktrees, git.GetBranchPRURL)
	return json.NewEncoder(os.Stdout).Encode(entries)
}

func worktreeJSONEntries(worktrees []git.Worktree, prURL func(string) string) []worktreeJSONEntry {
	entries := make([]worktreeJSONEntry, 0, len(worktrees))
	for i, wt := range worktrees {
		branch := strings.TrimPrefix(wt.Branch, "refs/heads/")
		entries = append(entries, worktreeJSONEntry{
			Path:   wt.Path,
			Branch: branch,
			Head:   wt.HEAD,
			PRURL:  prURL(branch),
			IsMain: i == 0,
		})
	}
	return entries
}

// shortenHomeDir replaces the user's home directory prefix with "~".
func shortenHomeDir(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + path[len(home):]
	}
	return path
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
