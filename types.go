package ethindex

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// BlockRef is a (number, hash) pair identifying a block.
type BlockRef struct {
	Number uint64
	Hash   common.Hash
}

// Filter specifies which logs the indexer fetches.
type Filter struct {
	// FromBlock is the first block to index.
	FromBlock uint64

	// Addresses restrict logs to the given contract addresses.
	// See [ethereum.FilterQuery.Addresses].
	Addresses []common.Address

	// Topics restrict logs by indexed event topics.
	// See [ethereum.FilterQuery.Topics].
	Topics [][]common.Hash
}

// rangeQuery builds a block-range FilterQuery over [from, to].
func (f Filter) rangeQuery(from, to uint64) ethereum.FilterQuery {
	return ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(from),
		ToBlock:   new(big.Int).SetUint64(to),
		Addresses: f.Addresses,
		Topics:    f.Topics,
	}
}

// blockQuery builds a single-block FilterQuery anchored to hash.
func (f Filter) blockQuery(hash common.Hash) ethereum.FilterQuery {
	return ethereum.FilterQuery{
		BlockHash: &hash,
		Addresses: f.Addresses,
		Topics:    f.Topics,
	}
}

// Handler defines the application-specific indexing logic.
type Handler interface {
	// Snapshot returns the current handler state.
	Snapshot(context.Context) ([]byte, error)

	// Restore restores a previously captured state.
	Restore(context.Context, []byte) error

	// Process applies matching logs in block order.
	Process(context.Context, []types.Log) error
}

// Client provides access to Ethereum logs and block headers.
type Client interface {
	FilterLogs(context.Context, ethereum.FilterQuery) ([]types.Log, error)
	HeaderByNumber(context.Context, *big.Int) (*types.Header, error)
}

// Logger records operational messages.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// Config holds the indexer's configuration.
type Config struct {
	// Client fetches logs and block headers.
	Client Client

	// Logger records operational messages.
	Logger Logger

	// Handler processes matching logs and snapshots state.
	Handler Handler

	// Filter specifies which logs the indexer fetches.
	Filter Filter

	// Store persists checkpoints and handler state.
	Store Store

	// MaxBlockRange is the maximum block span per backfill request.
	// Defaults to 10,000.
	MaxBlockRange uint64

	// FinalityDepth is the block depth considered finalized.
	// Defaults to 64.
	FinalityDepth uint64

	// MaxConcurrency bounds concurrent header fetches.
	// Defaults to 16.
	MaxConcurrency int
}

// Validate checks required fields and applies defaults.
func (c *Config) Validate() error {
	if c.Client == nil {
		return fmt.Errorf("client is required")
	}
	if c.Store == nil {
		return fmt.Errorf("store is required")
	}
	if c.Handler == nil {
		return fmt.Errorf("handler is required")
	}

	// Apply defaults
	if c.FinalityDepth == 0 {
		c.FinalityDepth = 64
	}
	if c.MaxBlockRange == 0 {
		c.MaxBlockRange = 10_000
	}
	if c.MaxConcurrency == 0 {
		c.MaxConcurrency = 16
	}
	if c.Logger == nil {
		c.Logger = noopLogger{}
	}

	return nil
}

// Store provides keyed byte storage.
type Store interface {
	// Read returns the data stored under key. A missing key returns (nil, nil).
	Read(ctx context.Context, key string) ([]byte, error)

	// Write stores data under key, replacing any existing value.
	Write(ctx context.Context, key string, blob []byte) error

	// Move atomically transfers data from srcKey to dstKey, replacing any
	// existing value under dstKey.
	Move(ctx context.Context, srcKey, dstKey string) error
}

// noopLogger is the default Logger when Config.Logger is nil.
type noopLogger struct{}

var _ Logger = (*noopLogger)(nil)

func (noopLogger) Debug(string, ...any) {}
func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}
