package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
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

	items := parseSymlinkItems(symlinkDirs)
	existsInRepo := func(name string) bool {
		_, err := os.Stat(filepath.Join(repoRoot, name))
		return err == nil
	}
	existsInWorktree := func(name string) bool {
		_, err := os.Stat(filepath.Join(wtDir, name))
		return err == nil
	}
	available := computeSymlinkCandidates(items, existsInRepo, existsInWorktree)

	if len(available) == 0 {
		return
	}

	opts := make([]huh.Option[string], len(available))
	for i, item := range available {
		opts[i] = huh.NewOption(item, item).Selected(true)
	}

	selected, err := ui.ChooseMultiWithOptions("Symlink into worktree:", opts)
	if err != nil || len(selected) == 0 {
		return
	}

	for _, item := range selected {
		src := filepath.Join(repoRoot, item)
		dst := filepath.Join(wtDir, item)

		os.MkdirAll(filepath.Dir(dst), 0o755)

		if err := os.Symlink(src, dst); err != nil {
			ui.Warnf("Failed to symlink %s.", item)
		} else {
			ui.Successf("Symlinked %s", item)
		}
	}
}

// setupHook defines a project-specific setup command to run in a worktree.
type setupHook struct {
	trigger string   // file that must exist in wtDir to trigger this hook
	tool    string   // binary name that must be on PATH
	spinMsg string   // spinner message
	warnMsg string   // warning on failure
	args    []string // command + args
}

var defaultSetupHooks = []setupHook{
	{
		trigger: "package.json",
		tool:    "npm",
		spinMsg: "Running npm install...",
		warnMsg: "npm install failed (non-fatal).",
		args:    []string{"npm", "install"},
	},
	{
		trigger: "pyproject.toml",
		tool:    "uv",
		spinMsg: "Running uv sync...",
		warnMsg: "uv sync failed (non-fatal).",
		args:    []string{"uv", "sync", "--all-groups"},
	},
	{
		trigger: "renv.lock",
		tool:    "Rscript",
		spinMsg: "Running renv::restore()...",
		warnMsg: "renv::restore() failed (non-fatal).",
		args:    []string{"Rscript", "-e", "renv::restore(prompt = FALSE)"},
	},
}

// runWorktreeSetup runs project-specific setup commands in the worktree.
func runWorktreeSetup(wtDir string) {
	for _, hook := range defaultSetupHooks {
		if _, err := os.Stat(filepath.Join(wtDir, hook.trigger)); err != nil {
			continue
		}
		if _, err := exec.LookPath(hook.tool); err != nil {
			continue
		}
		if err := ui.Spin(hook.spinMsg, func() error {
			cmd := exec.Command(hook.args[0], hook.args[1:]...)
			cmd.Dir = wtDir
			return cmd.Run()
		}); err != nil {
			ui.Warn(hook.warnMsg)
		}
	}

	if makefile := filepath.Join(wtDir, "Makefile"); fileHasTarget(makefile, "setup") {
		if _, err := exec.LookPath("make"); err == nil {
			if err := ui.Spin("Running make setup...", func() error {
				cmd := exec.Command("make", "setup")
				cmd.Dir = wtDir
				return cmd.Run()
			}); err != nil {
				ui.Warn("make setup failed (non-fatal).")
			}
		}
	}
}

// fileHasTarget checks if a Makefile exists and contains a given target.
func fileHasTarget(path, target string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, target+":") {
			return true
		}
	}
	return false
}

func parseSymlinkItems(symlinkDirs string) []string {
	var items []string
	for _, item := range strings.Split(symlinkDirs, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}

func computeSymlinkCandidates(
	items []string,
	existsInRepo func(string) bool,
	existsInWorktree func(string) bool,
) []string {
	var candidates []string
	for _, item := range items {
		if !existsInRepo(item) {
			continue
		}
		if existsInWorktree(item) {
			if item == ".claude" && existsInRepo(".claude/settings.json") && !existsInWorktree(".claude/settings.json") {
				candidates = append(candidates, ".claude/settings.json")
			}
			continue
		}
		candidates = append(candidates, item)
	}
	return candidates
}
