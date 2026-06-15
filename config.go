package ethindex

import (
	"log/slog"

	"github.com/ethereum/go-ethereum/ethclient"
)

type configureHandler struct{ i *Indexer }
type configureClients struct{ i *Indexer }
type configureCache struct{ i *Indexer }
type configureOptions struct{ i *Indexer }

// New begins the configuration chain for an Indexer.
func New() configureHandler {
	return configureHandler{&Indexer{
		newHeadsBuffer:     128,
		maxBlockRange:      10_000,
		maxConcurrentCalls: 100,
		checkpointInterval: 64,
		logger:             slog.Default(),
		stopCh:             make(chan struct{}),
	}}
}

// WithHandler sets the event handler for the indexer.
func (c configureHandler) WithHandler(h Handler) configureClients {
	c.i.handler = h
	return configureClients(c)
}

// WithClients sets the Ethereum RPC clients.
func (c configureClients) WithClients(http *ethclient.Client, ws *ethclient.Client) configureCache {
	c.i.http = http
	c.i.ws = ws
	return configureCache(c)
}

// WithCache sets the caching layer.
func (cc configureCache) WithCache(c Cache) configureOptions {
	cc.i.cache = c
	return configureOptions(cc)
}

// Build finalizes the configuration and returns the fully constructed Indexer.
func (c configureOptions) Build() *Indexer {
	return c.i
}

// WithRetryFunc sets the retry policy for RPC calls and block processing.
// Returning true triggers a reconnect; returning false halts the indexer.
func (c configureOptions) WithRetryFunc(f func(err error, attempt int) bool) configureOptions {
	c.i.retryFunc = f
	return c
}

// WithNewHeadsBuffer sets the capacity of the live block subscription channel.
// Default is 128.
func (c configureOptions) WithNewHeadsBuffer(n int) configureOptions {
	c.i.newHeadsBuffer = n
	return c
}

// WithMaxBlockRange sets the maximum block span per backfill RPC call.
// Default is 10,000.
func (c configureOptions) WithMaxBlockRange(r uint64) configureOptions {
	c.i.maxBlockRange = r
	return c
}

// WithCheckpointInterval sets how often (in blocks) the indexer saves state.
// Default is 64 (~2 epochs).
func (c configureOptions) WithCheckpointInterval(interval uint64) configureOptions {
	c.i.checkpointInterval = interval
	return c
}

// WithLogger sets the structured logger.
// Default is slog.Default().
func (c configureOptions) WithLogger(l *slog.Logger) configureOptions {
	c.i.logger = l
	return c
}
