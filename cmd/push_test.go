package cmd

import "testing"

func TestPushHasAgentFlag(t *testing.T) {
	if pushCmd.Flags().Lookup("agent") == nil {
		t.Fatal("push command is missing the --agent flag")
	}
}

func TestDeterminePRTarget(t *testing.T) {
	tests := []struct {
		name       string
		layout     string
		pushRepo   string
		sourceRepo string
		want       string
	}{
		{
			name:       "owner layout returns pushRepo",
			layout:     "owner",
			pushRepo:   "alice/myrepo",
			sourceRepo: "",
			want:       "alice/myrepo",
		},
		{
			name:       "fork layout returns sourceRepo",
			layout:     "fork",
			pushRepo:   "alice/myrepo",
			sourceRepo: "upstream/myrepo",
			want:       "upstream/myrepo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determinePRTarget(tt.layout, tt.pushRepo, tt.sourceRepo)
			if got != tt.want {
				t.Errorf("determinePRTarget(%q, %q, %q) = %q, want %q",
					tt.layout, tt.pushRepo, tt.sourceRepo, got, tt.want)
			}
		})
	}
}

func TestBuildPRHeadRef(t *testing.T) {
	tests := []struct {
		name          string
		layout        string
		pushOwnerRepo string
		branch        string
		want          string
	}{
		{
			name:          "owner layout returns plain branch",
			layout:        "owner",
			pushOwnerRepo: "alice/myrepo",
			branch:        "feature-x",
			want:          "feature-x",
		},
		{
			name:          "fork layout qualifies with fork owner",
			layout:        "fork",
			pushOwnerRepo: "alice/myrepo",
			branch:        "feature-x",
			want:          "alice:feature-x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPRHeadRef(tt.layout, tt.pushOwnerRepo, tt.branch)
			if got != tt.want {
				t.Errorf("buildPRHeadRef(%q, %q, %q) = %q, want %q",
					tt.layout, tt.pushOwnerRepo, tt.branch, got, tt.want)
			}
		})
	}
}

func TestBuildCompareURL(t *testing.T) {
	tests := []struct {
		name            string
		layout          string
		pushOwnerRepo   string
		sourceOwnerRepo string
		defaultBranch   string
		branch          string
		want            string
	}{
		{
			name:            "owner layout",
			layout:          "owner",
			pushOwnerRepo:   "alice/myrepo",
			sourceOwnerRepo: "alice/myrepo",
			defaultBranch:   "main",
			branch:          "feature-x",
			want:            "https://github.com/alice/myrepo/compare/main...feature-x?expand=1",
		},
		{
			name:            "fork layout",
			layout:          "fork",
			pushOwnerRepo:   "alice/myrepo",
			sourceOwnerRepo: "upstream/myrepo",
			defaultBranch:   "main",
			branch:          "feature-x",
			want:            "https://github.com/upstream/myrepo/compare/main...alice:feature-x?expand=1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCompareURL(tt.layout, tt.pushOwnerRepo, tt.sourceOwnerRepo, tt.defaultBranch, tt.branch)
			if got != tt.want {
				t.Errorf("buildCompareURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
