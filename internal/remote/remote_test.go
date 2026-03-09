package remote

import (
	"testing"

	"github.com/gadenbuie/utpr/internal/gh"
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
		name         string
		info         RemoteInfo
		defaultBranch string
		wantLayout   string
		wantSource   string
		wantPush     string
		wantDefault  string
	}{
		{
			name: "not a fork with push permission",
			info: RemoteInfo{
				OriginName: "origin",
				GHRepo: &gh.RepoInfo{
					Fork:        false,
					Permissions: struct{ Push bool `json:"push"` }{Push: true},
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
					Fork:        true,
					Permissions: struct{ Push bool `json:"push"` }{Push: true},
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
					Fork:        true,
					Permissions: struct{ Push bool `json:"push"` }{Push: true},
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
					Fork:        true,
					Permissions: struct{ Push bool `json:"push"` }{Push: false},
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
					Fork:        false,
					Permissions: struct{ Push bool `json:"push"` }{Push: false},
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
					Fork:        false,
					Permissions: struct{ Push bool `json:"push"` }{Push: true},
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
					Fork:        false,
					Permissions: struct{ Push bool `json:"push"` }{Push: true},
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
