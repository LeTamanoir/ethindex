package ethindex

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
)

type BlockRef struct {
	Number uint64
	Hash   common.Hash
}

type Indexer struct {
	c Client
	h Handler
	f Filter
	s Store
	l *slog.Logger

	// Configs
	finalityDepth uint64

	// State
	dangling BlockRef
	head     BlockRef
}

func NewIndexer(ctx context.Context, cfg Config) (*Indexer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	idx := &Indexer{
		c: cfg.Client,
		f: cfg.Filter,
		h: cfg.Handler,
		s: cfg.Store,
		l: cfg.Logger,

		finalityDepth: cfg.FinalityDepth,
	}

	cp, ok, err := loadFinalized(ctx, idx.s)
	if err != nil {
		return nil, fmt.Errorf("load finalized: %w", err)
	}
	if ok {
		if err := idx.h.Restore(ctx, cp.State); err != nil {
			return nil, fmt.Errorf("restore finalized: %w", err)
		}
		idx.head = cp.Head
	}

	final, err := idx.c.HeaderByNumber(ctx, big.NewInt(int64(rpc.FinalizedBlockNumber)))
	if err != nil {
		return nil, err
	}

	from := idx.f.FromBlock
	if idx.head != (BlockRef{}) {
		from = idx.head.Number + 1
	}
	to := final.Number.Uint64()

	if from <= to {
		if err := backfill(ctx, idx.c, idx.h, idx.s, idx.f, from, to, cfg.MaxBlockRange); err != nil {
			return nil, fmt.Errorf("backfill: %w", err)
		}

		idx.head = BlockRef{Number: to, Hash: final.Hash()}

		state, err := idx.h.Snapshot(ctx)
		if err != nil {
			return nil, fmt.Errorf("snapshot: %w", err)
		}

		if err := saveFinalized(ctx, idx.s, checkpoint{idx.head, state}); err != nil {
			return nil, fmt.Errorf("save finalized: %w", err)
		}
	}

	return idx, nil
}

// Process ingests a new head and handles reorgs.
func (idx *Indexer) Process(ctx context.Context, h *types.Header) error {
	inum := idx.head.Number
	hnum := h.Number.Uint64()

	if hnum <= inum {
		return fmt.Errorf("can not process old heads")
	}

	// Enforce we only process strictly sequential heads
	if hnum != inum+1 {
		heads, err := headersRange(ctx, idx.c, inum+1, hnum)
		if err != nil {
			return fmt.Errorf("headers range: %w", err)
		}

		for _, h := range heads {
			if err := idx.Process(ctx, h); err != nil {
				return err
			}
		}

		return nil
	}

	// Handle reorg
	if idx.head.Hash != h.ParentHash {
		idx.head = BlockRef{}
		idx.dangling = BlockRef{}

		cp, ok, err := loadFinalized(ctx, idx.s)
		if err != nil {
			return fmt.Errorf("load finalized: %w", err)
		}
		if !ok {
			return errors.New("reorg detected but no finalized checkpoint found")
		}
		if err := idx.h.Restore(ctx, cp.State); err != nil {
			return fmt.Errorf("restore: %w", err)
		}

		idx.head = cp.Head

		return idx.Process(ctx, h)
	}

	logs, err := idx.c.FilterLogs(ctx, newFilterQuery(idx.f, hnum, hnum))
	if err != nil {
		return fmt.Errorf("get logs: %w", err)
	}

	if err := idx.h.Process(ctx, logs); err != nil {
		return fmt.Errorf("process logs: %w", err)
	}

	idx.head = BlockRef{Number: hnum, Hash: h.Hash()}

	if idx.dangling == (BlockRef{}) {
		state, err := idx.h.Snapshot(ctx)
		if err != nil {
			return fmt.Errorf("snapshot: %w", err)
		}

		cp := checkpoint{Head: idx.head, State: state}
		if err := saveDangling(ctx, idx.s, cp); err != nil {
			return fmt.Errorf("save dangling: %w", err)
		}
		idx.dangling = cp.Head
	}

	if idx.head.Number >= idx.dangling.Number+idx.finalityDepth {
		if err := promoteDangling(ctx, idx.s); err != nil {
			return fmt.Errorf("promote dangling: %w", err)
		}
		idx.dangling = BlockRef{}
	}

	return nil
}

func backfill(
	ctx context.Context,
	c Client,
	h Handler,
	s Store,
	f Filter,
	from, to, maxBlockRange uint64,
) error {
	for _, ch := range chunkBlockRange(from, to, maxBlockRange) {
		logs, err := cachedFilterLogs(ctx, c, s, newFilterQuery(f, ch.from, ch.to))
		if err != nil {
			return fmt.Errorf("get logs: %w", err)
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		if err := h.Process(ctx, logs); err != nil {
			return fmt.Errorf("process logs: %w", err)
		}
	}

	return nil
}
