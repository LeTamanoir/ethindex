package main

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"os/signal"
	"syscall"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/joho/godotenv"

	"github.com/letamanoir/ethindex"
	"github.com/letamanoir/ethindex/examples/contracts"
)

var (
	erc20       = contracts.NewERC20()
	erc20ABI, _ = contracts.ERC20MetaData.ParseABI()

	transferEventID = erc20ABI.Events["Transfer"].ID
	approvalEventID = erc20ABI.Events["Approval"].ID
)

type WETH struct {
	Balances   map[common.Address]big.Int
	Allowances map[common.Address]map[common.Address]big.Int
}

func NewWETH() *WETH {
	return &WETH{
		Balances:   make(map[common.Address]big.Int),
		Allowances: make(map[common.Address]map[common.Address]big.Int),
	}
}

func (e *WETH) Filter() ethindex.Filter {
	return ethindex.Filter{
		FromBlock: 4719568,
		Addresses: []common.Address{common.HexToAddress("0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2")},
		Topics:    [][]common.Hash{{transferEventID, approvalEventID}},
	}
}

func (e *WETH) Restore(_ context.Context, data []byte) error {
	return gob.NewDecoder(bytes.NewReader(data)).Decode(e)
}

func (e *WETH) Snapshot(_ context.Context) ([]byte, error) {
	var b bytes.Buffer
	if err := gob.NewEncoder(&b).Encode(e); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (e *WETH) Process(_ context.Context, log types.Log) error {
	switch log.Topics[0] {
	case transferEventID:
		t, err := erc20.UnpackTransferEvent(&log)
		if err != nil {
			return err
		}

		if t.From != (common.Address{}) {
			fromBalance := e.Balances[t.From]
			e.Balances[t.From] = *new(big.Int).Sub(&fromBalance, t.Value)
		}
		if t.To != (common.Address{}) {
			toBalance := e.Balances[t.To]
			e.Balances[t.To] = *new(big.Int).Add(&toBalance, t.Value)
		}
	case approvalEventID:
		a, err := erc20.UnpackApprovalEvent(&log)
		if err != nil {
			return err
		}

		al, ok := e.Allowances[a.Owner]
		if !ok {
			al = make(map[common.Address]big.Int)
		}
		al[a.Spender] = *a.Value
	}
	return nil
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	_ = godotenv.Load()

	var options []rpc.ClientOption

	if v := os.Getenv("ETH_JWT_SECRET"); v != "" {
		var secret [32]byte
		if _, err := hex.Decode(secret[:], []byte(v)); err != nil {
			return fmt.Errorf("failed to decode secret: %w", err)
		}
		options = append(options, rpc.WithHTTPAuth(node.NewJWTAuth(secret)))
	}

	httpURL := os.Getenv("ETH_HTTP_URL")
	if httpURL == "" {
		return fmt.Errorf("missing ETH_HTTP_URL")
	}

	wsURL := os.Getenv("ETH_WS_URL")
	if wsURL == "" {
		return fmt.Errorf("missing ETH_WS_URL")
	}

	httpRPC, err := rpc.DialOptions(ctx, httpURL, options...)
	if err != nil {
		return err
	}

	wsRPC, err := rpc.DialOptions(ctx, wsURL, options...)
	if err != nil {
		return err
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))

	httpC := ethclient.NewClient(httpRPC)
	wsC := ethclient.NewClient(wsRPC)

	weth := NewWETH()

	cache := ethindex.NewFileCache("./indexer_data")

	idx := ethindex.New().
		WithHandler(weth).
		WithClients(httpC, wsC).
		WithCache(cache).
		Build()

	return idx.Run(ctx)
}

func main() {
	if err := run(); err != nil {
		slog.Error("Indexer error", "error", err)
		os.Exit(1)
	}
}
