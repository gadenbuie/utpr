package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

// updateCheckInterval is how often utpr checks for a new release: at most
// once a day, on the first command that runs after the interval has passed.
const updateCheckInterval = 24 * time.Hour

// updateCheckTimeout bounds the background release check so it never hangs
// the command the user actually asked for.
const updateCheckTimeout = 5 * time.Second

// updateCheckState records when utpr last attempted an update check. It is
// stored in the user's config directory, outside any repository.
type updateCheckState struct {
	LastCheck time.Time `json:"last_check"`
}

// Seams for testing.
var (
	updateCheckFetch     = fetchLatestReleaseTag
	updateCheckStatePath = updateCheckConfigPath
)

func updateCheckConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "utpr", "update-check.json"), nil
}

func readUpdateCheckState(path string) (updateCheckState, error) {
	var state updateCheckState
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	return state, nil
}

func writeUpdateCheckState(path string, state updateCheckState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// isUpdateCheckDue reports whether an update check should run, given the last
// check time (zero if never checked).
func isUpdateCheckDue(last, now time.Time) bool {
	return now.Sub(last) >= updateCheckInterval
}

// shouldAutoCheckForUpdates reports whether the automatic update check should
// run for the invoked command. The explicit `utpr update` command does its
// own check; informational and plumbing commands never need one.
func shouldAutoCheckForUpdates(cmd *cobra.Command) bool {
	if version == "dev" {
		return false
	}
	if ui.PlainMode() {
		// Piped output and --agent mode: never interleave a check.
		return false
	}
	if os.Getenv("UTPR_NO_UPDATE_CHECK") != "" {
		return false
	}
	if cmd == rootCmd {
		return false
	}
	if cmd.Name() == "update" || cmd.Name() == "help" || cmd.Name() == "completion" {
		return false
	}
	if cmd.Parent() != nil && cmd.Parent().Name() == "completion" {
		return false
	}
	return true
}

// updateCheckResult carries the outcome of one update-check fetch. Results
// are passed over a buffered channel so that a cancelled spinner never races
// with a fetch goroutine that is still running.
type updateCheckResult struct {
	tag string
	err error
}

// maybeCheckForUpdates runs the once-a-day update check before a command
// runs. It never fails the command: network errors and cancellations are
// silently ignored, and the check result is advisory only.
func maybeCheckForUpdates(cmd *cobra.Command) {
	if !shouldAutoCheckForUpdates(cmd) {
		return
	}

	statePath, err := updateCheckStatePath()
	if err != nil {
		return
	}
	state, _ := readUpdateCheckState(statePath)
	if !isUpdateCheckDue(state.LastCheck, time.Now()) {
		return
	}

	resultCh := make(chan updateCheckResult, 1)
	// Ctrl+C during the spinner cancels the check: the spinner returns an
	// error and the command continues as if the check never ran. The fetch
	// goroutine sends its result into the buffered channel and exits without
	// anyone reading it.
	spinErr := ui.Spin("Checking for updates...", func() error {
		var res updateCheckResult
		res.tag, res.err = updateCheckFetch()
		resultCh <- res
		return res.err
	})
	// Record the attempt whether the check succeeded, failed, or was
	// cancelled, so we try at most once a day.
	_ = writeUpdateCheckState(statePath, updateCheckState{LastCheck: time.Now()})

	// The result is only read when the spinner completed normally: on
	// cancellation the fetch goroutine may still be running.
	if spinErr != nil {
		return
	}
	result := <-resultCh

	if result.err != nil || result.tag == "" || isCurrentOrNewer(version, result.tag) {
		return
	}

	ui.Infof("Update available: %s → %s.", version, displayVersion(result.tag))
	if isBrewInstall() {
		ui.Info("  Run 'brew upgrade gadenbuie/tap/utpr' to install it.")
	} else {
		ui.Info("  Run 'utpr update' to install it.")
	}
}

// displayVersion normalizes a release tag for display: release builds record
// the version without the leading "v" (e.g. 0.5.3, not v0.5.3).
func displayVersion(tag string) string {
	return strings.TrimPrefix(tag, "v")
}

// isBrewInstall reports whether the running executable is managed by
// Homebrew, in which case updating with `utpr update` would fight with brew.
func isBrewInstall() bool {
	execPath, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
		execPath = resolved
	}
	return strings.Contains(execPath, filepath.Join("Cellar", "utpr"))
}

// fetchLatestReleaseTag returns the tag of the latest utpr release. It
// prefers the authenticated GitHub API (which works for private forks of the
// repo) and falls back to an anonymous API request, so the check also works
// before `gh auth login`. The whole fetch is bounded by updateCheckTimeout:
// go-gh clients have no default request timeout, so without the context a
// stalled connection could hang the command.
func fetchLatestReleaseTag() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
	defer cancel()

	if gh.IsAuthenticated() {
		if client, err := gh.RESTClient(); err == nil {
			var release struct {
				TagName string `json:"tag_name"`
			}
			if err := client.DoWithContext(ctx, http.MethodGet, fmt.Sprintf("repos/%s/releases/latest", utprRepo), nil, &release); err == nil && release.TagName != "" {
				return release.TagName, nil
			}
		}
	}
	return fetchLatestReleaseTagAnonymous(ctx)
}

func fetchLatestReleaseTagAnonymous(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", utprRepo), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "utpr/"+version)

	resp, err := http.DefaultClient.Do(req) //nolint:gosec // constant URL
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status checking for updates: HTTP %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	if release.TagName == "" {
		return "", fmt.Errorf("no release found for %s", utprRepo)
	}
	return release.TagName, nil
}
