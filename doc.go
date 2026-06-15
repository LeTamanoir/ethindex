// Package ethindex implements a state-machine driven Ethereum log indexer.
//
// The indexer automatically manages the transition from backfilling historical
// events via HTTP to streaming live blocks via WebSockets. It includes built-in
// handling for transient RPC errors, websocket disconnects, and chain reorganizations.
//
// To use ethindex, callers must provide two interfaces: a [Handler] and a [Cache].
// The Handler dictates log filtering, event processing, and state snapshotting.
// The Cache is used to persist these state checkpoints across restarts and to
// cache expensive RPC calls.
package ethindex
