package cmd

import "github.com/cli/browser"

func openURL(url string) error {
	return browser.OpenURL(url)
}
