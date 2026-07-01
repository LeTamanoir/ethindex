package ethindexer

// Options configures an Indexer.
type Options struct {
	// Client provides access to Ethereum logs and block headers.
	Client ChainReader

	// Handler receives logs and owns the indexed state.
	Handler Handler

	// Filter specifies which logs the indexer fetches.
	Filter Filter

	// Store persists checkpoints and cached log batches.
	Store BlobStore

	// LogFunc receives indexer log events.
	LogFunc func(msg string, args ...any)

	Config *Config
}

// Config holds the indexer's tunables.
type Config struct {
	// MaxBlockRange is the maximum block span per backfill request.
	MaxBlockRange uint64

	// FinalityDepth is the block depth considered finalized.
	FinalityDepth uint64

	// MaxConcurrency bounds concurrent header fetches.
	MaxConcurrency int
}

var DefaultConfig = Config{
	MaxBlockRange:  10_000,
	FinalityDepth:  64,
	MaxConcurrency: 16,
}
