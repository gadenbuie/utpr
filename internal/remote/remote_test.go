package remote

import (
	"testing"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/testutil"
	"github.com/gadenbuie/utpr/internal/ui"
)

func TestParseRepoSpec(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    string
		wantErr bool
	}{
		{
			name: "SSH shorthand",
			url:  "git@github.com:gadenbuie/utpr.git",
			want: "gadenbuie/utpr",
		},
		{
			name: "SSH shorthand without .git",
			url:  "git@github.com:gadenbuie/utpr",
			want: "gadenbuie/utpr",
		},
		{
			name: "HTTPS",
			url:  "https://github.com/gadenbuie/utpr.git",
			want: "gadenbuie/utpr",
		},
		{
			name: "HTTPS without .git",
			url:  "https://github.com/gadenbuie/utpr",
			want: "gadenbuie/utpr",
		},
		{
			name: "HTTP",
			url:  "http://github.com/gadenbuie/utpr.git",
			want: "gadenbuie/utpr",
		},
		{
			name: "SSH scheme",
			url:  "ssh://git@github.com/gadenbuie/utpr.git",
			want: "gadenbuie/utpr",
		},
		{
			name: "GHE with path prefix",
			url:  "https://ghe.corp.com/prefix/owner/repo.git",
			want: "owner/repo",
		},
		{
			name:    "Unrecognized format",
			url:     "ftp://example.com/repo",
			wantErr: true,
		},
		{
			name:    "Empty URL",
			url:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRepoSpec(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseRepoSpec(%q) expected error, got %q", tt.url, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseRepoSpec(%q) unexpected error: %v", tt.url, err)
				return
			}
			if got != tt.want {
				t.Errorf("ParseRepoSpec(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestResolveLayout(t *testing.T) {
	tests := []struct {
		name          string
		info          RemoteInfo
		defaultBranch string
		wantLayout    string
		wantSource    string
		wantPush      string
		wantDefault   string
	}{
		{
			name: "not a fork with push permission",
			info: RemoteInfo{
				OriginName: "origin",
				GHRepo: &gh.RepoInfo{
					Fork: false,
					Permissions: struct {
						Push bool `json:"push"`
					}{Push: true},
				},
			},
			defaultBranch: "main",
			wantLayout:    "ours",
			wantSource:    "origin",
			wantPush:      "origin",
			wantDefault:   "main",
		},
		{
			name: "fork with upstream exists",
			info: RemoteInfo{
				OriginName:  "origin",
				HasUpstream: true,
				GHRepo: &gh.RepoInfo{
					Fork: true,
					Permissions: struct {
						Push bool `json:"push"`
					}{Push: true},
				},
			},
			defaultBranch: "develop",
			wantLayout:    "fork",
			wantSource:    "upstream",
			wantPush:      "origin",
			wantDefault:   "develop",
		},
		{
			name: "fork without upstream",
			info: RemoteInfo{
				OriginName:  "origin",
				HasUpstream: false,
				GHRepo: &gh.RepoInfo{
					Fork: true,
					Permissions: struct {
						Push bool `json:"push"`
					}{Push: true},
				},
			},
			defaultBranch: "main",
			wantLayout:    "fork",
			wantSource:    "origin",
			wantPush:      "origin",
			wantDefault:   "main",
		},
		{
			name: "API unavailable with upstream",
			info: RemoteInfo{
				OriginName:  "origin",
				HasUpstream: true,
				GHRepo:      nil,
			},
			defaultBranch: "main",
			wantLayout:    "fork",
			wantSource:    "upstream",
			wantPush:      "origin",
			wantDefault:   "main",
		},
		{
			name: "API unavailable without upstream",
			info: RemoteInfo{
				OriginName:  "origin",
				HasUpstream: false,
				GHRepo:      nil,
			},
			defaultBranch: "main",
			wantLayout:    "ours",
			wantSource:    "origin",
			wantPush:      "origin",
			wantDefault:   "main",
		},
		{
			name: "fork no push permission on origin",
			info: RemoteInfo{
				OriginName:  "origin",
				HasUpstream: false,
				GHRepo: &gh.RepoInfo{
					Fork: true,
					Permissions: struct {
						Push bool `json:"push"`
					}{Push: false},
				},
			},
			defaultBranch: "main",
			wantLayout:    "fork",
			wantSource:    "origin",
			wantPush:      "origin",
			wantDefault:   "main",
		},
		{
			name: "not a fork no push permission",
			info: RemoteInfo{
				OriginName: "origin",
				GHRepo: &gh.RepoInfo{
					Fork: false,
					Permissions: struct {
						Push bool `json:"push"`
					}{Push: false},
				},
			},
			defaultBranch: "main",
			wantLayout:    "ours",
			wantSource:    "origin",
			wantPush:      "origin",
			wantDefault:   "main",
		},
		{
			name: "empty default branch falls back to main",
			info: RemoteInfo{
				OriginName: "origin",
				GHRepo: &gh.RepoInfo{
					Fork: false,
					Permissions: struct {
						Push bool `json:"push"`
					}{Push: true},
				},
			},
			defaultBranch: "",
			wantLayout:    "ours",
			wantSource:    "origin",
			wantPush:      "origin",
			wantDefault:   "main",
		},
		{
			name: "custom origin name",
			info: RemoteInfo{
				OriginName: "myremote",
				GHRepo: &gh.RepoInfo{
					Fork: false,
					Permissions: struct {
						Push bool `json:"push"`
					}{Push: true},
				},
			},
			defaultBranch: "main",
			wantLayout:    "ours",
			wantSource:    "myremote",
			wantPush:      "myremote",
			wantDefault:   "main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveLayout(tt.info, tt.defaultBranch)
			if got.Layout != tt.wantLayout {
				t.Errorf("Layout = %q, want %q", got.Layout, tt.wantLayout)
			}
			if got.SourceRemote != tt.wantSource {
				t.Errorf("SourceRemote = %q, want %q", got.SourceRemote, tt.wantSource)
			}
			if got.PushRemote != tt.wantPush {
				t.Errorf("PushRemote = %q, want %q", got.PushRemote, tt.wantPush)
			}
			if got.DefaultBranch != tt.wantDefault {
				t.Errorf("DefaultBranch = %q, want %q", got.DefaultBranch, tt.wantDefault)
			}
		})
	}
}

func TestShouldCleanupRemote(t *testing.T) {
	tests := []struct {
		name         string
		remote       string
		trackingRefs []string
		want         bool
	}{
		{
			name:         "no tracking refs",
			remote:       "pr-123",
			trackingRefs: []string{},
			want:         true,
		},
		{
			name:         "tracking ref matches remote",
			remote:       "pr-123",
			trackingRefs: []string{"pr-123/main", "origin/feature"},
			want:         false,
		},
		{
			name:         "tracking ref for different remote",
			remote:       "pr-123",
			trackingRefs: []string{"origin/main", "upstream/develop"},
			want:         true,
		},
		{
			name:         "partial prefix match does not match",
			remote:       "pr",
			trackingRefs: []string{"prfork/main"},
			want:         true,
		},
		{
			name:         "empty tracking refs list",
			remote:       "pr-123",
			trackingRefs: []string{},
			want:         true,
		},
		{
			name:         "empty string in refs",
			remote:       "pr-123",
			trackingRefs: []string{""},
			want:         true,
		},
		{
			name:         "multiple refs one matches",
			remote:       "upstream",
			trackingRefs: []string{"origin/main", "upstream/develop", "origin/feature"},
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldCleanupRemote(tt.remote, tt.trackingRefs)
			if got != tt.want {
				t.Errorf("shouldCleanupRemote(%q, %v) = %v, want %v",
					tt.remote, tt.trackingRefs, got, tt.want)
			}
		})
	}
}

func TestResetCache(t *testing.T) {
	cached = &Config{Layout: "test"}
	resetCache()
	if cached != nil {
		t.Error("resetCache() did not clear cached config")
	}
}

func TestResolveLayoutForkIsPrimary(t *testing.T) {
	ghRepo := &gh.RepoInfo{Fork: true}
	ghRepo.Parent.FullName = "jcheng5/scorecard-db"

	// The fork itself is the primary repo: behave like a direct-push repo,
	// even though an upstream remote exists.
	cfg := resolveLayout(RemoteInfo{
		OriginName:  "origin",
		OriginRepo:  "gadenbuie/scorecard-db",
		HasUpstream: true,
		PrimaryRepo: "gadenbuie/scorecard-db",
		GHRepo:      ghRepo,
	}, "main")
	if cfg.Layout != "ours" {
		t.Errorf("Layout = %q, want %q", cfg.Layout, "ours")
	}
	if cfg.SourceRemote != "origin" {
		t.Errorf("SourceRemote = %q, want %q", cfg.SourceRemote, "origin")
	}
	if cfg.PushRemote != "origin" {
		t.Errorf("PushRemote = %q, want %q", cfg.PushRemote, "origin")
	}
	if cfg.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q, want %q", cfg.DefaultBranch, "main")
	}
}

func TestValidatePrimaryRepo(t *testing.T) {
	const forkRepo = "gadenbuie/scorecard-db"
	const parentRepo = "jcheng5/scorecard-db"

	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"empty", "", ""},
		{"whitespace", "   ", ""},
		{"matches fork", forkRepo, forkRepo},
		{"matches parent", parentRepo, parentRepo},
		{"whitespace trimmed", " " + forkRepo + " ", forkRepo},
		{"unknown repo", "someone/else", ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := validatePrimaryRepo(tt.value, forkRepo, parentRepo); got != tt.want {
				t.Errorf("validatePrimaryRepo(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestDecideForkPrimaryDefaults(t *testing.T) {
	const forkRepo = "gadenbuie/scorecard-db"
	const parentRepo = "jcheng5/scorecard-db"

	cases := []struct {
		name          string
		hasUpstream   bool
		forkDefault   string
		parentDefault string
		want          string
	}{
		// Contributor fork with upstream remote: no prompt, upstream is primary.
		{"upstream remote and same defaults", true, "main", "main", parentRepo},
		// Diverged defaults suggest the fork is primary, but without a TTY
		// the historical default (upstream) applies with a warning.
		{"upstream remote and diverged defaults", true, "main", "master", parentRepo},
		// No upstream remote: the fork is its own primary.
		{"no upstream remote", false, "main", "master", forkRepo},
		{"no upstream remote and same defaults", false, "main", "main", forkRepo},
	}
	// decideForkPrimary consults the prompt in a TTY; force the non-TTY
	// path so the test never hangs or prompts regardless of how the test
	// binary's stdin is wired.
	restoreTTY := ui.SetTTYFuncs(func() bool { return true }, func() bool { return false })
	defer restoreTTY()

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := decideForkPrimary(forkRepo, parentRepo, tt.hasUpstream, tt.forkDefault, tt.parentDefault)
			if got != tt.want {
				t.Errorf("decideForkPrimary(...) = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSuggestForkPrimary(t *testing.T) {
	cases := []struct {
		name          string
		hasUpstream   bool
		forkDefault   string
		parentDefault string
		want          bool
	}{
		// No upstream remote: the fork is the only locally workable primary.
		{"no upstream remote", false, "main", "main", true},
		// Fork moved ahead (fork main vs parent master): fork is primary.
		{"fork default moved ahead", true, "main", "master", true},
		// Stale contributor fork (fork master, parent renamed to main):
		// keep the parent preselected.
		{"stale fork, renamed parent", true, "master", "main", false},
		// Same defaults with upstream: parent first.
		{"same defaults with upstream", true, "main", "main", false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := suggestForkPrimary(tt.hasUpstream, tt.forkDefault, tt.parentDefault); got != tt.want {
				t.Errorf("suggestForkPrimary(%v, %q, %q) = %v, want %v",
					tt.hasUpstream, tt.forkDefault, tt.parentDefault, got, tt.want)
			}
		})
	}
}

// TestPromptPrimaryRepoOrder verifies the preselected (first) option in the
// primary-repo prompt: the parent goes first for stale contributor forks,
// the fork first when the fork is the only or clearly primary choice.
func TestPromptPrimaryRepoOrder(t *testing.T) {
	const forkRepo = "gadenbuie/scorecard-db"
	const parentRepo = "jcheng5/scorecard-db"
	testutil.TempRepo(t)

	var options []string
	restore := ui.SetChooseFunc(func(header string, opts []string) (string, error) {
		options = opts
		return "", ui.ErrCancelled
	})
	defer restore()

	promptPrimaryRepo(forkRepo, parentRepo, true)
	if len(options) != 2 || options[0] != forkRepo+" (this fork)" || options[1] != parentRepo+" (upstream)" {
		t.Errorf("suggestFork order = %v, want fork first", options)
	}

	options = nil
	promptPrimaryRepo(forkRepo, parentRepo, false)
	if len(options) != 2 || options[0] != parentRepo+" (upstream)" || options[1] != forkRepo+" (this fork)" {
		t.Errorf("suggestParent order = %v, want parent first", options)
	}
}

func TestGhResolvedRepo(t *testing.T) {
	dir := testutil.TempRepo(t)
	t.Setenv(PrimaryRepoEnvVar, "")

	if got := ghResolvedRepo(); got != "" {
		t.Errorf("ghResolvedRepo() with no gh config = %q, want empty", got)
	}

	testutil.RunGit(t, dir, "remote", "add", "origin", "https://github.com/gadenbuie/scorecard-db.git")
	testutil.RunGit(t, dir, "remote", "add", "upstream", "https://github.com/jcheng5/scorecard-db.git")

	// gh repo set-default writes "base" on the chosen remote.
	testutil.RunGit(t, dir, "config", "remote.origin.gh-resolved", "base")
	if got := ghResolvedRepo(); got != "gadenbuie/scorecard-db" {
		t.Errorf("ghResolvedRepo() = %q, want %q", got, "gadenbuie/scorecard-db")
	}

	// gh stores OWNER/REPO when the chosen repo has no remote of its own.
	testutil.RunGit(t, dir, "config", "remote.origin.gh-resolved", "jcheng5/scorecard-db")
	if got := ghResolvedRepo(); got != "jcheng5/scorecard-db" {
		t.Errorf("ghResolvedRepo() with owner/repo value = %q, want %q", got, "jcheng5/scorecard-db")
	}
}

func TestPrimaryRepoOverridePrecedence(t *testing.T) {
	const forkRepo = "gadenbuie/scorecard-db"
	const parentRepo = "jcheng5/scorecard-db"
	dir := testutil.TempRepo(t)

	// Don't let an inherited UTPR_PRIMARY_REPO shadow the sources under test.
	t.Setenv(PrimaryRepoEnvVar, "")

	testutil.RunGit(t, dir, "remote", "add", "origin", "https://github.com/gadenbuie/scorecard-db.git")
	testutil.RunGit(t, dir, "config", "remote.origin.gh-resolved", "base")

	// gh's stored default applies when nothing else is set.
	if got := primaryRepoOverride(forkRepo, parentRepo); got != forkRepo {
		t.Errorf("primaryRepoOverride() = %q, want %q (gh-resolved)", got, forkRepo)
	}

	// utpr's git config wins over gh's stored default.
	testutil.RunGit(t, dir, "config", PrimaryRepoConfigKey, parentRepo)
	if got := primaryRepoOverride(forkRepo, parentRepo); got != parentRepo {
		t.Errorf("primaryRepoOverride() = %q, want %q (git config)", got, parentRepo)
	}

	// The environment variable wins over everything.
	t.Setenv(PrimaryRepoEnvVar, forkRepo)
	if got := primaryRepoOverride(forkRepo, parentRepo); got != forkRepo {
		t.Errorf("primaryRepoOverride() = %q, want %q (env var)", got, forkRepo)
	}

	// An unknown value is skipped, falling through to the next source.
	t.Setenv(PrimaryRepoEnvVar, "someone/else")
	if got := primaryRepoOverride(forkRepo, parentRepo); got != parentRepo {
		t.Errorf("primaryRepoOverride() with unknown env value = %q, want %q", got, parentRepo)
	}
}

func TestPromptPrimaryRepoSavesAnswer(t *testing.T) {
	const forkRepo = "gadenbuie/scorecard-db"
	const parentRepo = "jcheng5/scorecard-db"
	testutil.TempRepo(t)

	restore := testutil.StubChoose(forkRepo + " (this fork)")
	defer restore()

	got := promptPrimaryRepo(forkRepo, parentRepo, true)
	if got != forkRepo {
		t.Fatalf("promptPrimaryRepo() = %q, want %q", got, forkRepo)
	}
	if stored := git.ConfigGet(PrimaryRepoConfigKey); stored != forkRepo {
		t.Errorf("stored %s = %q, want %q", PrimaryRepoConfigKey, stored, forkRepo)
	}
}

func TestPromptPrimaryRepoCancel(t *testing.T) {
	const forkRepo = "gadenbuie/scorecard-db"
	const parentRepo = "jcheng5/scorecard-db"
	testutil.TempRepo(t)

	restore := ui.SetChooseFunc(func(header string, options []string) (string, error) {
		return "", ui.ErrCancelled
	})
	defer restore()

	if got := promptPrimaryRepo(forkRepo, parentRepo, true); got != "" {
		t.Errorf("promptPrimaryRepo() on cancel = %q, want empty", got)
	}
	if stored := git.ConfigGet(PrimaryRepoConfigKey); stored != "" {
		t.Errorf("stored %s = %q, want empty", PrimaryRepoConfigKey, stored)
	}
}
