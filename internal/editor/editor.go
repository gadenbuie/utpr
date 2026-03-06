package editor

import (
	"os"
	"os/exec"
	"strings"

	"github.com/gadenbuie/utpr/internal/ui"
)

// AutoDetect determines the user's editor from environment variables.
// Returns empty string if no editor can be detected.
func AutoDetect() string {
	if e := os.Getenv("UTPR_EDITOR"); e != "" {
		return e
	}
	if os.Getenv("POSITRON") == "1" {
		return "positron"
	}
	if os.Getenv("CURSOR_TRACE_ID") != "" || os.Getenv("CURSOR_SESSION_ID") != "" {
		return "cursor"
	}
	if os.Getenv("WINDSURF") != "" || os.Getenv("CODEIUM_WIND_SURF") != "" {
		return "windsurf"
	}
	if os.Getenv("ZED_SESSION") != "" || os.Getenv("TERM_PROGRAM") == "zed" {
		return "zed"
	}
	if os.Getenv("TERM_PROGRAM") == "vscode" {
		if strings.Contains(os.Getenv("VSCODE_GIT_IPC_HANDLE"), "Code - Insiders") {
			return "code-insiders"
		}
		return "code"
	}
	if os.Getenv("VSCODE_GIT_IPC_HANDLE") != "" || os.Getenv("VSCODE_PID") != "" {
		if strings.Contains(os.Getenv("VSCODE_GIT_IPC_HANDLE"), "Code - Insiders") {
			return "code-insiders"
		}
		return "code"
	}
	return ""
}

// Open opens a directory or file in the given editor command.
func Open(editorSpec, targetPath string) error {
	parts := strings.Fields(editorSpec)
	if len(parts) == 0 {
		ui.Warn("No editor command provided.")
		return nil
	}

	if _, err := exec.LookPath(parts[0]); err != nil {
		ui.Warnf("Editor command '%s' not found; skipping open.", parts[0])
		return nil
	}

	args := make([]string, len(parts)-1, len(parts))
	copy(args, parts[1:])
	args = append(args, targetPath)
	cmd := exec.Command(parts[0], args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		ui.Warnf("Failed to open in %s.", editorSpec)
		return err
	}

	ui.Successf("Opened in %s.", editorSpec)
	return nil
}

// AvailableEditors returns a list of known editors that are installed.
func AvailableEditors() []string {
	candidates := []string{"code", "code-insiders", "cursor", "positron", "windsurf", "zed"}
	var available []string
	for _, cmd := range candidates {
		if _, err := exec.LookPath(cmd); err == nil {
			available = append(available, cmd)
		}
	}
	return available
}
