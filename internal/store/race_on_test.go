//go:build race

package store

// The race detector drops a quarter of what goes back into a sync.Pool, see Pool.Put in sync/pool.go,
// so an allocation count that relies on a pool is not meaningful under -race.
const raceEnabled = true
