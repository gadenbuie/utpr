package ui

import (
	"os"

	"golang.org/x/term"
)

// TTY detection. Package-level so tests can stub them without a real terminal.
var (
	stdoutIsTTY = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }
	stdinIsTTY  = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
)

// StdoutIsTTY reports whether stdout is attached to a terminal.
func StdoutIsTTY() bool { return stdoutIsTTY() }

// StdinIsTTY reports whether stdin is attached to a terminal.
func StdinIsTTY() bool { return stdinIsTTY() }

// SetTTYFuncs overrides TTY detection for tests. Returns a restore function.
func SetTTYFuncs(stdout, stdin func() bool) func() {
	oldStdout, oldStdin := stdoutIsTTY, stdinIsTTY
	stdoutIsTTY, stdinIsTTY = stdout, stdin
	return func() { stdoutIsTTY, stdinIsTTY = oldStdout, oldStdin }
}

// plainMode routes output for non-terminal consumers: no spinners, no
// alt-screen pager, and status messages go to stdout without styling.
var plainMode bool

// SetPlainMode enables plain output for the rest of the process.
func SetPlainMode(plain bool) { plainMode = plain }

// PlainMode reports whether plain output is active.
func PlainMode() bool { return plainMode }

// AgentMode decides whether output should be plain for agent consumption.
// pretty forces styled output, agent forces plain output, and with neither
// set, output is plain whenever stdout is not a terminal.
func AgentMode(agent, pretty bool) bool {
	if pretty {
		return false
	}
	return agent || !stdoutIsTTY()
}

// RequireInteractiveTTY returns an error explaining how to avoid an
// interactive prompt when stdin is not attached to a terminal.
func RequireInteractiveTTY(guidance string) error {
	if stdinIsTTY() {
		return nil
	}
	return Die("This prompt requires an interactive terminal; " + guidance)
}

// requireTTY is the shared guard for interactive prompts that have no
// command-specific guidance.
func requireTTY() error {
	return Die("This prompt requires an interactive terminal; pass --yes or explicit arguments instead")
}
