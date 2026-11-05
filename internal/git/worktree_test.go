package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseWorktreeListOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []Worktree
	}{
		{
			name:   "empty output",
			output: "",
			want:   nil,
		},
		{
			name: "single main worktree",
			output: "worktree /home/user/repo\n" +
				"HEAD abc123\n" +
				"branch refs/heads/main\n" +
				"\n",
			want: []Worktree{
				{Path: "/home/user/repo", HEAD: "abc123", Branch: "refs/heads/main"},
			},
		},
		{
			name: "main plus one linked worktree",
			output: "worktree /home/user/repo\n" +
				"HEAD abc123\n" +
				"branch refs/heads/main\n" +
				"\n" +
				"worktree /home/user/repo.worktrees/feat\n" +
				"HEAD def456\n" +
				"branch refs/heads/feat\n" +
				"\n",
			want: []Worktree{
				{Path: "/home/user/repo", HEAD: "abc123", Branch: "refs/heads/main"},
				{Path: "/home/user/repo.worktrees/feat", HEAD: "def456", Branch: "refs/heads/feat"},
			},
		},
		{
			name: "multiple worktrees",
			output: "worktree /repo\n" +
				"HEAD aaa\n" +
				"branch refs/heads/main\n" +
				"\n" +
				"worktree /repo.worktrees/a\n" +
				"HEAD bbb\n" +
				"branch refs/heads/a\n" +
				"\n" +
				"worktree /repo.worktrees/b\n" +
				"HEAD ccc\n" +
				"branch refs/heads/b\n" +
				"\n",
			want: []Worktree{
				{Path: "/repo", HEAD: "aaa", Branch: "refs/heads/main"},
				{Path: "/repo.worktrees/a", HEAD: "bbb", Branch: "refs/heads/a"},
				{Path: "/repo.worktrees/b", HEAD: "ccc", Branch: "refs/heads/b"},
			},
		},
		{
			name: "detached HEAD (no branch line)",
			output: "worktree /repo\n" +
				"HEAD abc123\n" +
				"branch refs/heads/main\n" +
				"\n" +
				"worktree /repo.worktrees/detached\n" +
				"HEAD def456\n" +
				"detached\n" +
				"\n",
			want: []Worktree{
				{Path: "/repo", HEAD: "abc123", Branch: "refs/heads/main"},
				{Path: "/repo.worktrees/detached", HEAD: "def456", Branch: ""},
			},
		},
		{
			name: "no trailing newline",
			output: "worktree /repo\n" +
				"HEAD abc123\n" +
				"branch refs/heads/main",
			want: []Worktree{
				{Path: "/repo", HEAD: "abc123", Branch: "refs/heads/main"},
			},
		},
		{
			name: "trailing newline",
			output: "worktree /repo\n" +
				"HEAD abc123\n" +
				"branch refs/heads/main\n",
			want: []Worktree{
				{Path: "/repo", HEAD: "abc123", Branch: "refs/heads/main"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseWorktreeListOutput(tt.output)
			if len(got) != len(tt.want) {
				t.Fatalf("ParseWorktreeListOutput() returned %d worktrees, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("worktree[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestComputeWorktreeDir(t *testing.T) {
	tests := []struct {
		name     string
		topLevel string
		branch   string
		want     string
	}{
		{
			name:     "simple branch",
			topLevel: "/home/user/myrepo",
			branch:   "feat/foo",
			want:     filepath.Join("/home/user", "myrepo.worktrees", "feat/foo"),
		},
		{
			name:     "nested branch with slashes",
			topLevel: "/home/user/myrepo",
			branch:   "fix/issue/123",
			want:     filepath.Join("/home/user", "myrepo.worktrees", "fix/issue/123"),
		},
		{
			name:     "repo at filesystem root",
			topLevel: "/myrepo",
			branch:   "dev",
			want:     filepath.Join("/", "myrepo.worktrees", "dev"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeWorktreeDir(tt.topLevel, tt.branch)
			if got != tt.want {
				t.Errorf("ComputeWorktreeDir(%q, %q) = %q, want %q", tt.topLevel, tt.branch, got, tt.want)
			}
		})
	}
}

func TestFindWorktreeForBranch(t *testing.T) {
	worktrees := []Worktree{
		{Path: "/repo", HEAD: "aaa", Branch: "refs/heads/main"},
		{Path: "/repo.worktrees/feat-a", HEAD: "bbb", Branch: "refs/heads/feat-a"},
		{Path: "/repo.worktrees/feat-b", HEAD: "ccc", Branch: "refs/heads/feat-b"},
		{Path: "/repo.worktrees/feat-c", HEAD: "ddd", Branch: "refs/heads/feat-c"},
	}

	tests := []struct {
		name      string
		worktrees []Worktree
		branch    string
		want      string
	}{
		{
			name:      "branch in second worktree",
			worktrees: worktrees,
			branch:    "feat-a",
			want:      "/repo.worktrees/feat-a",
		},
		{
			name:      "branch not found",
			worktrees: worktrees,
			branch:    "nonexistent",
			want:      "",
		},
		{
			name:      "main worktree branch is skipped",
			worktrees: worktrees,
			branch:    "main",
			want:      "",
		},
		{
			name:      "match in last worktree",
			worktrees: worktrees,
			branch:    "feat-c",
			want:      "/repo.worktrees/feat-c",
		},
		{
			name:      "nil worktrees",
			worktrees: nil,
			branch:    "main",
			want:      "",
		},
		{
			name:      "single main worktree only",
			worktrees: []Worktree{{Path: "/repo", Branch: "refs/heads/main"}},
			branch:    "main",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindWorktreeForBranch(tt.worktrees, tt.branch)
			if got != tt.want {
				t.Errorf("FindWorktreeForBranch(worktrees, %q) = %q, want %q", tt.branch, got, tt.want)
			}
		})
	}
}

func TestBranchInMainWorktree(t *testing.T) {
	worktrees := []Worktree{
		{Path: "/repo", HEAD: "aaa", Branch: "refs/heads/main"},
		{Path: "/repo.worktrees/feat-a", HEAD: "bbb", Branch: "refs/heads/feat-a"},
	}

	tests := []struct {
		name      string
		worktrees []Worktree
		branch    string
		want      bool
	}{
		{
			name:      "branch checked out in main worktree",
			worktrees: worktrees,
			branch:    "main",
			want:      true,
		},
		{
			name:      "branch checked out in other worktree",
			worktrees: worktrees,
			branch:    "feat-a",
			want:      false,
		},
		{
			name:      "branch not checked out anywhere",
			worktrees: worktrees,
			branch:    "nonexistent",
			want:      false,
		},
		{
			name:      "nil worktrees",
			worktrees: nil,
			branch:    "main",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BranchInMainWorktree(tt.worktrees, tt.branch)
			if got != tt.want {
				t.Errorf("BranchInMainWorktree(worktrees, %q) = %v, want %v", tt.branch, got, tt.want)
			}
		})
	}
}

func TestUntrackedPaths(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{
			name:   "empty output",
			output: "",
			want:   nil,
		},
		{
			name:   "single untracked file",
			output: "?? foo.txt\x00",
			want:   []string{"foo.txt"},
		},
		{
			name:   "untracked directories are skipped",
			output: "?? dir/\x00?? file.txt\x00",
			want:   []string{"file.txt"},
		},
		{
			name:   "non-untracked statuses are skipped",
			output: " M tracked.txt\x00?? new.txt\x00A  staged.txt\x00 M file with space.txt\x00",
			want:   []string{"new.txt"},
		},
		{
			name:   "paths with spaces are kept intact",
			output: "?? my file.txt\x00",
			want:   []string{"my file.txt"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := untrackedPaths(tt.output)
			if len(got) != len(tt.want) {
				t.Fatalf("untrackedPaths() = %q, want %q", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("untrackedPaths() = %q, want %q", got, tt.want)
				}
			}
		})
	}
}

func TestSymlinkPointsOutside(t *testing.T) {
	dir := t.TempDir()
	outsideDir := t.TempDir()

	insideFile := filepath.Join(dir, "inside.txt")
	if err := os.WriteFile(insideFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outsideDir, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	relOutside, err := filepath.Rel(dir, outsideFile)
	if err != nil {
		t.Fatal(err)
	}

	links := map[string]string{
		"rel-inside":     "inside.txt",
		"abs-inside":     insideFile,
		"dir-inside":     ".",
		"abs-outside":    outsideFile,
		"rel-outside":    relOutside,
		"broken-inside":  "missing.txt",
		"broken-outside": filepath.Join(outsideDir, "missing.txt"),
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name string
		link string
		want bool
	}{
		{"relative link inside", "rel-inside", false},
		{"absolute link inside", "abs-inside", false},
		{"link to the root itself", "dir-inside", false},
		{"absolute link outside", "abs-outside", true},
		{"relative link outside", "rel-outside", true},
		// A broken symlink is judged lexically: one aimed inside the worktree
		// must be kept (it may be deliberate, like current -> build/out),
		// while one aimed outside still counts as outside.
		{"broken link inside", "broken-inside", false},
		{"broken link outside", "broken-outside", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := symlinkPointsOutside(filepath.Join(dir, tt.link), dir)
			if err != nil {
				t.Fatalf("symlinkPointsOutside(%q) failed: %v", tt.link, err)
			}
			if got != tt.want {
				t.Fatalf("symlinkPointsOutside(%q) = %v, want %v", tt.link, got, tt.want)
			}
		})
	}
}
