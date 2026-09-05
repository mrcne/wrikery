// Package sync keeps the local store and the Wrike API in step.
// It pulls followed scopes and reference data into the cache and drains the outbox of queued writes.
// It is the only package that knows both sides.
package sync
