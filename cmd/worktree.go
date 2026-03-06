package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gadenbuie/utpr/internal/editor"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/ui"
)

// initWorktree creates a worktree for the given branch, symlinks dirs,
// runs setup, and opens in editor.
func initWorktree(branch string) error {
	repoRoot, err := git.GetTopLevel()
	if err != nil {
		return err
	}
	wtDir, err := git.GetWorktreeDir(branch)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(wtDir), 0o755); err != nil {
		return err
	}
	if err := git.WorktreeAdd(wtDir, branch); err != nil {
		return err
	}
	ui.Successf("Created worktree at %s", wtDir)

	symlinkWorktreeDirs(repoRoot, wtDir)
	runWorktreeSetup(wtDir)

	ed := editor.AutoDetect()
	if ed != "" {
		editor.Open(ed, wtDir)
	}
	ui.Infof("cd \"%s\"", wtDir)
	return nil
}

// offerWorktreeNavigation presents options for navigating to an existing worktree.
func offerWorktreeNavigation(targetPath string) {
	ed := editor.AutoDetect()

	var options []string
	if ed != "" {
		options = []string{"Open in " + ed, "Show path", "Do nothing"}
	} else {
		options = []string{"Show path", "Open in editor...", "Do nothing"}
	}

	choice, err := ui.Choose("Worktree: "+targetPath, options)
	if err != nil {
		return
	}

	switch {
	case choice == "Open in editor...":
		pickAndOpenEditor(targetPath)
	case strings.HasPrefix(choice, "Open in "):
		editor.Open(ed, targetPath)
	case choice == "Show path":
		ui.Infof("cd \"%s\"", targetPath)
	}
}

func pickAndOpenEditor(targetPath string) {
	editors := editor.AvailableEditors()
	editors = append(editors, "Custom...")

	choice, err := ui.Choose("Select editor:", editors)
	if err != nil {
		return
	}

	if choice == "Custom..." {
		choice, err = ui.Input("Editor command:", "", "editor command (optional args)")
		if err != nil || choice == "" {
			return
		}
	}
	editor.Open(choice, targetPath)
}

// symlinkWorktreeDirs symlinks configured dirs from main repo into worktree.
func symlinkWorktreeDirs(repoRoot, wtDir string) {
	symlinkDirs := os.Getenv("UTPR_SYMLINK_DIRS")
	if symlinkDirs == "" {
		symlinkDirs = "_dev,.claude,.env,.env.local,.Renviron,.Rprofile,.agents,.secrets,secrets,.htpasswd,.vscode,.vscode/settings.json"
	}

	var available []string
	for _, item := range strings.Split(symlinkDirs, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		srcPath := filepath.Join(repoRoot, item)
		dstPath := filepath.Join(wtDir, item)

		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			continue
		}

		if _, err := os.Stat(dstPath); err == nil {
			// Already exists in worktree (tracked by git)
			// Special case: .claude dir exists but settings.json doesn't
			if item == ".claude" {
				settingsSrc := filepath.Join(repoRoot, ".claude/settings.json")
				settingsDst := filepath.Join(wtDir, ".claude/settings.json")
				if _, err := os.Stat(settingsSrc); err == nil {
					if _, err := os.Stat(settingsDst); os.IsNotExist(err) {
						available = append(available, ".claude/settings.json")
					}
				}
			}
			continue
		}
		available = append(available, item)
	}

	if len(available) == 0 {
		return
	}

	// For now, symlink all available items (the bash version uses gum choose --no-limit
	// with all pre-selected; we'll do the same by default)
	for _, item := range available {
		src := filepath.Join(repoRoot, item)
		dst := filepath.Join(wtDir, item)

		// Ensure parent directory exists
		os.MkdirAll(filepath.Dir(dst), 0o755)

		if err := os.Symlink(src, dst); err != nil {
			ui.Warnf("Failed to symlink %s.", item)
		} else {
			ui.Successf("Symlinked %s", item)
		}
	}
}

// runWorktreeSetup runs project-specific setup commands in the worktree.
func runWorktreeSetup(wtDir string) {
	if _, err := os.Stat(filepath.Join(wtDir, "package.json")); err == nil {
		ui.Spin("Running npm install...", func() error {
			cmd := exec.Command("npm", "install")
			cmd.Dir = wtDir
			cmd.Stdout = nil
			cmd.Stderr = nil
			return cmd.Run()
		})
	}
}
