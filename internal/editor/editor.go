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
	return autoDetectFromEnv(os.Getenv)
}

func autoDetectFromEnv(getenv func(string) string) string {
	if e := getenv("UTPR_EDITOR"); e != "" {
		return e
	}
	if getenv("POSITRON") == "1" {
		return "positron"
	}
	if getenv("CURSOR_TRACE_ID") != "" || getenv("CURSOR_SESSION_ID") != "" {
		return "cursor"
	}
	if getenv("WINDSURF") != "" || getenv("CODEIUM_WIND_SURF") != "" {
		return "windsurf"
	}
	if getenv("ZED_SESSION") != "" || getenv("TERM_PROGRAM") == "zed" {
		return "zed"
	}
	if getenv("TERM_PROGRAM") == "vscode" {
		if strings.Contains(getenv("VSCODE_GIT_IPC_HANDLE"), "Code - Insiders") {
			return "code-insiders"
		}
		return "code"
	}
	if getenv("VSCODE_GIT_IPC_HANDLE") != "" || getenv("VSCODE_PID") != "" {
		if strings.Contains(getenv("VSCODE_GIT_IPC_HANDLE"), "Code - Insiders") {
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
