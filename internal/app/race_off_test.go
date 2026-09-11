//go:build !race

package app

// raceEnabledMain reports whether the binary was built with -race. See race_on_test.go.
const raceEnabledMain = false
