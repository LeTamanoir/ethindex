package ethindex

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"golang.org/x/sync/errgroup"
)

type BlockRef struct {
	Number uint64
	Hash   common.Hash
}

// ChainReader provides access to Ethereum logs and block headers.
type ChainReader interface {
	FilterLogs(context.Context, ethereum.FilterQuery) ([]types.Log, error)
	HeaderByNumber(context.Context, *big.Int) (*types.Header, error)
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

// Indexer indexes Ethereum logs from a finalized block onward, handling reorgs and checkpointing.
type Indexer struct {
	cr ChainReader
	h  Handler
	f  Filter
	cp Checkpointer

	finalityDepth uint64
	maxBlockRange uint64
	maxConcurrent int

	synced bool
	head   BlockRef
}

// NewIndexer builds the indexer.
func NewIndexer(cr ChainReader, h Handler, f Filter, cp Checkpointer, cfg *Config) *Indexer {
	cfg.applyDefaults()
	return &Indexer{
		cr: cr,
		h:  h,
		f:  f,
		cp: cp,

		finalityDepth: cfg.FinalityDepth,
		maxBlockRange: cfg.MaxBlockRange,
		maxConcurrent: cfg.MaxConcurrency,
	}
}

// Sync restores state and catches up to the current finalized head.
func (i *Indexer) Sync(ctx context.Context) error {
	if i.synced {
		return nil
	}

	if _, err := i.restoreFinalized(ctx); err != nil {
		return err
	}

	if err := i.syncFinalized(ctx); err != nil {
		return err
	}

	i.synced = true

	return nil
}

// Process ingests a new head and handles gaps and reorgs.
func (i *Indexer) Process(ctx context.Context, h *types.Header) error {
	if !i.synced {
		return errors.New("indexer is not synced")
	}

	idxNum := i.head.Number
	headNum := h.Number.Uint64()

	if headNum <= idxNum {
		return nil
	}

	// Ensure contiguous block processing.
	if headNum != idxNum+1 {
		return i.backfillUnfinalized(ctx, idxNum+1, headNum)
	}

	// Ensure chain continuity.
	if i.head.Hash != h.ParentHash {
		return i.handleReorg(ctx, h)
	}

	return i.processHead(ctx, h)
}

// restoreFinalized loads and applies the finalized checkpoint, if one exists.
func (i *Indexer) restoreFinalized(ctx context.Context) (bool, error) {
	cp, err := i.cs.Load(ctx)
	if err != nil {
		return false, fmt.Errorf("load checkpoint: %w", err)
	}
	if cp == nil {
		return false, nil
	}

	if err := i.h.Restore(ctx, cp.State); err != nil {
		return false, fmt.Errorf("handler restore: %w", err)
	}

	i.head = cp.Head

	return true, nil
}

// syncFinalized backfills to the current finalized head and saves a finalized checkpoint.
func (i *Indexer) syncFinalized(ctx context.Context) error {
	final, err := i.c.HeaderByNumber(ctx, big.NewInt(int64(rpc.FinalizedBlockNumber)))
	if err != nil {
		return err
	}

	from := i.f.FromBlock
	if i.head != (BlockRef{}) {
		from = i.head.Number + 1
	}
	to := final.Number.Uint64()

	if from > to {
		return nil
	}

	if err := i.backfillFinalized(ctx, from, to); err != nil {
		return fmt.Errorf("backfill: %w", err)
	}

	i.head = BlockRef{Number: to, Hash: final.Hash()}

	state, err := i.h.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}

	if err := i.cs.Stage(ctx, Checkpoint{i.head, state}); err != nil {
		return fmt.Errorf("stage finalized: %w", err)
	}
	if err := i.cs.Commit(ctx); err != nil {
		return fmt.Errorf("commit finalized: %w", err)
	}

	return nil
}

// backfillUnfinalized fetches and processes the missing headers in [from, to].
//
// The range is assumed to be unfinalized, so each header is fetched
// individually and logs are queried by block hash to preserve reorg safety.
func (i *Indexer) backfillUnfinalized(ctx context.Context, from, to uint64) error {
	heads, err := i.headersRange(ctx, from, to)
	if err != nil {
		return fmt.Errorf("headers range: %w", err)
	}

	for _, h := range heads {
		if err := i.Process(ctx, h); err != nil {
			return err
		}
	}

	return nil
}

// handleReorg restores the finalized checkpoint and reprocesses the divergent head.
func (i *Indexer) handleReorg(ctx context.Context, h *types.Header) error {
	i.head = BlockRef{}

	ok, err := i.restoreFinalized(ctx)
	if err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	if !ok {
		return errors.New("reorg detected but no finalized checkpoint found")
	}

	return i.Process(ctx, h)
}

// processHead handles a new header and assumes it is strictly consecutive to idx.head.
func (i *Indexer) processHead(ctx context.Context, h *types.Header) error {
	logs, err := i.c.FilterLogs(ctx, i.f.blockQuery(h.Hash()))
	if err != nil {
		return fmt.Errorf("filter logs: %w", err)
	}

	if err := i.h.Process(ctx, logs); err != nil {
		return fmt.Errorf("process logs: %w", err)
	}

	i.head = BlockRef{Number: h.Number.Uint64(), Hash: h.Hash()}

	return i.checkpoint(ctx)
}

// checkpoint saves a dangling checkpoint if none is pending, then promotes the
// dangling checkpoint to finalized once the head has aged past finalityDepth.
func (i *Indexer) checkpoint(ctx context.Context) error {
	staged := i.cs.Staged()

	if staged == nil {
		data, err := i.h.Snapshot(ctx)
		if err != nil {
			return fmt.Errorf("handler snapshot: %w", err)
		}

		return i.cs.Stage(ctx, Checkpoint{i.head, data})
	}

	if i.head.Number >= staged.Number+i.finalityDepth {
		return i.cs.Commit(ctx)
	}

	return nil
}

// headersRange fetches headers [from, to] concurrently, preserving order.
func (idx *Indexer) headersRange(ctx context.Context, from, to uint64) ([]*types.Header, error) {
	if from > to {
		panic("invalid range: from > to")
	}

	total := to - from + 1

	heads := make([]*types.Header, total)
	eg, ctx := errgroup.WithContext(ctx)

	eg.SetLimit(idx.maxConcurrent)

	for i := range total {
		eg.Go(func() error {
			h, e := idx.c.HeaderByNumber(ctx, big.NewInt(int64(from+i)))
			heads[i] = h
			return e
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return heads, nil
}

// backfillFinalized fetches and processes logs over [from, to] in chunks.
//
// The range is assumed to be finalized, allowing logs to be queried by block
// range with FilterLogs instead of by block hash. This is more efficient but
// does not provide reorg safety.
func (i *Indexer) backfillFinalized(ctx context.Context, from, to uint64) error {
	chunks := chunkBlockRange(from, to, i.maxBlockRange)

	for _, ch := range chunks {
		logs, err := i.c.FilterLogs(ctx, i.f.rangeQuery(ch.from, ch.to))
		if err != nil {
			return fmt.Errorf("get logs: %w", err)
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		if err := i.h.Process(ctx, logs); err != nil {
			return fmt.Errorf("process logs: %w", err)
		}
	}

	return nil
}
