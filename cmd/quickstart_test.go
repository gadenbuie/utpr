package cmd

import (
	"strings"
	"testing"

	"github.com/gadenbuie/utpr/internal/ui"
)

// runQuickstartCaptured runs the quickstart command with the given stdout-TTY
// state and flags, returning everything it wrote to stdout.
func runQuickstartCaptured(t *testing.T, stdoutTTY bool, args []string) string {
	t.Helper()
	restoreTTY := ui.SetTTYFuncs(func() bool { return stdoutTTY }, func() bool { return true })
	t.Cleanup(restoreTTY)
	t.Cleanup(func() { ui.SetPlainMode(false) })

	if err := quickstartCmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags(%v) failed: %v", args, err)
	}
	defer resetPlainFlags(quickstartCmd)

	initPlainMode(quickstartCmd)

	return captureStdout(t, func() {
		if err := runQuickstart(quickstartCmd, nil); err != nil {
			t.Errorf("runQuickstart() failed: %v", err)
		}
	})
}

// collapseSpace joins wrapped lines so content assertions match phrases
// that are line-wrapped in the markdown source.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func TestQuickstartVariantSelection(t *testing.T) {
	tests := []struct {
		name      string
		stdoutTTY bool
		args      []string
		wantAgent bool
	}{
		{"tty default shows human guide", true, nil, false},
		{"pipe default shows agent guide", false, nil, true},
		{"tty --agent shows agent guide", true, []string{"--agent"}, true},
		{"pipe --pretty shows human guide", false, []string{"--pretty"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := runQuickstartCaptured(t, tt.stdoutTTY, tt.args)
			plain := collapseSpace(ui.StripANSI(out))

			// "pre-trimmed" appears only in the agent guide.
			if gotAgent := strings.Contains(plain, "pre-trimmed"); gotAgent != tt.wantAgent {
				t.Errorf("agent guide shown = %v, want %v", gotAgent, tt.wantAgent)
			}
			if gotHuman := strings.Contains(plain, "The PR lifecycle"); gotHuman == tt.wantAgent {
				t.Errorf("human guide shown = %v, want %v", gotHuman, !tt.wantAgent)
			}
			if !strings.HasSuffix(out, "\n") {
				t.Error("quickstart output should end with a newline")
			}
		})
	}
}

func TestQuickstartHumanGuideContent(t *testing.T) {
	out := collapseSpace(ui.StripANSI(runQuickstartCaptured(t, true, nil)))

	// The human guide is a lifecycle tour: each core command appears with
	// an example, plus pointers to per-command help and the README.
	for _, want := range []string{
		"utpr init my-feature",
		"utpr push",
		"utpr status",
		"utpr view",
		"utpr ci",
		"utpr finish",
		"utpr <command> --help",
		"README",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("human guide missing expected content %q", want)
		}
	}
}

func TestQuickstartAgentGuideContent(t *testing.T) {
	out := collapseSpace(ui.StripANSI(runQuickstartCaptured(t, false, nil)))

	// The agent guide covers the agent-facing surface: CI commands and
	// their non-interactive forms, output-mode composition, prompt
	// avoidance, and the pre-trimmed output note.
	for _, want := range []string{
		"utpr ci --wait",
		"utpr ci logs",
		"utpr ci list",
		"failure reason",
		"--agent",
		"--pretty",
		"utpr status --json",
		"utpr worktree list --json",
		"--yes",
		"non-interactive",
		"head",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("agent guide missing expected content %q", want)
		}
	}
}
