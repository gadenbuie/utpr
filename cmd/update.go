package cmd

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	goghapi "github.com/cli/go-gh/v2/pkg/api"
	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

const utprRepo = "gadenbuie/utpr"

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update utpr to the latest version",
	Long:  "Check for the latest release of utpr and update in place.",
	// Override root's PersistentPreRunE: update doesn't need a git repo.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if !gh.IsAuthenticated() {
			return ui.Die("GitHub authentication not found. Run 'gh auth login' or set GITHUB_TOKEN.")
		}
		return nil
	},
	RunE: runUpdate,
}

var updateForce bool
var updateYes bool

func init() {
	updateCmd.Flags().BoolVar(&updateForce, "force", false, "Re-download even if already on the latest version")
	updateCmd.Flags().BoolVarP(&updateYes, "yes", "y", false, "Skip confirmation prompt")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	var latestTag string
	err := ui.Spin("Checking for updates...", func() error {
		var spinErr error
		latestTag, spinErr = getLatestReleaseTag()
		return spinErr
	})
	if err != nil {
		return ui.Dief("Failed to check for updates: %s", err)
	}

	if !updateForce && version != "dev" && isCurrentOrNewer(version, latestTag) {
		ui.Successf("Already up to date (%s).", version)
		return nil
	}

	execPath, err := os.Executable()
	if err != nil {
		return ui.Die("Could not determine executable path.")
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return ui.Die("Could not resolve executable path.")
	}

	goos := runtime.GOOS
	goarch := runtime.GOARCH

	if version == "dev" {
		ui.Infof("Development build — will install latest release %s.", latestTag)
	} else if updateForce {
		ui.Infof("Force-reinstalling %s.", latestTag)
	} else {
		ui.Infof("Updating %s → %s.", version, latestTag)
	}
	ui.Infof("Platform:  %s/%s", goos, goarch)
	ui.Infof("Location:  %s", execPath)

	if !updateYes {
		if err := ui.MustConfirm("Proceed with update?", true); err != nil {
			return err
		}
	}

	assetName := fmt.Sprintf("utpr-%s-%s.tar.gz", goos, goarch)
	downloadURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", utprRepo, latestTag, assetName)

	err = ui.Spin(fmt.Sprintf("Downloading %s...", latestTag), func() error {
		return downloadAndReplace(downloadURL, goos, goarch, execPath)
	})
	if err != nil {
		return ui.Dief("Update failed: %s", err)
	}

	ui.Successf("Updated to %s.", latestTag)
	return nil
}

// isCurrentOrNewer reports whether the running version is the latest release
// or a development build made after it (git-describe: "v0.2.2-2-gabcdef").
func isCurrentOrNewer(current, latestTag string) bool {
	return current == latestTag || strings.HasPrefix(current, latestTag+"-")
}

func getLatestReleaseTag() (string, error) {
	client, err := gh.RESTClient()
	if err != nil {
		return "", fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := client.Get(fmt.Sprintf("repos/%s/releases/latest", utprRepo), &release); err != nil {
		return "", fmt.Errorf("failed to get latest release: %w", err)
	}
	if release.TagName == "" {
		return "", fmt.Errorf("no release found for %s", utprRepo)
	}
	return release.TagName, nil
}

// downloadAndReplace fetches the release archive for the current platform and
// atomically replaces the running executable at execPath.
func downloadAndReplace(url, goos, goarch, execPath string) error {
	httpClient, err := goghapi.DefaultHTTPClient()
	if err != nil {
		return fmt.Errorf("failed to create HTTP client: %w", err)
	}
	resp, err := httpClient.Get(url) //nolint:gosec // URL constructed from known constants + release tag
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d for %s", resp.StatusCode, url)
	}

	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to decompress archive: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	binaryName := fmt.Sprintf("utpr-%s-%s/utpr", goos, goarch)
	if goos == "windows" {
		binaryName += ".exe"
	}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read archive: %w", err)
		}
		if hdr.Name != binaryName {
			continue
		}

		dir := filepath.Dir(execPath)
		tmp, err := os.CreateTemp(dir, ".utpr-update-*")
		if err != nil {
			return fmt.Errorf("failed to create temp file in %s (check permissions): %w", dir, err)
		}
		tmpPath := tmp.Name()

		_, copyErr := io.Copy(tmp, tr)
		tmp.Close()
		if copyErr != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to write update: %w", copyErr)
		}

		if err := os.Chmod(tmpPath, 0755); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to set permissions: %w", err)
		}

		if err := os.Rename(tmpPath, execPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to install update to %s: %w", execPath, err)
		}
		return nil
	}

	return fmt.Errorf("binary %q not found in release archive %s", binaryName, url)
}
