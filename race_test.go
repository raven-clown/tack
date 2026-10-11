//go:build race

package tack

// raceEnabled skips the allocation budgets, which the race detector inflates.
const raceEnabled = true
