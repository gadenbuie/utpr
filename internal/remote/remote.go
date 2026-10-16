package remote

import (
	"fmt"
	"os"
	"strings"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/ui"
)

// PrimaryRepoConfigKey is the git config key where utpr stores the user's
// answer to "which repository is the primary home for PRs and issues?".
const PrimaryRepoConfigKey = "utpr.primary-repo"

// PrimaryRepoEnvVar overrides the primary repo for a single invocation.
const PrimaryRepoEnvVar = "UTPR_PRIMARY_REPO"

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
	PrimaryRepo string       // owner/repo that owns PRs/issues; "" when inferred (empty for tests using zero values)
}

// resolveLayout determines the remote config from gathered remote info.
func resolveLayout(info RemoteInfo, defaultBranch string) *Config {
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	// Explicit primary-repo choice: when the primary repo is the origin
	// repo itself — e.g. a fork that has become the primary home of the
	// project — behave like a direct-push repo, ignoring any upstream remote.
	if info.PrimaryRepo != "" && info.PrimaryRepo == info.OriginRepo {
		return &Config{
			Layout:        "ours",
			SourceRemote:  info.OriginName,
			PushRemote:    info.OriginName,
			DefaultBranch: defaultBranch,
		}
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
			return nil, fmt.Errorf("no git remotes configured. A git remote is required for issue lookups and for fetching, pulling, or pushing branches")
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

	// Resolve the primary repo — the repository that owns PRs and issues.
	// For forks this is ambiguous (parent vs. the fork itself), so consult
	// overrides, gh's stored default, and the user before falling back.
	primary := ""
	if ghRepo != nil && ghRepo.Fork {
		parentRepo := ghRepo.Parent.FullName
		primary = primaryRepoOverride(ownerRepo, parentRepo)
		if primary == "" && parentRepo != "" {
			primary = decideForkPrimary(ownerRepo, parentRepo, hasUpstream,
				ghRepo.DefaultBranch, ghRepo.Parent.DefaultBranch)
		}
		// The primary is upstream: make sure the local remote exists so
		// fetch/pull/merge work. An explicit choice counts as consent.
		if primary == parentRepo && !hasUpstream {
			if addUpstreamRemote(parentRepo) == nil {
				hasUpstream = true
				info.HasUpstream = true
			}
		}
		if primary == "" && parentRepo == "" && !hasUpstream {
			ui.Warn("Fork detected but could not determine parent repo. Using origin as source.")
		}
	}
	info.PrimaryRepo = primary

	preliminary := resolveLayout(info, "")
	defaultBranch, _ := git.GetDefaultBranch(preliminary.SourceRemote)

	// Prefer the GitHub API's default branch for the repo being treated as
	// primary — local origin/HEAD can go stale when the default changes.
	if ghRepo != nil {
		if api := apiDefaultBranch(ghRepo, primary, ownerRepo); api != "" {
			defaultBranch = api
		}
	}

	cfg := resolveLayout(info, defaultBranch)

	if ghRepo != nil && !ghRepo.Fork && !ghRepo.Permissions.Push {
		ui.Warn("Could not determine remote config. Falling back to 'ours'.")
	}

	return cfg, nil
}

// apiDefaultBranch returns the GitHub API default branch for the repo that
// utpr treats as primary: the parent for contributor forks, the repo itself
// otherwise. Returns "" when the API did not report one.
func apiDefaultBranch(repo *gh.RepoInfo, primary, originRepo string) string {
	if primary != "" && primary != originRepo {
		return repo.Parent.DefaultBranch
	}
	return repo.DefaultBranch
}

// primaryRepoOverride reads the primary-repo override in precedence order:
// the UTPR_PRIMARY_REPO environment variable, the utpr.primary-repo git
// config (written by the one-time confirmation prompt), and gh's
// remote.*.gh-resolved values (written by `gh repo set-default`). Values
// matching neither candidate are warned about and ignored. Returns "" when
// nothing usable is set.
func primaryRepoOverride(forkRepo, parentRepo string) string {
	for _, value := range []string{
		strings.TrimSpace(os.Getenv(PrimaryRepoEnvVar)),
		git.ConfigGet(PrimaryRepoConfigKey),
		ghResolvedRepo(),
	} {
		if repo := validatePrimaryRepo(value, forkRepo, parentRepo); repo != "" {
			return repo
		}
	}
	return ""
}

// validatePrimaryRepo accepts a candidate value for the primary repo when it
// matches the fork or its parent. Unrecognized values produce a warning.
func validatePrimaryRepo(value, forkRepo, parentRepo string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if value == forkRepo || (parentRepo != "" && value == parentRepo) {
		return value
	}
	ui.Warnf("Ignoring primary repo '%s': expected '%s' or '%s'.", value, forkRepo, parentRepo)
	return ""
}

// ghResolvedRepo returns the default repository chosen by gh CLI, read from
// the remote.<name>.gh-resolved config entries that `gh repo set-default`
// writes. A value of "base" means the remote itself is the default repo;
// any other value is an OWNER/REPO for a repo without its own remote.
// Returns "" when gh has no stored default.
func ghResolvedRepo() string {
	out, err := git.Run("config", "--get-regexp", `^remote\..*\.gh-resolved$`)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		if value == "base" {
			remoteName := strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".gh-resolved")
			url, err := git.Run("remote", "get-url", remoteName)
			if err != nil {
				continue
			}
			if repo, err := ParseRepoSpec(url); err == nil {
				return repo
			}
			continue
		}
		if strings.Contains(value, "/") {
			return value
		}
	}
	return ""
}

// decideForkPrimary resolves the primary repo for a fork whose parent is
// known but has no stored answer. When the situation is genuinely ambiguous
// — the user has no upstream remote yet, or the fork's default branch has
// diverged from the parent's — ask once and save the answer. Otherwise
// keep the historical default.
func decideForkPrimary(forkRepo, parentRepo string, hasUpstream bool, forkDefault, parentDefault string) string {
	branchesDiffer := forkDefault != "" && parentDefault != "" && forkDefault != parentDefault

	if !hasUpstream || branchesDiffer {
		if !ui.StdinIsTTY() {
			ui.Warnf(
				"'%s' is a GitHub fork of '%s' but which repo is primary is ambiguous. Using '%s' for now; choose one with: git config %s <owner>/<repo> (or run 'gh repo set-default').",
					forkRepo, parentRepo, defaultForkPrimary(forkRepo, parentRepo, hasUpstream), PrimaryRepoConfigKey)
		} else if choice := promptPrimaryRepo(forkRepo, parentRepo, suggestForkPrimary(hasUpstream, forkDefault, parentDefault)); choice != "" {
				return choice
		}
	}

	return defaultForkPrimary(forkRepo, parentRepo, hasUpstream)
}

// suggestForkPrimary reports whether the fork should be the preselected
// option in the primary-repo prompt. The fork goes first only when it is
// the only locally workable primary (no upstream remote exists) or when
// its default branch has moved ahead of the parent's (fork main vs parent
// master) — a strong signal the fork is now the primary home. With an
// upstream remote, any other divergence (typically a stale fork still on
// master while the parent renamed to main) keeps the parent first, so
// pressing Enter preserves the historical contributor workflow.
func suggestForkPrimary(hasUpstream bool, forkDefault, parentDefault string) bool {
	return !hasUpstream || (forkDefault == "main" && parentDefault == "master")
}

// defaultForkPrimary picks the primary repo without asking: the parent when
// a local upstream remote already exists, otherwise the fork itself.
func defaultForkPrimary(forkRepo, parentRepo string, hasUpstream bool) string {
	if hasUpstream {
		return parentRepo
	}
	return forkRepo
}

// promptPrimaryRepo asks which repository is the primary home for PRs and
// issues and saves the answer to git config. Returns the chosen owner/repo,
// or "" when the user cancels. suggestFork puts the fork first.
func promptPrimaryRepo(forkRepo, parentRepo string, suggestFork bool) string {
	options := []string{
		fmt.Sprintf("%s (this fork)", forkRepo),
		fmt.Sprintf("%s (upstream)", parentRepo),
	}
	if !suggestFork {
		options[0], options[1] = options[1], options[0]
	}

	chosen, err := ui.Choose("Which repository is the primary home for your PRs and issues?", options)
	if err != nil {
		ui.Info("No answer saved; using the default for now.")
		return ""
	}
	repo, _, _ := strings.Cut(chosen, " ")
	if repo != forkRepo && repo != parentRepo {
		return ""
	}

	if err := git.ConfigSet(PrimaryRepoConfigKey, repo); err != nil {
		ui.Warnf("Could not save the primary repo setting: %v", err)
	} else {
		ui.Infof("Saved '%s=%s'. Change it any time with: git config %s <owner>/<repo>",
			PrimaryRepoConfigKey, repo, PrimaryRepoConfigKey)
	}
	return repo
}

// addUpstreamRemote adds a local 'upstream' remote for the parent repo.
// Returns an error when the remote could not be added.
func addUpstreamRemote(parentRepo string) error {
	_, err := git.Run("remote", "add", "upstream", fmt.Sprintf("https://github.com/%s.git", parentRepo))
	if err == nil {
		ui.Infof("Added 'upstream' remote for '%s'.", parentRepo)
	} else {
		ui.Warnf("Could not add 'upstream' remote for '%s': %v", parentRepo, err)
	}
	return err
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
			_, _ = git.Run("remote", "remove", remote)
		}
	}
}
