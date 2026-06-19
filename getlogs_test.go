package ethindex

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/joho/godotenv"
)

func setupRPC(b *testing.B) *rpc.Client {
	b.Helper()

	godotenv.Load()

	httpURL := os.Getenv("ETH_HTTP_URL")
	if httpURL == "" {
		b.Fatal("missing ETH_HTTP_URL")
	}

	var options []rpc.ClientOption

	if v := os.Getenv("ETH_JWT_SECRET"); v != "" {
		var secret [32]byte
		if _, err := hex.Decode(secret[:], []byte(v)); err != nil {
			b.Fatalf("failed to decode secret: %s", err)
		}
		options = append(options, rpc.WithHTTPAuth(node.NewJWTAuth(secret)))
	}

	rpc, err := rpc.DialOptions(b.Context(), httpURL, options...)
	if err != nil {
		b.Fatal(err)
	}

	return rpc
}

func BenchmarkFetLogs(b *testing.B) {
	rpc := setupRPC(b)

	startBlock := 24419568
	endBlock := startBlock + 100_000
	// endBlock := 24519567

	b.Run("easyjson", func(b *testing.B) {
		for b.Loop() {
			var logs logs
			err := rpc.CallContext(b.Context(), &logs, "eth_getLogs", map[string]any{
				"fromBlock": fmt.Sprintf("0x%x", startBlock),
				"toBlock":   fmt.Sprintf("0x%x", endBlock),
				"address":   []string{"0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"},
				"topics":    [][]string{{"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef", "0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925"}},
			})
			if err != nil {
				b.Fatal(err)
			}
			b.Logf("logs count: %d", len(logs))
		}
	})

	b.Run("types.Log", func(b *testing.B) {
		for b.Loop() {
			var logs []types.Log
			err := rpc.CallContext(b.Context(), &logs, "eth_getLogs", map[string]any{
				"fromBlock": fmt.Sprintf("0x%x", startBlock),
				"toBlock":   fmt.Sprintf("0x%x", endBlock),
				"address":   []string{"0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"},
				"topics":    [][]string{{"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef", "0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925"}},
			})
			if err != nil {
				b.Fatal(err)
			}
			b.Logf("logs count: %d", len(logs))
		}
	})

	b.Run("no decode", func(b *testing.B) {
		for b.Loop() {
			var logs json.RawMessage
			err := rpc.CallContext(b.Context(), &logs, "eth_getLogs", map[string]any{
				"fromBlock": fmt.Sprintf("0x%x", startBlock),
				"toBlock":   fmt.Sprintf("0x%x", endBlock),
				"address":   []string{"0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"},
				"topics":    [][]string{{"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef", "0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925"}},
			})
			if err != nil {
				b.Fatal(err)
			}
			b.Logf("logs count: %d", len(logs))
		}
	})

}
