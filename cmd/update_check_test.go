package cmd

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

// stubUpdateCheck replaces the update check seams for a test and restores
// them on cleanup: version is a release build, the state file lives in a
// temp directory, and fetch is replaced with a recorder returning the given
// result.
func stubUpdateCheck(t *testing.T, latest string, fetchErr error) *int {
	t.Helper()
	calls := 0

	oldVersion := version
	oldFetch := updateCheckFetch
	oldPath := updateCheckStatePath
	t.Cleanup(func() {
		version = oldVersion
		updateCheckFetch = oldFetch
		updateCheckStatePath = oldPath
	})

	version = "0.5.3"
	updateCheckFetch = func() (string, error) {
		calls++
		return latest, fetchErr
	}
	statePath := filepath.Join(t.TempDir(), "update-check.json")
	updateCheckStatePath = func() (string, error) {
		return statePath, nil
	}
	return &calls
}

// captureStderr captures everything written to os.Stderr while fn runs.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()

	done := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- string(out)
	}()

	fn()
	_ = w.Close()
	return <-done
}

func TestIsUpdateCheckDue(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		last time.Time
		want bool
	}{
		{"never checked", time.Time{}, true},
		{"checked just now", now, false},
		{"checked 23 hours ago", now.Add(-23 * time.Hour), false},
		{"checked exactly 24 hours ago", now.Add(-24 * time.Hour), true},
		{"checked days ago", now.Add(-72 * time.Hour), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUpdateCheckDue(tt.last, now); got != tt.want {
				t.Fatalf("isUpdateCheckDue(%v) = %v, want %v", tt.last, got, tt.want)
			}
		})
	}
}

func TestMaybeCheckForUpdates(t *testing.T) {
	// Run in styled mode: plain mode skips the check entirely. Under go test
	// there is no TTY, so ui output is unstyled apart from the "ℹ" prefix.
	ui.SetPlainMode(false)
	defer ui.SetPlainMode(false)

	t.Run("reports a newer release, then waits a day", func(t *testing.T) {
		calls := stubUpdateCheck(t, "v9.9.9", nil)

		var first, second string
		first = captureStderr(t, func() {
			maybeCheckForUpdates(&cobra.Command{Use: "status"})
		})
		second = captureStderr(t, func() {
			maybeCheckForUpdates(&cobra.Command{Use: "status"})
		})

		if *calls != 1 {
			t.Fatalf("expected exactly 1 fetch, got %d", *calls)
		}
		for _, want := range []string{
			"Update available: 0.5.3 → 9.9.9.",
			"Run 'utpr update' to install it.",
		} {
			if !strings.Contains(first, want) {
				t.Fatalf("notice %q does not contain %q", first, want)
			}
		}
		if second != "" {
			t.Fatalf("expected no notice on second invocation, got %q", second)
		}
	})

	t.Run("stays silent when already up to date", func(t *testing.T) {
		// v0.0.1 is older than the stubbed version 0.5.3.
		calls := stubUpdateCheck(t, "v0.0.1", nil)

		out := captureStderr(t, func() {
			maybeCheckForUpdates(&cobra.Command{Use: "status"})
		})

		if *calls != 1 {
			t.Fatalf("expected exactly 1 fetch, got %d", *calls)
		}
		if out != "" {
			t.Fatalf("expected no notice, got %q", out)
		}
	})

	t.Run("stays silent and records the attempt when the check fails", func(t *testing.T) {
		calls := stubUpdateCheck(t, "", errors.New("network down"))

		// Both invocations: only the first one may fetch, neither may print.
		var out string
		for range 2 {
			out = captureStderr(t, func() {
				maybeCheckForUpdates(&cobra.Command{Use: "status"})
			})
			if out != "" {
				t.Fatalf("expected no notice, got %q", out)
			}
		}

		if *calls != 1 {
			t.Fatalf("expected exactly 1 fetch, got %d", *calls)
		}
	})

	t.Run("is skipped when UTPR_NO_UPDATE_CHECK is set", func(t *testing.T) {
		t.Setenv("UTPR_NO_UPDATE_CHECK", "1")
		calls := stubUpdateCheck(t, "v9.9.9", nil)

		out := captureStderr(t, func() {
			maybeCheckForUpdates(&cobra.Command{Use: "status"})
		})

		if *calls != 0 {
			t.Fatalf("expected no fetch, got %d", *calls)
		}
		if out != "" {
			t.Fatalf("expected no notice, got %q", out)
		}
	})

	t.Run("is skipped for informational and plumbing commands", func(t *testing.T) {
		calls := stubUpdateCheck(t, "v9.9.9", nil)

		for _, use := range []string{"update", "completion"} {
			out := captureStderr(t, func() {
				maybeCheckForUpdates(&cobra.Command{Use: use})
			})
			if out != "" {
				t.Fatalf("%s: expected no notice, got %q", use, out)
			}
		}

		completionParent := &cobra.Command{Use: "completion"}
		completionParent.AddCommand(&cobra.Command{Use: "bash"})
		completionSub := completionParent.Commands()[0]
		out := captureStderr(t, func() {
			maybeCheckForUpdates(completionSub)
		})
		if out != "" {
			t.Fatalf("completion subcommand: expected no notice, got %q", out)
		}

		if *calls != 0 {
			t.Fatalf("expected no fetch, got %d", *calls)
		}
	})
}

func TestMaybeCheckForUpdatesCancelled(t *testing.T) {
	// Styled mode with a spinner stub that simulates Ctrl+C interrupting the
	// check: the spinner returns an error without running the fetch.
	calls := stubUpdateCheck(t, "v9.9.9", nil)
	ui.SetPlainMode(false)
	t.Cleanup(func() { ui.SetPlainMode(false) })
	restore := ui.SetSpinFunc(func(_ string, _ func() error) error {
		return errors.New("program was interrupted")
	})
	defer restore()

	out := captureStderr(t, func() {
		maybeCheckForUpdates(&cobra.Command{Use: "status"})
	})

	if *calls != 0 {
		t.Fatalf("cancelled check should not have fetched, got %d calls", *calls)
	}
	if out != "" {
		t.Fatalf("expected no notice after cancellation, got %q", out)
	}

	// The cancelled attempt still counts as today's check.
	out = captureStderr(t, func() {
		maybeCheckForUpdates(&cobra.Command{Use: "status"})
	})
	if *calls != 0 {
		t.Fatalf("expected no fetch on second invocation, got %d", *calls)
	}
	if out != "" {
		t.Fatalf("expected no notice on second invocation, got %q", out)
	}
}

func TestDisplayVersion(t *testing.T) {
	tests := []struct{ tag, want string }{
		{"v1.2.3", "1.2.3"},
		{"1.2.3", "1.2.3"},
	}
	for _, tt := range tests {
		if got := displayVersion(tt.tag); got != tt.want {
			t.Errorf("displayVersion(%q) = %q, want %q", tt.tag, got, tt.want)
		}
	}
}
