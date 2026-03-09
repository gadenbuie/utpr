package remote

import (
	"fmt"
	"strings"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/ui"
)

// Config holds the detected remote configuration.
type Config struct {
	// Layout is "ours" (direct push) or "fork" (push to fork, source is upstream)
	Layout        string
	SourceRemote  string // remote to pull from (e.g., "origin" or "upstream")
	PushRemote    string // remote to push to (e.g., "origin")
	DefaultBranch string
}

var cached *Config

// RemoteInfo holds raw data gathered by detect() before layout resolution.
type RemoteInfo struct {
	OriginName  string       // name of the primary remote ("origin" or first available)
	OriginRepo  string       // owner/repo parsed from the remote URL
	HasUpstream bool         // whether an "upstream" remote exists
	GHRepo      *gh.RepoInfo // nil if GitHub API unavailable
}

// resolveLayout determines the remote config from gathered remote info.
func resolveLayout(info RemoteInfo, defaultBranch string) *Config {
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	if info.GHRepo != nil {
		repo := info.GHRepo

		if !repo.Fork && repo.Permissions.Push {
			return &Config{
				Layout:        "ours",
				SourceRemote:  info.OriginName,
				PushRemote:    info.OriginName,
				DefaultBranch: defaultBranch,
			}
		}

		if repo.Fork {
			cfg := &Config{
				Layout:        "fork",
				PushRemote:    info.OriginName,
				DefaultBranch: defaultBranch,
			}
			if info.HasUpstream {
				cfg.SourceRemote = "upstream"
			} else {
				cfg.SourceRemote = info.OriginName
			}
			return cfg
		}

		// Fallback: not a fork, no push permission
		return &Config{
			Layout:        "ours",
			SourceRemote:  info.OriginName,
			PushRemote:    info.OriginName,
			DefaultBranch: defaultBranch,
		}
	}

	// No GitHub API info — infer from local remotes
	if info.HasUpstream {
		return &Config{
			Layout:        "fork",
			SourceRemote:  "upstream",
			PushRemote:    info.OriginName,
			DefaultBranch: defaultBranch,
		}
	}
	return &Config{
		Layout:        "ours",
		SourceRemote:  info.OriginName,
		PushRemote:    info.OriginName,
		DefaultBranch: defaultBranch,
	}
}

// shouldCleanupRemote returns true if no tracking refs reference the given remote.
func shouldCleanupRemote(remote string, trackingRefs []string) bool {
	prefix := remote + "/"
	for _, ref := range trackingRefs {
		if strings.HasPrefix(ref, prefix) {
			return false
		}
	}
	return true
}

// resetCache clears the cached remote config. Used by tests.
func resetCache() {
	cached = nil
}

// ResetCacheForTest clears the cached remote config. Exported for integration tests.
func ResetCacheForTest() {
	resetCache()
}

// SetCacheForTest sets the cached remote config directly. Exported for integration tests.
func SetCacheForTest(cfg *Config) {
	cached = cfg
}

// Detect determines the remote configuration by inspecting git remotes
// and the GitHub API. The result is cached after the first call.
func Detect() (*Config, error) {
	if cached != nil {
		return cached, nil
	}

	cfg, err := detect()
	if err != nil {
		return nil, err
	}
	cached = cfg
	return cached, nil
}

// Require returns the cached config or panics if not initialized.
func Require() *Config {
	if cached == nil {
		panic("internal error: remote config not initialized")
	}
	return cached
}

func detect() (*Config, error) {
	originName := "origin"
	originURL, err := git.Run("remote", "get-url", "origin")
	if err != nil {
		// No origin — use first available remote
		remotes, err := git.Run("remote")
		if err != nil || remotes == "" {
			return nil, fmt.Errorf("no git remotes configured. Add a remote before using utpr")
		}
		originName = strings.Split(remotes, "\n")[0]
		ui.Warnf("No 'origin' remote found. Using '%s'.", originName)
		defaultBranch, _ := git.GetDefaultBranch(originName)
		return resolveLayout(RemoteInfo{OriginName: originName}, defaultBranch), nil
	}

	ownerRepo, err := ParseRepoSpec(originURL)
	if err != nil {
		return nil, err
	}

	// Check for existing upstream remote
	_, upstreamErr := git.Run("remote", "get-url", "upstream")
	hasUpstream := upstreamErr == nil

	// Try GitHub API to detect fork status
	ghRepo, apiErr := gh.GetRepo(ownerRepo)
	if apiErr != nil {
		ui.Warn("Could not reach GitHub API. Inferring remote config from local remotes.")
		ghRepo = nil
	}

	info := RemoteInfo{
		OriginName:  originName,
		OriginRepo:  ownerRepo,
		HasUpstream: hasUpstream,
		GHRepo:      ghRepo,
	}

	// Determine default branch based on what will be the source remote
	preliminary := resolveLayout(info, "")
	defaultBranch, _ := git.GetDefaultBranch(preliminary.SourceRemote)

	cfg := resolveLayout(info, defaultBranch)

	// Side effect: offer to add upstream remote for forks without one
	if ghRepo != nil && ghRepo.Fork && !hasUpstream {
		if ghRepo.Parent.FullName != "" {
			confirmed, _ := ui.Confirm(
				fmt.Sprintf("Add 'upstream' remote for '%s'?", ghRepo.Parent.FullName),
				true,
			)
			if confirmed {
				_, addErr := git.Run("remote", "add", "upstream",
					fmt.Sprintf("https://github.com/%s.git", ghRepo.Parent.FullName))
				if addErr == nil {
					cfg.SourceRemote = "upstream"
					newDefault, _ := git.GetDefaultBranch("upstream")
					if newDefault != "" {
						cfg.DefaultBranch = newDefault
					}
				} else {
					ui.Warn("Using origin as source remote (no upstream configured).")
				}
			} else {
				ui.Warn("Using origin as source remote (no upstream configured).")
			}
		} else {
			ui.Warn("Fork detected but could not determine parent repo. Using origin as source.")
		}
	}

	if ghRepo != nil && !ghRepo.Fork && !ghRepo.Permissions.Push {
		ui.Warn("Could not determine remote config. Falling back to 'ours'.")
	}

	return cfg, nil
}

// ParseRepoSpec extracts "owner/repo" from a git remote URL.
func ParseRepoSpec(url string) (string, error) {
	url = strings.TrimSuffix(url, ".git")

	var path string
	if strings.HasPrefix(url, "git@") && strings.Contains(url, ":") {
		// SSH shorthand: git@github.com:owner/repo
		path = url[strings.Index(url, ":")+1:]
	} else if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "ssh://") {
		// URL scheme
		path = url[strings.Index(url, "://")+3:]
		// Remove host
		if idx := strings.Index(path, "/"); idx >= 0 {
			path = path[idx+1:]
		}
	} else {
		return "", fmt.Errorf("unrecognized remote URL format: %s", url)
	}

	// Extract last two path segments as owner/repo
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("could not parse owner/repo from remote URL: %s", url)
	}
	owner := parts[len(parts)-2]
	repo := parts[len(parts)-1]

	if owner == "" || repo == "" {
		return "", fmt.Errorf("could not parse owner/repo from remote URL: %s", url)
	}
	return owner + "/" + repo, nil
}

// CleanupUtprRemotes removes utpr-created remotes that no longer have
// any branches tracking them.
func CleanupUtprRemotes() {
	remotes, err := git.Run("remote")
	if err != nil {
		return
	}
	for _, remote := range strings.Split(remotes, "\n") {
		remote = strings.TrimSpace(remote)
		if remote == "" {
			continue
		}
		if !git.IsRemoteCreatedByPRTool(remote) {
			continue
		}
		refs, _ := git.ForEachRef("%(upstream:short)", "-committerdate", "refs/heads/")
		trackingRefs := strings.Split(refs, "\n")
		if shouldCleanupRemote(remote, trackingRefs) {
			ui.Infof("Removing unused remote '%s'.", remote)
			git.Run("remote", "remove", remote)
		}
	}
}
