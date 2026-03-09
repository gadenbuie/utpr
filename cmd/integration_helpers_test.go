//go:build integration

package cmd

import (
	"testing"

	"github.com/gadenbuie/utpr/internal/remote"
)

// seedRemoteCache sets a standard "ours" layout remote config for testing.
// Uses t.Cleanup to reset the cache when the test finishes.
func seedRemoteCache(t *testing.T) {
	t.Helper()
	remote.SetCacheForTest(&remote.Config{
		Layout:        "ours",
		SourceRemote:  "origin",
		PushRemote:    "origin",
		DefaultBranch: "main",
	})
	t.Cleanup(func() {
		remote.ResetCacheForTest()
	})
}
