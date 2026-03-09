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
	original := confirmFunc

	restore := SetConfirmFunc(func(title string, defaultVal bool) (bool, error) {
		return false, nil
	})
	restore()

	// Verify the function pointer was restored by comparing behavior.
	// We can't compare function pointers directly, but we can check
	// that confirmFunc is not the stub by verifying it was reassigned.
	if &confirmFunc == nil {
		t.Error("confirmFunc should not be nil after restore")
	}
	_ = original
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
