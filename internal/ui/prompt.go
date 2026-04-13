package ui

import (
	"errors"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

var ErrCancelled = errors.New("cancelled")

// confirmFunc, chooseFunc, and inputFunc are the implementations for their
// respective public functions. Tests can replace them via Set*Func().
// These are not safe for concurrent use; do not use t.Parallel() in tests
// that stub these functions.
var confirmFunc = defaultConfirm

func defaultConfirm(title string, defaultVal bool) (bool, error) {
	confirmed := defaultVal
	err := huh.NewConfirm().
		Title(title).
		Affirmative("Yes").
		Negative("No").
		WithButtonAlignment(lipgloss.Left).
		Value(&confirmed).
		Run()
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return false, ErrCancelled
		}
		return false, err
	}
	return confirmed, nil
}

// Confirm shows a yes/no confirmation prompt. Returns the user's choice.
// defaultVal sets the pre-selected answer.
func Confirm(title string, defaultVal bool) (bool, error) {
	return confirmFunc(title, defaultVal)
}

// SetConfirmFunc replaces the Confirm implementation. Returns a restore function.
func SetConfirmFunc(fn func(string, bool) (bool, error)) func() {
	old := confirmFunc
	confirmFunc = fn
	return func() { confirmFunc = old }
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

var inputFunc = defaultInput

func defaultInput(header, value, placeholder string) (string, error) {
	result := value
	err := huh.NewInput().
		Title(header).
		Value(&result).
		Placeholder(placeholder).
		Run()
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", ErrCancelled
		}
		return "", err
	}
	return result, nil
}

// Input shows a single-line text input with an optional default value and placeholder.
func Input(header, value, placeholder string) (string, error) {
	return inputFunc(header, value, placeholder)
}

// SetInputFunc replaces the Input implementation. Returns a restore function.
func SetInputFunc(fn func(string, string, string) (string, error)) func() {
	old := inputFunc
	inputFunc = fn
	return func() { inputFunc = old }
}

var chooseFunc = defaultChoose

func defaultChoose(header string, options []string) (string, error) {
	if len(options) == 0 {
		return "", errors.New("no options provided")
	}

	huhOpts := make([]huh.Option[string], len(options))
	for i, opt := range options {
		huhOpts[i] = huh.NewOption(opt, opt)
	}

	var selected string
	sel := huh.NewSelect[string]().
		Title(header).
		Options(huhOpts...).
		Value(&selected).
		Height(SelectHeight(len(options)))
	err := huh.NewForm(huh.NewGroup(sel)).WithShowHelp(true).Run()
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", ErrCancelled
		}
		return "", err
	}
	return selected, nil
}

// Choose shows a single-select picker with string options and filtering enabled.
// Returns the selected option's value.
func Choose(header string, options []string) (string, error) {
	return chooseFunc(header, options)
}

// SetChooseFunc replaces the Choose implementation. Returns a restore function.
func SetChooseFunc(fn func(string, []string) (string, error)) func() {
	old := chooseFunc
	chooseFunc = fn
	return func() { chooseFunc = old }
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
	sel := huh.NewSelect[T]().
		Title(header).
		Options(options...).
		Value(&selected).
		Height(SelectHeight(len(options)))
	err := huh.NewForm(huh.NewGroup(sel)).WithShowHelp(true).Run()
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return zero, ErrCancelled
		}
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
	sel := huh.NewMultiSelect[T]().
		Title(header).
		Options(options...).
		Value(&selected).
		Height(SelectHeight(len(options))).
		Filterable(true)
	err := huh.NewForm(huh.NewGroup(sel)).WithShowHelp(true).Run()
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil, ErrCancelled
		}
		return nil, err
	}
	return selected, nil
}
