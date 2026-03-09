# Developer Guide

This guide covers how to build, test, and contribute to utpr.
It assumes basic terminal comfort but no prior Go experience.

## Prerequisites

- **Go 1.25+** — install from <https://go.dev/dl/> or via your
  package manager (`brew install go`, `sudo apt install golang`, etc.)
- **git** and **gh** — utpr shells out to both at runtime

Verify your setup:

```bash
go version   # go1.25.5 or later
git --version
gh --version
```

## Quick start

```bash
# Clone the repo
git clone https://github.com/gadenbuie/utpr.git
cd utpr

# Build and run
go build -o utpr .
./utpr --help

# Or run directly without building a binary
go run . --help

# Run all tests
go test ./...
```

## Project layout

```
utpr/
├── main.go              # Entry point — sets version info, calls cmd.Execute()
├── go.mod               # Module definition and dependencies
├── cmd/                 # CLI commands (one file per command)
│   ├── root.go          # Root command, prerequisite checks
│   ├── init.go          # utpr init
│   ├── push.go          # utpr push
│   ├── fetch.go         # utpr fetch
│   ├── finish.go        # utpr finish
│   ├── forget.go        # utpr forget
│   ├── pause.go         # utpr pause
│   ├── resume.go        # utpr resume
│   ├── pull.go          # utpr pull
│   ├── mergemain.go     # utpr merge-main
│   ├── view.go          # utpr view
│   ├── bisect.go        # utpr bisect
│   ├── browser.go       # Open-in-browser helpers
│   ├── helpers.go        # Shared helpers (challenge prompts, pull default branch)
│   ├── worktree.go      # Worktree creation and symlink logic
│   └── *_test.go        # Tests for command logic
├── internal/            # Internal packages (not importable by others)
│   ├── git/             # Git operations (run commands, parse output)
│   │   ├── git.go       # Core: Run(), RunSilent(), RunInteractive()
│   │   ├── config.go    # Git config helpers (get/set branch metadata)
│   │   └── worktree.go  # Worktree path discovery
│   ├── gh/              # GitHub CLI wrapper (REST/GraphQL clients, auth checks)
│   │   └── client.go
│   ├── remote/          # Remote detection (ours vs fork layout)
│   │   └── remote.go
│   ├── ui/              # User interface (all output goes to stderr)
│   │   ├── output.go    # Info/Warn/Error/Success styled output
│   │   ├── prompt.go    # Interactive prompts (confirm, input, select)
│   │   └── spinner.go   # Spinner for long operations
│   └── editor/          # Editor detection and worktree opening
│       └── editor.go
├── utpr.bash            # Original bash implementation (for reference)
└── _dev/                # Development notes and plans
```

## Key concepts for Go newcomers

### Modules and dependencies

Go uses **modules** for dependency management. The `go.mod` file at the
repo root declares the module path and all dependencies with exact versions.
`go.sum` contains checksums for verification.

```bash
# Download dependencies (usually automatic)
go mod download

# Add a new dependency (just import it in code, then run)
go mod tidy

# See all dependencies
go list -m all
```

You don't need a separate install step — `go build` and `go test`
automatically download missing dependencies.

### Packages

Every directory under the module is a **package**. All `.go` files in a
directory share the same `package` declaration and can see each other's
unexported (lowercase) identifiers.

- `cmd/` — package `cmd`, contains all CLI command definitions
- `internal/git/` — package `git`, wraps git operations
- `internal/ui/` — package `ui`, handles all terminal output

The `internal/` directory is special in Go: packages under it can only be
imported by code within this module. This prevents external users from
depending on our internal APIs.

### Error handling

Go doesn't have exceptions. Functions that can fail return an `error` as
their last return value. The standard pattern is:

```go
result, err := someFunction()
if err != nil {
    return fmt.Errorf("context about what failed: %w", err)
}
```

The `%w` verb wraps the original error so callers can inspect it with
`errors.Is()` or `errors.Unwrap()`.

### Building

```bash
# Development build
go build -o utpr .

# Build with version info (how releases are built)
go build -ldflags "-X main.version=1.0.0 -X main.commit=$(git rev-parse HEAD) -X main.date=$(date -u +%Y-%m-%d)" -o utpr .
```

## How things work

### CLI framework: cobra

utpr uses [cobra](https://github.com/spf13/cobra) for command-line parsing.
Each command is a `*cobra.Command` registered in `cmd/root.go`:

```go
// In cmd/root.go init()
rootCmd.AddCommand(initCmd)
rootCmd.AddCommand(pushCmd)
// ...
```

Each command file defines its command variable and a `runXxx` function:

```go
// In cmd/push.go
var pushCmd = &cobra.Command{
    Use:   "push",
    Short: "Push branch and create/update PR",
    RunE:  runPush,
}

func runPush(cmd *cobra.Command, args []string) error {
    // implementation
}
```

Flags are registered in `init()` functions within each command file.

### Output and UI

**All user-facing output goes to stderr** via the `ui` package. This keeps
stdout clean for machine-readable output.

```go
ui.Info("Starting operation...")    // ℹ blue
ui.Warn("Something to note")       // ⚠ yellow
ui.Error("Something went wrong")   // ✖ red
ui.Success("Done!")                 // ✔ green
```

Interactive prompts use [charmbracelet/huh](https://github.com/charmbracelet/huh):

```go
ui.MustConfirm("Proceed?", false)  // yes/no, returns error if declined
ui.Input("Branch name", "")        // text input
ui.Select("Pick a PR", options)    // selection list
```

Long operations are wrapped in a spinner:

```go
ui.Spin("Fetching...", func() error {
    return git.Fetch(remote, branch)
})
```

### Git operations

The `internal/git` package wraps all git commands. It never imports `os/exec`
directly in command files — always go through the `git` package:

```go
// Run a git command and get stdout
output, err := git.Run("status", "--porcelain")

// Run interactively (stdin/stdout/stderr connected to terminal)
// Used for git merge where conflict output must be visible
err := git.RunInteractive("merge", branch)
```

### Remote detection

The `remote` package detects whether you're working in an "ours" (direct push
access) or "fork" layout. This is cached after the first call:

```go
cfg, err := remote.Detect()
// cfg.Layout: "ours" or "fork"
// cfg.SourceRemote: where to pull from
// cfg.PushRemote: where to push to
// cfg.DefaultBranch: e.g. "main"
```

### Fatal errors

Use `ui.Die()` for unrecoverable errors. It prints a styled error message
and returns an error that propagates up to `main()`:

```go
if !git.IsInsideWorkTree() {
    return ui.Die("Not inside a git repository.")
}
```

## Testing

```bash
# Run all tests
go test ./...

# Run tests in a specific package
go test ./cmd/
go test ./internal/ui/

# Run a specific test by name
go test ./cmd/ -run TestIssueToSlug

# Run tests with verbose output
go test -v ./...

# Run tests with coverage
go test -cover ./...
```

Tests live alongside the code they test, in files named `*_test.go`.
Go's testing convention uses table-driven tests:

```go
func TestSomething(t *testing.T) {
    tests := []struct {
        name  string
        input string
        want  string
    }{
        {"basic", "hello", "HELLO"},
        {"empty", "", ""},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := Transform(tt.input)
            if got != tt.want {
                t.Errorf("Transform(%q) = %q, want %q", tt.input, got, tt.want)
            }
        })
    }
}
```

## Adding a new command

1. Create `cmd/mycommand.go`:

```go
package cmd

import "github.com/spf13/cobra"

var mycommandCmd = &cobra.Command{
    Use:   "mycommand",
    Short: "One-line description",
    Long:  "Longer description shown in --help.",
    RunE:  runMycommand,
}

func init() {
    // Register flags here if needed
    // mycommandCmd.Flags().BoolVar(&flagVerbose, "verbose", false, "verbose output")
}

func runMycommand(cmd *cobra.Command, args []string) error {
    cfg, err := remote.Detect()
    if err != nil {
        return err
    }

    // Implementation here
    ui.Success("Done!")
    return nil
}
```

2. Register it in `cmd/root.go`:

```go
rootCmd.AddCommand(mycommandCmd)
```

3. Add a test in `cmd/mycommand_test.go` if the command has testable logic.

## Common tasks

```bash
# Format code (Go has one standard style)
gofmt -w .

# Run the linter
go vet ./...

# Update dependencies
go get -u ./...
go mod tidy

# See what would change with a dependency update
go list -u -m all
```

## Dependencies

| Package | Purpose |
|---------|---------|
| [spf13/cobra](https://github.com/spf13/cobra) | CLI framework (commands, flags, help text) |
| [charmbracelet/huh](https://github.com/charmbracelet/huh) | Interactive prompts (confirm, input, select) |
| [charmbracelet/huh/spinner](https://github.com/charmbracelet/huh) | Spinner for long-running operations |
| [charmbracelet/lipgloss](https://github.com/charmbracelet/lipgloss) | Terminal styling (colors, borders) |
| [cli/go-gh](https://github.com/cli/go-gh) | GitHub API client (REST and GraphQL) |
| [cli/browser](https://github.com/cli/browser) | Cross-platform browser opening |
