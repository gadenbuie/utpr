package cmd

import "testing"

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
