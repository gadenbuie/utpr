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
	originURL, err := git.Run("remote", "get-url", "origin")
	if err != nil {
		// No origin — use first available remote
		remotes, err := git.Run("remote")
		if err != nil || remotes == "" {
			return nil, fmt.Errorf("no git remotes configured. Add a remote before using utpr")
		}
		fallback := strings.Split(remotes, "\n")[0]
		ui.Warnf("No 'origin' remote found. Using '%s'.", fallback)
		defaultBranch, _ := git.GetDefaultBranch(fallback)
		if defaultBranch == "" {
			defaultBranch = "main"
		}
		return &Config{
			Layout:        "ours",
			SourceRemote:  fallback,
			PushRemote:    fallback,
			DefaultBranch: defaultBranch,
		}, nil
	}

	ownerRepo, err := ParseRepoSpec(originURL)
	if err != nil {
		return nil, err
	}

	// Try GitHub API to detect fork status
	repo, apiErr := gh.GetRepo(ownerRepo)
	if apiErr != nil {
		// API unavailable — infer from local remotes
		ui.Warn("Could not reach GitHub API. Inferring remote config from local remotes.")
		return inferFromLocalRemotes()
	}

	if !repo.Fork && repo.Permissions.Push {
		defaultBranch, _ := git.GetDefaultBranch("origin")
		if defaultBranch == "" {
			defaultBranch = "main"
		}
		return &Config{
			Layout:        "ours",
			SourceRemote:  "origin",
			PushRemote:    "origin",
			DefaultBranch: defaultBranch,
		}, nil
	}

	if repo.Fork {
		cfg := &Config{
			Layout:     "fork",
			PushRemote: "origin",
		}

		// Check for existing upstream remote
		_, err := git.Run("remote", "get-url", "upstream")
		if err == nil {
			cfg.SourceRemote = "upstream"
		} else if repo.Parent.FullName != "" {
			confirmed, _ := ui.Confirm(
				fmt.Sprintf("Add 'upstream' remote for '%s'?", repo.Parent.FullName),
				true,
			)
			if confirmed {
				_, addErr := git.Run("remote", "add", "upstream",
					fmt.Sprintf("https://github.com/%s.git", repo.Parent.FullName))
				if addErr == nil {
					cfg.SourceRemote = "upstream"
				} else {
					ui.Warn("Using origin as source remote (no upstream configured).")
					cfg.SourceRemote = "origin"
				}
			} else {
				ui.Warn("Using origin as source remote (no upstream configured).")
				cfg.SourceRemote = "origin"
			}
		} else {
			ui.Warn("Fork detected but could not determine parent repo. Using origin as source.")
			cfg.SourceRemote = "origin"
		}

		cfg.DefaultBranch, _ = git.GetDefaultBranch(cfg.SourceRemote)
		if cfg.DefaultBranch == "" {
			cfg.DefaultBranch = "main"
		}
		return cfg, nil
	}

	// Fallback
	ui.Warn("Could not determine remote config. Falling back to 'ours'.")
	defaultBranch, _ := git.GetDefaultBranch("origin")
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	return &Config{
		Layout:        "ours",
		SourceRemote:  "origin",
		PushRemote:    "origin",
		DefaultBranch: defaultBranch,
	}, nil
}

func inferFromLocalRemotes() (*Config, error) {
	_, err := git.Run("remote", "get-url", "upstream")
	if err == nil {
		defaultBranch, _ := git.GetDefaultBranch("upstream")
		if defaultBranch == "" {
			defaultBranch = "main"
		}
		return &Config{
			Layout:        "fork",
			SourceRemote:  "upstream",
			PushRemote:    "origin",
			DefaultBranch: defaultBranch,
		}, nil
	}

	defaultBranch, _ := git.GetDefaultBranch("origin")
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	return &Config{
		Layout:        "ours",
		SourceRemote:  "origin",
		PushRemote:    "origin",
		DefaultBranch: defaultBranch,
	}, nil
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
		if !git.IsRemoteCreatedByUtpr(remote) {
			continue
		}
		// Check if any branch tracks this remote
		refs, _ := git.ForEachRef("%(upstream:short)", "-committerdate", "refs/heads/")
		hasTracking := false
		for _, ref := range strings.Split(refs, "\n") {
			if strings.HasPrefix(ref, remote+"/") {
				hasTracking = true
				break
			}
		}
		if !hasTracking {
			ui.Infof("Removing unused remote '%s'.", remote)
			git.Run("remote", "remove", remote)
		}
	}
}
