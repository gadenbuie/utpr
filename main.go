package main

import (
	"errors"
	"os"

	"github.com/gadenbuie/utpr/cmd"
	"github.com/gadenbuie/utpr/internal/ui"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd.SetVersionInfo(version, commit, date)
	if err := cmd.Execute(); err != nil {
		if errors.Is(err, ui.ErrCancelled) {
			os.Exit(130)
		}
		os.Exit(1)
	}
}
