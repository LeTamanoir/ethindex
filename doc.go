/*
Package ethindex provides a resilient, state-machine driven Ethereum event indexer.

It is designed to simplify the process of backfilling historical logs and seamlessly
transitioning to live block subscription, while automatically handling chain
reorganizations and websocket disconnects.

# Features

  - Automatic retry and reconnect logic for transient RPC errors
  - Built-in checkpointing and state pruning
  - Graceful handling of Ethereum chain reorganizations
  - Separation of HTTP (backfilling) and WS (live) clients to avoid read limits

# Getting Started

To use the indexer, you must implement the [Handler] and [Cache] interfaces.
The Handler dictates which logs to filter and how to process them, while the Cache
manages saving the indexer's checkpoints to disk.

See the examples directory for a full implementation.
*/
package ethindex
