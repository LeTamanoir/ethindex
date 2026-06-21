package ethindex

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type Filter struct {
	FromBlock uint64
	Addresses []common.Address
	Topics    [][]common.Hash
}

type Handler interface {
	Snapshot(context.Context) ([]byte, error)
	Restore(context.Context, []byte) error
	Filter() Filter
	Process(context.Context, []types.Log) error
}

type Client interface {
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
}

type Config struct {
	MaxBlockRange uint64
	FinalityDepth uint64
}

type Store interface {
	Load(key string) ([]byte, error)
	Save(key string, data []byte) error
	Delete(key string) error
}
