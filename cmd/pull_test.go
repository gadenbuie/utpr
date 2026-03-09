package cmd

import "testing"

func TestDeterminePullStrategy(t *testing.T) {
	tests := []struct {
		name   string
		ahead  int
		behind int
		want   PullStrategy
	}{
		{
			name:   "both zero is up to date",
			ahead:  0,
			behind: 0,
			want:   PullUpToDate,
		},
		{
			name:   "behind only is fast forward",
			ahead:  0,
			behind: 3,
			want:   PullFastForward,
		},
		{
			name:   "ahead and behind is diverged",
			ahead:  2,
			behind: 3,
			want:   PullDiverged,
		},
		{
			name:   "ahead only is up to date",
			ahead:  2,
			behind: 0,
			want:   PullUpToDate,
		},
		{
			name:   "minimal behind is fast forward",
			ahead:  0,
			behind: 1,
			want:   PullFastForward,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determinePullStrategy(tt.ahead, tt.behind)
			if got != tt.want {
				t.Errorf("determinePullStrategy(%d, %d) = %d, want %d",
					tt.ahead, tt.behind, got, tt.want)
			}
		})
	}
}
