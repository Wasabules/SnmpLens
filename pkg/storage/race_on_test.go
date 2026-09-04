//go:build race

package storage

// raceEnabled reports whether the binary was built with -race.
//
// The detector instruments every memory access, which makes SQLite several
// times slower and turns any assertion about elapsed time into noise: the trim
// benchmark measured 20 ms without it and 73 SECONDS with it. CI runs -race, so
// a timing test that does not know this is a test that fails for a reason that
// has nothing to do with the code.
const raceEnabled = true
