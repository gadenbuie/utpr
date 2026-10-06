package ui

import (
	"io"
	"os"
	"strings"
	"testing"
)

func withTTY(t *testing.T, stdout, stdin bool) {
	t.Helper()
	restore := SetTTYFuncs(
		func() bool { return stdout },
		func() bool { return stdin },
	)
	t.Cleanup(restore)
}

func TestAgentModeMatrix(t *testing.T) {
	tests := []struct {
		name      string
		stdoutTTY bool
		agent     bool
		pretty    bool
		want      bool
	}{
		{"tty no flags", true, false, false, false},
		{"pipe no flags", false, false, false, true},
		{"pipe agent", false, true, false, true},
		{"tty agent", true, true, false, true},
		{"pipe pretty", false, false, true, false},
		{"tty pretty", true, false, true, false},
		{"pipe agent pretty", false, true, true, false},
		{"tty agent pretty", true, true, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withTTY(t, tt.stdoutTTY, true)
			if got := AgentMode(tt.agent, tt.pretty); got != tt.want {
				t.Errorf("AgentMode(%v, %v) = %v, want %v", tt.agent, tt.pretty, got, tt.want)
			}
		})
	}
}

func TestRequireInteractiveTTY(t *testing.T) {
	withTTY(t, true, false)
	err := RequireInteractiveTTY("some guidance")
	if err == nil {
		t.Fatal("RequireInteractiveTTY() = nil with non-TTY stdin, want error")
	}
	if !strings.Contains(err.Error(), "some guidance") {
		t.Errorf("RequireInteractiveTTY() error %q missing guidance", err)
	}

	withTTY(t, true, true)
	if err := RequireInteractiveTTY("some guidance"); err != nil {
		t.Errorf("RequireInteractiveTTY() = %v with TTY stdin, want nil", err)
	}
}

func TestPromptsRequireTTY(t *testing.T) {
	withTTY(t, true, false)

	if _, err := Confirm("Proceed?", true); err == nil {
		t.Error("Confirm() = nil error with non-TTY stdin, want error")
	}
	if _, err := Input("Name:", "", ""); err == nil {
		t.Error("Input() = nil error with non-TTY stdin, want error")
	}
	if _, err := Choose("Pick:", []string{"a", "b"}); err == nil {
		t.Error("Choose() = nil error with non-TTY stdin, want error")
	}
	if _, err := ChooseWithOptions[int]("Pick:", nil); err == nil {
		t.Error("ChooseWithOptions() = nil error with non-TTY stdin, want error")
	}
	if _, err := ChooseMultiWithOptions[int]("Pick:", nil); err == nil {
		t.Error("ChooseMultiWithOptions() = nil error with non-TTY stdin, want error")
	}
}

func TestSpinSkipsInPlainMode(t *testing.T) {
	restore := SetSpinFunc(func(title string, fn func() error) error {
		t.Error("Spin() invoked the spinner in plain mode")
		return fn()
	})
	t.Cleanup(restore)

	oldPlain := plainMode
	t.Cleanup(func() { SetPlainMode(oldPlain) })
	SetPlainMode(true)

	if err := Spin("Working...", func() error { return nil }); err != nil {
		t.Fatalf("Spin() error in plain mode: %v", err)
	}
}

func TestInfoRoutesToStdoutInPlainMode(t *testing.T) {
	oldPlain := plainMode
	t.Cleanup(func() { SetPlainMode(oldPlain) })
	SetPlainMode(true)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() failed: %v", err)
	}
	oldStdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = oldStdout })

	Info("plain info")
	Success("plain success")

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe failed: %v", err)
	}
	var buf strings.Builder
	_, _ = io.Copy(&buf, r)

	want := "plain info\nplain success\n"
	if buf.String() != want {
		t.Errorf("plain mode output = %q, want %q", buf.String(), want)
	}
}
