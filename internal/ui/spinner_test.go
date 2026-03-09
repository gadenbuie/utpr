package ui

import (
	"errors"
	"testing"
)

func TestSpinStub(t *testing.T) {
	restore := SetSpinFunc(func(title string, action func() error) error {
		return action()
	})
	defer restore()

	called := false
	err := Spin("working...", func() error {
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

func TestSpinStubPropagatesError(t *testing.T) {
	restore := SetSpinFunc(func(title string, action func() error) error {
		return action()
	})
	defer restore()

	want := errors.New("boom")
	got := Spin("working...", func() error {
		return want
	})
	if got != want {
		t.Errorf("expected error %v, got %v", want, got)
	}
}
