package cmd

import (
	"strings"
	"testing"

	"github.com/gadenbuie/utpr/internal/ui"
)

func TestConfirmPRMergeRequiresYesWithoutTTY(t *testing.T) {
	previousYes := prMergeFlagYes
	t.Cleanup(func() { prMergeFlagYes = previousYes })
	prMergeFlagYes = false

	restoreTTY := ui.SetTTYFuncs(func() bool { return false }, func() bool { return false })
	t.Cleanup(restoreTTY)

	err := confirmPRMerge(7, "Fix CI logs", "squash commit")
	if err == nil || !strings.Contains(err.Error(), "pass --yes") {
		t.Fatalf("confirmPRMerge() error = %v, want guidance to pass --yes", err)
	}

	prMergeFlagYes = true
	if err := confirmPRMerge(7, "Fix CI logs", "squash commit"); err != nil {
		t.Errorf("confirmPRMerge() with --yes = %v, want nil", err)
	}
}
