package remote

import "testing"

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
