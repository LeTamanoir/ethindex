package ethindex

import (
	"compress/gzip"
	"context"
	"encoding/gob"
	"io"
	"log/slog"
	"math/big"
	"os"
	"sort"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type nopCache struct{}

func (nopCache) Load(name string, out any) (bool, error) { return false, nil }
func (nopCache) Save(name string, v any) error           { return nil }
func (nopCache) Delete(name string) error                { return nil }

type wethBenchHandler struct {
	filter          Filter
	balances        map[common.Address]*big.Int
	transferEventID common.Hash
}

func (h *wethBenchHandler) Snapshot(context.Context) ([]byte, error) { return nil, nil }
func (h *wethBenchHandler) Restore(context.Context, []byte) error    { return nil }
func (h *wethBenchHandler) Filter() Filter                           { return h.filter }
func (h *wethBenchHandler) Process(ctx context.Context, log types.Log) error {
	if len(log.Topics) >= 3 && log.Topics[0] == h.transferEventID {
		from := common.BytesToAddress(log.Topics[1].Bytes())
		to := common.BytesToAddress(log.Topics[2].Bytes())
		value := new(big.Int).SetBytes(log.Data)

		if from != (common.Address{}) {
			if bal, ok := h.balances[from]; ok {
				bal.Sub(bal, value)
			} else {
				h.balances[from] = new(big.Int).Neg(value)
			}
		}
		if to != (common.Address{}) {
			if bal, ok := h.balances[to]; ok {
				bal.Add(bal, value)
			} else {
				h.balances[to] = new(big.Int).Set(value)
			}
		}
	}
	return nil
}

func BenchmarkIndexer_Backfill(b *testing.B) {
	// Load the fixture
	f, err := os.Open("testdata/weth_logs.gob.gz")
	if err != nil {
		b.Fatalf("failed to open fixture (run download_fixture.go?): %v", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		b.Fatalf("failed to read gzip: %v", err)
	}
	defer gz.Close()

	var fixtureLogs []types.Log
	if err := gob.NewDecoder(gz).Decode(&fixtureLogs); err != nil {
		b.Fatalf("failed to decode gob: %v", err)
	}

	if len(fixtureLogs) == 0 {
		b.Fatalf("fixture has no logs")
	}

	fromBlock := fixtureLogs[0].BlockNumber
	finalizedBlock := fixtureLogs[len(fixtureLogs)-1].BlockNumber

	client := &mockClient{
		headerByNumberFunc: func(ctx context.Context, number *big.Int) (*types.Header, error) {
			return &types.Header{Number: big.NewInt(int64(finalizedBlock))}, nil
		},
		filterLogsFunc: func(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
			from := q.FromBlock.Uint64()
			to := q.ToBlock.Uint64()

			startIndex := sort.Search(len(fixtureLogs), func(i int) bool {
				return fixtureLogs[i].BlockNumber >= from
			})

			var result []types.Log
			for i := startIndex; i < len(fixtureLogs) && fixtureLogs[i].BlockNumber <= to; i++ {
				result = append(result, fixtureLogs[i])
			}
			return result, nil
		},
		subscribeNewHeadFunc: func(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error) {
			// Stop immediately after backfill when it attempts to go live
			sub := newMockSubscription()
			go func() { sub.errCh <- context.Canceled }()
			return sub, nil
		},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	transferSig := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

	for b.Loop() {
		b.StopTimer()

		handler := &wethBenchHandler{
			filter:          Filter{FromBlock: fromBlock},
			balances:        make(map[common.Address]*big.Int),
			transferEventID: transferSig,
		}

		indexer := New().
			WithHandler(handler).
			WithClients(client, client).
			WithCache(nopCache{}).
			WithMaxBlockRange(100). // Smaller chunk size since real logs are heavy
			WithLogger(logger).
			Build()

		b.StartTimer()

		err := indexer.Run(context.Background())
		if err != nil && err != context.Canceled {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}
