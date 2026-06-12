package cmd

import (
	"testing"
)

func TestIsCurrentOrNewer(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"v0.5.3", "v0.5.3", true},
		{"v0.5.3", "v0.5.2", true},  // current is newer patch
		{"v0.5.3", "v0.4.9", true},  // current is newer minor
		{"v1.0.0", "v0.9.9", true},  // current is newer major
		{"v0.5.3-2-gabcdef", "v0.5.3", true}, // dev build after latest
		{"v0.5.2", "v0.5.3", false}, // current is older
		{"dev", "v0.5.3", false},
	}
	for _, tt := range tests {
		got := isCurrentOrNewer(tt.current, tt.latest)
		if got != tt.want {
			t.Errorf("isCurrentOrNewer(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestIsMajorOrMinorBump(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"v0.2.2", "v0.2.3", false},
		{"v0.2.2", "v0.3.0", true},
		{"v0.2.2", "v1.0.0", true},
		{"v1.0.0", "v1.0.1", false},
		{"v1.0.0", "v1.1.0", true},
		{"dev", "v0.3.0", true},
		{"v0.2.2-2-gabcdef", "v0.2.3", false},
		{"v0.2.2-2-gabcdef", "v0.3.0", true},
	}
	for _, tt := range tests {
		got := isMajorOrMinorBump(tt.current, tt.latest)
		if got != tt.want {
			t.Errorf("isMajorOrMinorBump(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestReleaseAssetName(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		goarch string
		want   string
	}{
		{
			name:   "linux uses tarball",
			goos:   "linux",
			goarch: "amd64",
			want:   "utpr-linux-amd64.tar.gz",
		},
		{
			name:   "windows uses tarball",
			goos:   "windows",
			goarch: "arm64",
			want:   "utpr-windows-arm64.tar.gz",
		},
		{
			name:   "darwin uses dmg",
			goos:   "darwin",
			goarch: "arm64",
			want:   "utpr-darwin-arm64.dmg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := releaseAssetName(tt.goos, tt.goarch)
			if got != tt.want {
				t.Errorf("releaseAssetName(%q, %q) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
			}
		})
	}
}
