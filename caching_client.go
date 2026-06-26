package ethindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"sync/atomic"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
)

// CachingClient caches finalized log range queries in a BlobStore.
type CachingClient struct {
	c ChainReader
	s BlobStore

	finalized atomic.Uint64
}

var _ ChainReader = (*CachingClient)(nil)

// NewCachingClient returns a ChainReader that caches finalized log range queries.
func NewCachingClient(c ChainReader, s BlobStore) *CachingClient {
	return &CachingClient{c: c, s: s}
}

// HeaderByNumber returns a block header from the underlying client.
func (cc *CachingClient) HeaderByNumber(ctx context.Context, num *big.Int) (*types.Header, error) {
	b, err := cc.c.HeaderByNumber(ctx, num)

	if err == nil {
		if rpc.FinalizedBlockNumber == rpc.BlockNumber(num.Int64()) {
			cc.finalized.Store(b.Number.Uint64())
		}
	}

	return b, err
}

// cacheable reports whether q can be safely cached.
func (cc *CachingClient) cacheable(ctx context.Context, q ethereum.FilterQuery) bool {
	if q.BlockHash != nil || q.ToBlock == nil {
		return false
	}

	if cc.finalized.Load() == 0 {
		h, err := cc.HeaderByNumber(ctx, big.NewInt(int64(rpc.FinalizedBlockNumber)))
		if err != nil {
			return false
		}

		cc.finalized.Store(h.Number.Uint64())
	}

	return q.ToBlock.Uint64() <= cc.finalized.Load()
}

// FilterLogs returns logs from cache when q targets a finalized range.
func (cc *CachingClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	if !cc.cacheable(ctx, q) {
		return cc.c.FilterLogs(ctx, q)
	}

	key := logsCacheKey(q)

	{
		bin, err := cc.s.Read(key)
		if err != nil {
			return nil, fmt.Errorf("store read: %w", err)
		}
		if bin != nil {
			var logs Logs
			if err := logs.UnmarshalBinary(bin); err != nil {
				return nil, fmt.Errorf("unmarshal: %w", err)
			}
			return logs, nil
		}
	}

	logs, err := cc.c.FilterLogs(ctx, q)
	if err != nil {
		return nil, err
	}

	bin, err := Logs(logs).MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	if err := cc.s.Write(key, bin); err != nil {
		return nil, fmt.Errorf("store write: %w", err)
	}

	return logs, nil
}

// logsCacheKey returns the cache key for q.
func logsCacheKey(q ethereum.FilterQuery) string {
	var b []byte

	for _, a := range q.Addresses {
		b = append(b, a[:]...)
	}
	for _, tt := range q.Topics {
		b = append(b, '-')
		for _, t := range tt {
			b = append(b, t[:]...)
		}
	}

	hash := sha256.Sum256(b)

	return fmt.Sprintf("logs-%d-%d-%s", q.FromBlock, q.ToBlock, hex.EncodeToString(hash[:]))
}
