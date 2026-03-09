package ui

import (
	"errors"

	"github.com/charmbracelet/huh"
)

var ErrCancelled = errors.New("cancelled")

// Confirm shows a yes/no confirmation prompt. Returns the user's choice.
// defaultVal sets the pre-selected answer.
func Confirm(title string, defaultVal bool) (bool, error) {
	confirmed := defaultVal
	err := huh.NewConfirm().
		Title(title).
		Affirmative("Yes").
		Negative("No").
		Value(&confirmed).
		Run()
	if err != nil {
		return false, err
	}
	return confirmed, nil
}

// MustConfirm shows a confirmation prompt and returns an error if the user
// declines or cancels.
func MustConfirm(title string, defaultVal bool) error {
	confirmed, err := Confirm(title, defaultVal)
	if err != nil {
		return err
	}
	if !confirmed {
		return ErrCancelled
	}
	return nil
}

// Input shows a single-line text input with an optional default value and placeholder.
func Input(header, value, placeholder string) (string, error) {
	result := value
	err := huh.NewInput().
		Title(header).
		Value(&result).
		Placeholder(placeholder).
		Run()
	if err != nil {
		return "", err
	}
	return result, nil
}

// Choose shows a single-select picker with string options and filtering enabled.
// Returns the selected option's value.
func Choose(header string, options []string) (string, error) {
	if len(options) == 0 {
		return "", errors.New("no options provided")
	}

	huhOpts := make([]huh.Option[string], len(options))
	for i, opt := range options {
		huhOpts[i] = huh.NewOption(opt, opt)
	}

	var selected string
	err := huh.NewSelect[string]().
		Title(header).
		Options(huhOpts...).
		Value(&selected).
		Filtering(true).
		Run()
	if err != nil {
		return "", err
	}
	return selected, nil
}

// ChooseWithOptions shows a single-select picker with pre-built options and
// filtering enabled. The display key can contain ANSI styling; the value is
// returned cleanly.
func ChooseWithOptions[T comparable](header string, options []huh.Option[T]) (T, error) {
	var zero T
	if len(options) == 0 {
		return zero, errors.New("no options provided")
	}

	var selected T
	err := huh.NewSelect[T]().
		Title(header).
		Options(options...).
		Value(&selected).
		Filtering(true).
		Run()
	if err != nil {
		return zero, err
	}
	return selected, nil
}

// ChooseMultiWithOptions shows a multi-select picker with pre-built options
// and filtering enabled. Returns the selected values.
func ChooseMultiWithOptions[T comparable](header string, options []huh.Option[T]) ([]T, error) {
	if len(options) == 0 {
		return nil, errors.New("no options provided")
	}

	var selected []T
	err := huh.NewMultiSelect[T]().
		Title(header).
		Options(options...).
		Value(&selected).
		Filterable(true).
		Run()
	if err != nil {
		return nil, err
	}
	return selected, nil
}
