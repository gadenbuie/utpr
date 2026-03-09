package ui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh/spinner"
)

var spinFunc = defaultSpin

func defaultSpin(title string, fn func() error) error {
	return spinner.New().
		Title(title).
		ActionWithErr(func(_ context.Context) error {
			return fn()
		}).
		Run()
}

// Spin shows a spinner with a title while running the given function.
// If the function returns an error, the spinner stops and the error is returned.
func Spin(title string, fn func() error) error {
	return spinFunc(title, fn)
}

// SetSpinFunc replaces the Spin implementation. Returns a restore function.
func SetSpinFunc(fn func(string, func() error) error) func() {
	old := spinFunc
	spinFunc = fn
	return func() { spinFunc = old }
}

// SpinWithResult shows a spinner while running a function that returns a value.
func SpinWithResult[T any](title string, fn func() (T, error)) (T, error) {
	var result T
	var fnErr error
	err := spinner.New().
		Title(title).
		ActionWithErr(func(_ context.Context) error {
			result, fnErr = fn()
			return fnErr
		}).
		Run()
	if err != nil && fnErr == nil {
		return result, err
	}
	return result, fnErr
}

// Die prints an error message and returns a formatted error.
func Die(msg string) error {
	Error(msg)
	return fmt.Errorf("%s", msg)
}

// Dief prints a formatted error message and returns a formatted error.
func Dief(format string, args ...any) error {
	return Die(fmt.Sprintf(format, args...))
}
