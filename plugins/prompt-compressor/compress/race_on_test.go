//go:build race

package compress

// raceEnabled: the race detector defeats sync.Pool, so allocation bounds
// do not hold under it.
const raceEnabled = true
