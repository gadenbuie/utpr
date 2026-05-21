package cmd

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
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
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	var latestTag, assetURL string
	err := ui.Spin("Checking for updates...", func() error {
		var spinErr error
		latestTag, assetURL, spinErr = getLatestRelease(goos, goarch)
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

	err = ui.Spin(fmt.Sprintf("Downloading %s...", latestTag), func() error {
		return downloadAndReplace(assetURL, goos, goarch, execPath)
	})
	if err != nil {
		return ui.Dief("Update failed: %s", err)
	}

	ui.Successf("Updated to %s.", latestTag)
	ui.Info("Run 'utpr completion --help' to refresh your shell completions.")
	return nil
}

// isCurrentOrNewer reports whether the running version is the latest release
// or a development build made after it (git-describe: "v0.2.2-2-gabcdef").
func isCurrentOrNewer(current, latestTag string) bool {
	return current == latestTag || strings.HasPrefix(current, latestTag+"-")
}

// getLatestRelease returns the latest release tag and the GitHub API URL for
// the release asset matching the given platform. The API URL (not browser_download_url)
// is required for authenticated downloads from private repositories.
func getLatestRelease(goos, goarch string) (tag, assetURL string, err error) {
	client, err := gh.RESTClient()
	if err != nil {
		return "", "", fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"assets"`
	}
	if err := client.Get(fmt.Sprintf("repos/%s/releases/latest", utprRepo), &release); err != nil {
		return "", "", fmt.Errorf("failed to get latest release: %w", err)
	}
	if release.TagName == "" {
		return "", "", fmt.Errorf("no release found for %s", utprRepo)
	}
	assetName := releaseAssetName(goos, goarch)
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			return release.TagName, asset.URL, nil
		}
	}
	return release.TagName, "", fmt.Errorf("no release asset found for %s/%s in %s", goos, goarch, release.TagName)
}

func releaseAssetName(goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "darwin" {
		ext = ".dmg"
	}
	return fmt.Sprintf("utpr-%s-%s%s", goos, goarch, ext)
}

// downloadAndReplace fetches the release archive for the current platform and
// atomically replaces the running executable at execPath.
func downloadAndReplace(url, goos, goarch, execPath string) error {
	httpClient, err := goghapi.DefaultHTTPClient()
	if err != nil {
		return fmt.Errorf("failed to create HTTP client: %w", err)
	}
	req, err := http.NewRequest(http.MethodGet, url, nil) //nolint:gosec // URL from GitHub API response
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d for %s", resp.StatusCode, url)
	}

	if goos == "darwin" {
		return downloadAndReplaceFromDMG(resp.Body, execPath)
	}

	return downloadAndReplaceFromArchive(resp.Body, goos, goarch, execPath)
}

func downloadAndReplaceFromArchive(body io.Reader, goos, goarch, execPath string) error {
	gzr, err := gzip.NewReader(body)
	if err != nil {
		return fmt.Errorf("failed to decompress archive: %w", err)
	}
	defer gzr.Close() //nolint:errcheck

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

		return installBinary(execPath, tr)
	}

	return fmt.Errorf("binary %q not found in release archive", binaryName)
}

func downloadAndReplaceFromDMG(body io.Reader, execPath string) error {
	tmpDir, err := os.MkdirTemp("", "utpr-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck

	dmgPath := filepath.Join(tmpDir, "utpr.dmg")
	dmgFile, err := os.Create(dmgPath)
	if err != nil {
		return fmt.Errorf("failed to create temporary disk image: %w", err)
	}

	if _, err := io.Copy(dmgFile, body); err != nil {
		_ = dmgFile.Close()
		return fmt.Errorf("failed to write disk image: %w", err)
	}
	if err := dmgFile.Close(); err != nil {
		return fmt.Errorf("failed to finalize disk image: %w", err)
	}

	mountPath := filepath.Join(tmpDir, "mnt")
	if err := os.Mkdir(mountPath, 0755); err != nil {
		return fmt.Errorf("failed to create mount point: %w", err)
	}

	if output, err := exec.Command("hdiutil", "attach", "-quiet", "-nobrowse", "-readonly", "-mountpoint", mountPath, dmgPath).CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			return fmt.Errorf("failed to mount disk image: %w", err)
		}
		return fmt.Errorf("failed to mount disk image: %s: %w", msg, err)
	}

	binPath := filepath.Join(mountPath, "utpr")
	binFile, err := os.Open(binPath)
	if err != nil {
		_ = exec.Command("hdiutil", "detach", "-quiet", mountPath).Run()
		return fmt.Errorf("failed to open utpr from disk image: %w", err)
	}

	installErr := installBinary(execPath, binFile)
	closeErr := binFile.Close()
	detachOutput, detachErr := exec.Command("hdiutil", "detach", "-quiet", mountPath).CombinedOutput()

	if installErr != nil {
		return installErr
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close mounted binary: %w", closeErr)
	}
	if detachErr != nil {
		msg := strings.TrimSpace(string(detachOutput))
		if msg == "" {
			return fmt.Errorf("failed to detach disk image: %w", detachErr)
		}
		return fmt.Errorf("failed to detach disk image: %s: %w", msg, detachErr)
	}

	return nil
}

func installBinary(execPath string, src io.Reader) error {
	dir := filepath.Dir(execPath)
	tmp, err := os.CreateTemp(dir, ".utpr-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file in %s (check permissions): %w", dir, err)
	}
	tmpPath := tmp.Name()

	_, copyErr := io.Copy(tmp, src)
	closeErr := tmp.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write update: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to finalize update: %w", closeErr)
	}

	if err := os.Chmod(tmpPath, 0755); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	if err := os.Rename(tmpPath, execPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to install update to %s: %w", execPath, err)
	}
	return nil
}
