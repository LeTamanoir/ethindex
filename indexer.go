package ethindex

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"golang.org/x/sync/errgroup"
)

// Indexer indexes Ethereum logs from a finalized block onward, handling reorgs and checkpointing.
type Indexer struct {
	c Client
	h Handler
	f Filter
	l Logger
	s Store

	finalityDepth uint64
	maxBlockRange uint64
	maxConcurrent int

	synced      bool
	head        blockRef
	dangling    blockRef   // head of the pending dangling checkpoint
	pendingSave chan error // delivers the pending dangling save result
}

// NewIndexer builds the indexer.
func NewIndexer(c Client, h Handler, f Filter, s Store, l Logger, cfg Config) *Indexer {
	cfg.applyDefaults()
	if l == nil {
		l = slog.Default()
	}
	return &Indexer{
		c: c,
		h: h,
		f: f,
		s: s,
		l: l,

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

	i.l.Info("Syncing indexer",
		"from_block", i.f.FromBlock,
		"finality_depth", i.finalityDepth,
		"max_block_range", i.maxBlockRange,
		"max_concurent", i.maxConcurrent)

	if err := i.restore(ctx); err != nil {
		return err
	}

	if err := i.sync(ctx); err != nil {
		return err
	}

	i.l.Info("Indexer ready", "head", i.head.Number)

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
		i.l.Warn("Ignoring older head",
			"current", idxNum,
			"received", headNum)

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

// restore loads and applies the finalized checkpoint, if one exists.
func (i *Indexer) restore(ctx context.Context) error {
	cp, ok, err := loadCheckpoint(ctx, i.s, finalized)
	if err != nil {
		return fmt.Errorf("load finalized: %w", err)
	}
	if !ok {
		return nil
	}
	return i.restoreCheckpoint(ctx, cp)
}

// sync backfills from the restored head (or FromBlock on a fresh
// run) up to the node's finalized block, then saves a finalized checkpoint.
func (i *Indexer) sync(ctx context.Context) error {
	start := time.Now()
	final, err := i.c.HeaderByNumber(ctx, big.NewInt(int64(rpc.FinalizedBlockNumber)))
	if err != nil {
		return err
	}
	i.l.Debug("Fetched finalized header",
		"number", final.Number.Uint64(),
		"duration", time.Since(start))

	from := i.f.FromBlock
	if i.head != (blockRef{}) {
		from = i.head.Number + 1
	}
	to := final.Number.Uint64()

	if from > to {
		i.l.Info("No backfill required", "head", i.head.Number, "finalized", to)

		return nil
	}

	if err := i.backfillFinalized(ctx, from, to); err != nil {
		return fmt.Errorf("backfill: %w", err)
	}

	i.head = blockRef{Number: to, Hash: final.Hash()}

	snapSt := time.Now()
	state, err := i.h.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	snapDur := time.Since(snapSt)

	saveSt := time.Now()
	if err := saveCheckpoint(ctx, i.s, finalized, checkpoint{i.head, state}); err != nil {
		return fmt.Errorf("save finalized: %w", err)
	}
	saveDur := time.Since(saveSt)

	i.l.Info("Saved finalized checkpoint",
		"head", i.head.Number,
		"snapshot", snapDur,
		"save", saveDur)

	return nil
}

// backfillUnfinalized fetches and processes the missing headers in [from, to].
//
// The range is assumed to be unfinalized, so each header is fetched
// individually and logs are queried by block hash to preserve reorg safety.
func (i *Indexer) backfillUnfinalized(ctx context.Context, from, to uint64) error {
	start := time.Now()

	heads, err := i.headersRange(ctx, from, to)
	if err != nil {
		return fmt.Errorf("headers range: %w", err)
	}

	i.l.Info("Filled missing heads",
		"from", from,
		"to", to,
		"duration", time.Since(start))

	for _, h := range heads {
		if err := i.Process(ctx, h); err != nil {
			return err
		}
	}

	return nil
}

// waitPending waits for the current pending save, if any.
func (i *Indexer) waitPending(ctx context.Context) error {
	if i.pendingSave == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-i.pendingSave:
		i.pendingSave = nil
		return err
	}
}

// handleReorg restores the finalized checkpoint and reprocesses the divergent head.
func (i *Indexer) handleReorg(ctx context.Context, h *types.Header) error {
	if err := i.waitPending(ctx); err != nil {
		return err
	}

	i.l.Warn("Reorg detected",
		"head", i.head.Number,
		"expected_parent", i.head.Hash,
		"got_parent", h.ParentHash)

	i.head = blockRef{}
	i.dangling = blockRef{}

	cp, ok, err := loadCheckpoint(ctx, i.s, finalized)
	if err != nil {
		return fmt.Errorf("load finalized: %w", err)
	}
	if !ok {
		return errors.New("reorg detected but no finalized checkpoint found")
	}

	if err := i.restoreCheckpoint(ctx, cp); err != nil {
		return err
	}

	return i.Process(ctx, h)
}

// restoreCheckpoint restores handler state from a checkpoint and records the head.
func (i *Indexer) restoreCheckpoint(ctx context.Context, cp *checkpoint) error {
	start := time.Now()

	if err := i.h.Restore(ctx, cp.State); err != nil {
		return fmt.Errorf("restore: %w", err)
	}

	i.head = cp.Head

	i.l.Info("Restored from finalized checkpoint",
		"head", cp.Head.Number,
		"duration", time.Since(start))

	return nil
}

// processHead handles a new header and assumes it is strictly consecutive to idx.head.
func (i *Indexer) processHead(ctx context.Context, h *types.Header) error {
	start := time.Now()

	logs, err := i.c.FilterLogs(ctx, i.f.blockQuery(h.Hash()))
	if err != nil {
		return fmt.Errorf("filter logs: %w", err)
	}

	if err := i.h.Process(ctx, logs); err != nil {
		return fmt.Errorf("process logs: %w", err)
	}

	i.head = blockRef{Number: h.Number.Uint64(), Hash: h.Hash()}

	i.l.Debug("Processed new head",
		"number", h.Number.Uint64(),
		"logs", len(logs),
		"duration", time.Since(start))

	return i.checkpoint(ctx)
}

// checkpoint saves a dangling checkpoint if none is pending, then promotes the
// dangling checkpoint to finalized once the head has aged past finalityDepth.
func (i *Indexer) checkpoint(ctx context.Context) error {
	if i.dangling == (blockRef{}) {
		return i.saveDanglingAsync(ctx)
	}

	if i.head.Number >= i.dangling.Number+i.finalityDepth {
		return i.promoteDangling(ctx)
	}

	return nil
}

// promoteDangling moves the dangling checkpoint to finalized.
func (i *Indexer) promoteDangling(ctx context.Context) error {
	if err := i.waitPending(ctx); err != nil {
		return err
	}

	start := time.Now()
	if err := i.s.Move(ctx, string(dangling), string(finalized)); err != nil {
		return fmt.Errorf("move: %w", err)
	}

	i.l.Info("Promoted dangling checkpoint to finalized",
		"head", i.dangling.Number,
		"duration", time.Since(start))

	i.dangling = blockRef{}

	return nil
}

// saveDanglingAsync persists a dangling checkpoint asynchronously.
func (i *Indexer) saveDanglingAsync(ctx context.Context) error {
	start := time.Now()

	state, err := i.h.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	snapDur := time.Since(start)

	cp := checkpoint{Head: i.head, State: state}

	saveCh := make(chan error, 1)
	i.pendingSave = saveCh

	go func() {
		saveSt := time.Now()
		err := saveCheckpoint(ctx, i.s, dangling, cp)

		if err != nil {
			i.l.Error("Async save dangling failed",
				"head", cp.Head.Number,
				"error", err)
		} else {
			i.l.Debug("Saved dangling checkpoint",
				"head", cp.Head.Number,
				"snapshot", snapDur,
				"save", time.Since(saveSt),
				"duration", time.Since(start))
		}

		saveCh <- err
	}()

	i.dangling = cp.Head

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

// logsRange returns logs for [from, to], caching fetched results.
func (i *Indexer) logsRange(ctx context.Context, from, to uint64) ([]types.Log, error) {
	q := i.f.rangeQuery(from, to)

	cached, err := loadLogs(ctx, i.s, q)
	if err != nil {
		return nil, fmt.Errorf("load logs: %w", err)
	}
	if cached != nil {
		return cached, nil
	}

	logs, err := i.c.FilterLogs(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("filter logs: %w", err)
	}

	if err := saveLogs(ctx, i.s, q, logs); err != nil {
		return nil, fmt.Errorf("save logs: %w", err)
	}

	return logs, nil
}

// backfillFinalized fetches and processes logs over [from, to] in chunks.
//
// The range is assumed to be finalized, allowing logs to be queried by block
// range with FilterLogs instead of by block hash. This is more efficient but
// does not provide reorg safety.
func (i *Indexer) backfillFinalized(ctx context.Context, from, to uint64) error {
	chunks := chunkBlockRange(from, to, i.maxBlockRange)

	i.l.Info("Backfilling",
		"from", from,
		"to", to,
		"chunks", len(chunks))

	start := time.Now()

	for _, ch := range chunks {
		chStart := time.Now()

		logs, err := i.logsRange(ctx, ch.from, ch.to)
		if err != nil {
			return fmt.Errorf("get logs: %w", err)
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		if err := i.h.Process(ctx, logs); err != nil {
			return fmt.Errorf("process logs: %w", err)
		}

		i.l.Debug("Backfill chunk processed",
			"from", ch.from,
			"to", ch.to,
			"logs", len(logs),
			"duration", time.Since(chStart))
	}

	i.l.Info("Backfill complete",
		"from", from,
		"to", to,
		"duration", time.Since(start))

	return nil
}
