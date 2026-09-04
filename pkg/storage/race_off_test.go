//go:build !race

package storage

// raceEnabled reports whether the binary was built with -race. See race_on_test.go.
const raceEnabled = false
