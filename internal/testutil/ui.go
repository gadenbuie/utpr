package testutil

import (
	"fmt"
	"sync"

	"github.com/gadenbuie/utpr/internal/ui"
)

// StubConfirm replaces ui.Confirm to always return the given value.
// Call the returned function to restore the original.
func StubConfirm(val bool) func() {
	return ui.SetConfirmFunc(func(title string, defaultVal bool) (bool, error) {
		return val, nil
	})
}

// StubConfirmSequence replaces ui.Confirm to return values from the
// sequence in order. Panics if called more times than values provided.
func StubConfirmSequence(vals ...bool) func() {
	var mu sync.Mutex
	idx := 0
	return ui.SetConfirmFunc(func(title string, defaultVal bool) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		if idx >= len(vals) {
			panic(fmt.Sprintf("StubConfirmSequence: called %d times but only %d values provided", idx+1, len(vals)))
		}
		v := vals[idx]
		idx++
		return v, nil
	})
}

// StubChoose replaces ui.Choose to always return the given value.
// Call the returned function to restore the original.
func StubChoose(val string) func() {
	return ui.SetChooseFunc(func(header string, options []string) (string, error) {
		return val, nil
	})
}

// StubChooseSequence replaces ui.Choose to return values from the
// sequence in order. Panics if called more times than values provided.
func StubChooseSequence(vals ...string) func() {
	var mu sync.Mutex
	idx := 0
	return ui.SetChooseFunc(func(header string, options []string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if idx >= len(vals) {
			panic(fmt.Sprintf("StubChooseSequence: called %d times but only %d values provided", idx+1, len(vals)))
		}
		v := vals[idx]
		idx++
		return v, nil
	})
}

// StubInput replaces ui.Input to always return the given value.
// Call the returned function to restore the original.
func StubInput(val string) func() {
	return ui.SetInputFunc(func(header, value, placeholder string) (string, error) {
		return val, nil
	})
}

// StubInputSequence replaces ui.Input to return values from the
// sequence in order. Panics if called more times than values provided.
func StubInputSequence(vals ...string) func() {
	var mu sync.Mutex
	idx := 0
	return ui.SetInputFunc(func(header, value, placeholder string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if idx >= len(vals) {
			panic(fmt.Sprintf("StubInputSequence: called %d times but only %d values provided", idx+1, len(vals)))
		}
		v := vals[idx]
		idx++
		return v, nil
	})
}

// StubSpin replaces ui.Spin to skip the spinner and just call the action.
// Call the returned function to restore the original.
func StubSpin() func() {
	return ui.SetSpinFunc(func(title string, action func() error) error {
		return action()
	})
}
