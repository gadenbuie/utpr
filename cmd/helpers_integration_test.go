//go:build integration

package cmd

// Integration tests in this file mutate the process working directory. Do NOT use t.Parallel().

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/testutil"
)

func containsLine(s, line string) bool {
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) == line {
			return true
		}
	}
	return false
}

func TestPRHeadOwners(t *testing.T) {
	forkCfg := &remote.Config{
		Layout:        "fork",
		SourceRemote:  "upstream",
		PushRemote:    "origin",
		DefaultBranch: "main",
	}
	oursCfg := &remote.Config{
		Layout:        "ours",
		SourceRemote:  "origin",
		PushRemote:    "origin",
		DefaultBranch: "main",
	}

	tests := []struct {
		name        string
		cfg         *remote.Config
		sourceRepo  string
		trackingRef string
		remotes     map[string]string // remote name -> URL
		want        []string
	}{
		{
			name:       "fork layout, no tracking ref",
			cfg:        forkCfg,
			sourceRepo: "posit-dev/repo",
			remotes: map[string]string{
				"origin":   "https://github.com/gadenbuie/repo.git",
				"upstream": "https://github.com/posit-dev/repo.git",
			},
			want: []string{"gadenbuie", "posit-dev"},
		},
		{
			name:        "fork layout, tracking the push remote",
			cfg:         forkCfg,
			sourceRepo:  "posit-dev/repo",
			trackingRef: "origin/feature",
			remotes: map[string]string{
				"origin":   "https://github.com/gadenbuie/repo.git",
				"upstream": "https://github.com/posit-dev/repo.git",
			},
			want: []string{"gadenbuie", "posit-dev"},
		},
		{
			name:        "fork layout, tracking a contributor remote",
			cfg:         forkCfg,
			sourceRepo:  "posit-dev/repo",
			trackingRef: "contributor/feature",
			remotes: map[string]string{
				"origin":      "https://github.com/gadenbuie/repo.git",
				"upstream":    "https://github.com/posit-dev/repo.git",
				"contributor": "https://github.com/contributor/repo.git",
			},
			want: []string{"contributor", "gadenbuie", "posit-dev"},
		},
		{
			name:       "same-repo layout",
			cfg:        oursCfg,
			sourceRepo: "owner/repo",
			remotes: map[string]string{
				"origin": "https://github.com/owner/repo.git",
			},
			want: []string{"owner"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _ = testutil.TempRepoWithRemote(t) // chdirs into the clone
			for name, url := range tt.remotes {
				if testutil.RunGit(t, ".", "remote") == "" || !containsLine(testutil.RunGit(t, ".", "remote"), name) {
					testutil.RunGit(t, ".", "remote", "add", name, url)
				} else {
					testutil.RunGit(t, ".", "remote", "set-url", name, url)
				}
			}

			got := prHeadOwners(tt.cfg, tt.sourceRepo, tt.trackingRef)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("prHeadOwners() = %v, want %v", got, tt.want)
			}
		})
	}
}
