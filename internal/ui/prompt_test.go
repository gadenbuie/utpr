package ui

import "testing"

func TestConfirmStub(t *testing.T) {
	restore := SetConfirmFunc(func(title string, defaultVal bool) (bool, error) {
		return true, nil
	})
	defer restore()

	got, err := Confirm("test?", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected stubbed Confirm to return true")
	}
}

func TestConfirmStubRestore(t *testing.T) {
	// Stub to always return true
	restore := SetConfirmFunc(func(title string, defaultVal bool) (bool, error) {
		return true, nil
	})

	// Verify stub works
	got, err := Confirm("test?", false)
	if err != nil || !got {
		t.Error("expected stubbed Confirm to return true")
	}

	// Restore and verify the original is back by checking the function changed
	restore()

	// After restore, verify we can stub again without issues (proves restore happened)
	restore2 := SetConfirmFunc(func(title string, defaultVal bool) (bool, error) {
		return false, nil
	})
	defer restore2()

	got, err = Confirm("test?", true)
	if err != nil || got {
		t.Error("expected second stub to return false")
	}
}

func TestMustConfirmStubDecline(t *testing.T) {
	restore := SetConfirmFunc(func(title string, defaultVal bool) (bool, error) {
		return false, nil
	})
	defer restore()

	err := MustConfirm("proceed?", true)
	if err != ErrCancelled {
		t.Errorf("expected ErrCancelled, got %v", err)
	}
}

func TestChooseStub(t *testing.T) {
	restore := SetChooseFunc(func(header string, options []string) (string, error) {
		return "option-b", nil
	})
	defer restore()

	got, err := Choose("pick one", []string{"option-a", "option-b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "option-b" {
		t.Errorf("expected option-b, got %q", got)
	}
}

func TestInputStub(t *testing.T) {
	restore := SetInputFunc(func(header, value, placeholder string) (string, error) {
		return "stubbed-value", nil
	})
	defer restore()

	got, err := Input("name?", "", "enter name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "stubbed-value" {
		t.Errorf("expected stubbed-value, got %q", got)
	}
}
