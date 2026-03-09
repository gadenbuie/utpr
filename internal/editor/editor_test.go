package editor

import "testing"

func TestAutoDetectFromEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "UTPR_EDITOR set",
			env:  map[string]string{"UTPR_EDITOR": "vim"},
			want: "vim",
		},
		{
			name: "UTPR_EDITOR overrides other vars",
			env: map[string]string{
				"UTPR_EDITOR": "custom-editor",
				"POSITRON":    "1",
				"ZED_SESSION": "abc",
			},
			want: "custom-editor",
		},
		{
			name: "POSITRON=1",
			env:  map[string]string{"POSITRON": "1"},
			want: "positron",
		},
		{
			name: "POSITRON=0 not detected",
			env:  map[string]string{"POSITRON": "0"},
			want: "",
		},
		{
			name: "CURSOR_TRACE_ID set",
			env:  map[string]string{"CURSOR_TRACE_ID": "abc"},
			want: "cursor",
		},
		{
			name: "CURSOR_SESSION_ID set",
			env:  map[string]string{"CURSOR_SESSION_ID": "abc"},
			want: "cursor",
		},
		{
			name: "both cursor vars set",
			env: map[string]string{
				"CURSOR_TRACE_ID":   "abc",
				"CURSOR_SESSION_ID": "def",
			},
			want: "cursor",
		},
		{
			name: "WINDSURF set",
			env:  map[string]string{"WINDSURF": "1"},
			want: "windsurf",
		},
		{
			name: "CODEIUM_WIND_SURF set",
			env:  map[string]string{"CODEIUM_WIND_SURF": "1"},
			want: "windsurf",
		},
		{
			name: "ZED_SESSION set",
			env:  map[string]string{"ZED_SESSION": "abc"},
			want: "zed",
		},
		{
			name: "TERM_PROGRAM=zed",
			env:  map[string]string{"TERM_PROGRAM": "zed"},
			want: "zed",
		},
		{
			name: "TERM_PROGRAM=vscode",
			env:  map[string]string{"TERM_PROGRAM": "vscode"},
			want: "code",
		},
		{
			name: "TERM_PROGRAM=vscode with Code - Insiders",
			env: map[string]string{
				"TERM_PROGRAM":          "vscode",
				"VSCODE_GIT_IPC_HANDLE": "/tmp/Code - Insiders/ipc",
			},
			want: "code-insiders",
		},
		{
			name: "VSCODE_PID set without TERM_PROGRAM",
			env:  map[string]string{"VSCODE_PID": "12345"},
			want: "code",
		},
		{
			name: "VSCODE_GIT_IPC_HANDLE with Code - Insiders without TERM_PROGRAM",
			env: map[string]string{
				"VSCODE_GIT_IPC_HANDLE": "/tmp/Code - Insiders/ipc",
			},
			want: "code-insiders",
		},
		{
			name: "no env vars",
			env:  map[string]string{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string {
				return tt.env[key]
			}
			got := autoDetectFromEnv(getenv)
			if got != tt.want {
				t.Errorf("autoDetectFromEnv() = %q, want %q", got, tt.want)
			}
		})
	}
}
