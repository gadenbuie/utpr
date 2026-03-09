package testutil

import (
	"testing"

	"github.com/gadenbuie/utpr/internal/ui"
)

func TestStubConfirm(t *testing.T) {
	restore := StubConfirm(true)
	defer restore()

	got, err := ui.Confirm("test?", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected StubConfirm(true) to make Confirm return true")
	}
}

func TestStubConfirmSequence(t *testing.T) {
	restore := StubConfirmSequence(true, false, true)
	defer restore()

	for i, want := range []bool{true, false, true} {
		got, err := ui.Confirm("test?", false)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if got != want {
			t.Errorf("call %d: expected %v, got %v", i, want, got)
		}
	}
}

func TestStubChoose(t *testing.T) {
	restore := StubChoose("picked")
	defer restore()

	got, err := ui.Choose("pick", []string{"a", "b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "picked" {
		t.Errorf("expected picked, got %q", got)
	}
}

func TestStubInput(t *testing.T) {
	restore := StubInput("hello")
	defer restore()

	got, err := ui.Input("name?", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Errorf("expected hello, got %q", got)
	}
}

func TestStubSpin(t *testing.T) {
	restore := StubSpin()
	defer restore()

	called := false
	err := ui.Spin("working...", func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected action to be called")
	}
}
