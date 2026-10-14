package cmd

import (
	"testing"

	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/testutil"
)

// Reset cached remote.Detect() config around every test that creates a fresh
// repo, so a config detected in one repo (or in the utpr repo itself) can't
// leak into another test running in a different repo.
func init() {
	testutil.NewRepoHooks = append(testutil.NewRepoHooks, func(t *testing.T) {
		remote.ResetCacheForTest()
		t.Cleanup(remote.ResetCacheForTest)
	})
}
