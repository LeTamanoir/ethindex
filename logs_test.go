package ethindex

import (
	"bytes"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func makeLog(addr common.Address, topics []common.Hash, data []byte, blockNum uint64) types.Log {
	return types.Log{
		Address:        addr,
		Topics:         topics,
		Data:           data,
		BlockNumber:    blockNum,
		TxHash:         common.HexToHash("0xtx"),
		TxIndex:        3,
		BlockHash:      common.HexToHash("0xblock"),
		BlockTimestamp: 1_700_000_000,
		Index:          5,
	}
}

func TestLogs_MarshalUnmarshalRoundTrip(t *testing.T) {
	addr := common.HexToAddress("0x1234")
	topic1 := common.HexToHash("0xaaa")
	topic2 := common.HexToHash("0xbbb")

	tests := []struct {
		name string
		logs Logs
	}{
		{
			name: "empty slice",
			logs: Logs{},
		},
		{
			name: "single log no topics no data",
			logs: Logs{makeLog(addr, nil, nil, 100)},
		},
		{
			name: "single log with topics and data",
			logs: Logs{makeLog(addr, []common.Hash{topic1, topic2}, []byte("hello world"), 100)},
		},
		{
			name: "multiple logs",
			logs: Logs{
				makeLog(addr, []common.Hash{topic1}, []byte("first"), 100),
				makeLog(addr, []common.Hash{topic2}, []byte("second"), 101),
				makeLog(addr, nil, nil, 102),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := tc.logs.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary: %v", err)
			}

			var got Logs
			if err := got.UnmarshalBinary(b); err != nil {
				t.Fatalf("UnmarshalBinary: %v", err)
			}

			if len(got) != len(tc.logs) {
				t.Fatalf("len = %d, want %d", len(got), len(tc.logs))
			}
			for i := range got {
				if !bytes.Equal(got[i].Address[:], tc.logs[i].Address[:]) {
					t.Errorf("log[%d].Address = %x, want %x", i, got[i].Address, tc.logs[i].Address)
				}
				if len(got[i].Topics) != len(tc.logs[i].Topics) {
					t.Errorf("log[%d].Topics len = %d, want %d", i, len(got[i].Topics), len(tc.logs[i].Topics))
					continue
				}
				for j := range got[i].Topics {
					if got[i].Topics[j] != tc.logs[i].Topics[j] {
						t.Errorf("log[%d].Topics[%d] = %x, want %x", i, j, got[i].Topics[j], tc.logs[i].Topics[j])
					}
				}
				if !bytes.Equal(got[i].Data, tc.logs[i].Data) {
					t.Errorf("log[%d].Data = %q, want %q", i, got[i].Data, tc.logs[i].Data)
				}
				if got[i].BlockNumber != tc.logs[i].BlockNumber {
					t.Errorf("log[%d].BlockNumber = %d, want %d", i, got[i].BlockNumber, tc.logs[i].BlockNumber)
				}
				if got[i].TxIndex != tc.logs[i].TxIndex {
					t.Errorf("log[%d].TxIndex = %d, want %d", i, got[i].TxIndex, tc.logs[i].TxIndex)
				}
				if got[i].BlockTimestamp != tc.logs[i].BlockTimestamp {
					t.Errorf("log[%d].BlockTimestamp = %d, want %d", i, got[i].BlockTimestamp, tc.logs[i].BlockTimestamp)
				}
				if got[i].Index != tc.logs[i].Index {
					t.Errorf("log[%d].Index = %d, want %d", i, got[i].Index, tc.logs[i].Index)
				}
			}
		})
	}
}

// TestLogs_UnmarshalBinaryTruncation verifies that truncating the payload at
// every field boundary of the first log is rejected with errInvalidLogs.
func TestLogs_UnmarshalBinaryTruncation(t *testing.T) {
	addr := common.HexToAddress("0x1234")
	topic := common.HexToHash("0xaaa")
	log := makeLog(addr, []common.Hash{topic}, []byte("data"), 100)

	// Build a valid single-log body (without the leading count).
	body := appendLog(nil, log)

	// Field boundaries inside unmarshalLog, in order.
	boundaries := []int{
		common.AddressLength,                                                                     // after address
		common.AddressLength + 8,                                                                 // after topics length
		common.AddressLength + 8 + common.HashLength,                                             // after topics
		common.AddressLength + 8 + common.HashLength + 8,                                         // after data length
		common.AddressLength + 8 + common.HashLength + 8 + 4,                                     // mid-data
		common.AddressLength + 8 + common.HashLength + 8 + len(log.Data),                         // after data
		common.AddressLength + 8 + common.HashLength + 8 + len(log.Data) + 8,                     // after block number
		common.AddressLength + 8 + common.HashLength + 8 + len(log.Data) + 8 + common.HashLength, // after tx hash
	}

	for _, n := range boundaries {
		if n >= len(body) {
			continue
		}
		// Count = 1, then a truncated body.
		full := make([]byte, 8)
		full = append(full, body[:n]...)

		var got Logs
		if err := got.UnmarshalBinary(full); !errors.Is(err, errInvalidLogs) {
			t.Errorf("UnmarshalBinary trunc at %d: err = %v, want errInvalidLogs", n, err)
		}
	}
}

func TestLogs_UnmarshalBinaryMissingCount(t *testing.T) {
	var got Logs
	if err := got.UnmarshalBinary(nil); !errors.Is(err, errInvalidLogs) {
		t.Errorf("UnmarshalBinary(nil) err = %v, want errInvalidLogs", err)
	}
}

func TestLogs_UnmarshalBinaryTrailingBytes(t *testing.T) {
	full, err := Logs{makeLog(common.HexToAddress("0x1"), nil, nil, 1)}.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	extra := append([]byte{}, full...)
	extra = append(extra, 0xff)

	var got Logs
	if err := got.UnmarshalBinary(extra); !errors.Is(err, errInvalidLogs) {
		t.Errorf("UnmarshalBinary(extra byte) err = %v, want errInvalidLogs", err)
	}
}

func TestLogs_UnmarshalBinaryCountExceedsBody(t *testing.T) {
	log := makeLog(common.HexToAddress("0x1"), nil, nil, 1)
	body := appendLog(nil, log)

	// Count = 99 but only one log body present.
	full := make([]byte, 8)
	full[0] = 99
	full = append(full, body...)

	var got Logs
	if err := got.UnmarshalBinary(full); !errors.Is(err, errInvalidLogs) {
		t.Errorf("UnmarshalBinary(count=99) err = %v, want errInvalidLogs", err)
	}
}

func TestLogsKey(t *testing.T) {
	addr1 := common.HexToAddress("0x1111")
	addr2 := common.HexToAddress("0x2222")
	topic1 := common.HexToHash("0xaaaa")
	topic2 := common.HexToHash("0xbbbb")

	base := ethereum.FilterQuery{FromBlock: big.NewInt(0), ToBlock: big.NewInt(100)}

	queries := []struct {
		name string
		q    ethereum.FilterQuery
	}{
		{"base", base},
		{"with one address", withAddresses(base, addr1)},
		{"with two addresses", withAddresses(base, addr1, addr2)},
		{"with topics", withTopics(base, topic1)},
		{"with other topics", withTopics(base, topic2)},
		{"different from block", withFrom(base, 10)},
		{"different to block", withTo(base, 200)},
	}

	// Determinism: same query must always produce the same key.
	for _, tc := range queries {
		if logsKey(tc.q) != logsKey(tc.q) {
			t.Errorf("logsKey(%q) is not deterministic", tc.name)
		}
	}

	// Uniqueness: different queries must produce different keys.
	seen := make(map[string]string)
	for _, tc := range queries {
		k := logsKey(tc.q)
		if prev, dup := seen[k]; dup {
			t.Errorf("duplicate key for %q and %q", tc.name, prev)
		}
		seen[k] = tc.name
	}
}

func withAddresses(q ethereum.FilterQuery, addrs ...common.Address) ethereum.FilterQuery {
	q.Addresses = addrs
	return q
}
func withTopics(q ethereum.FilterQuery, topics ...common.Hash) ethereum.FilterQuery {
	q.Topics = [][]common.Hash{topics}
	return q
}
func withFrom(q ethereum.FilterQuery, n int64) ethereum.FilterQuery {
	q.FromBlock = big.NewInt(n)
	return q
}
func withTo(q ethereum.FilterQuery, n int64) ethereum.FilterQuery {
	q.ToBlock = big.NewInt(n)
	return q
}

func TestLoadSaveLogsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	addr := common.HexToAddress("0x1234")
	topic := common.HexToHash("0xaaa")
	logs := []types.Log{
		makeLog(addr, []common.Hash{topic}, []byte("hello"), 100),
		makeLog(addr, nil, []byte("world"), 101),
	}

	q := Filter{FromBlock: 100, Addresses: []common.Address{addr}, Topics: [][]common.Hash{{topic}}}.
		rangeQuery(100, 101)

	if err := saveLogs(t.Context(), store, q, logs); err != nil {
		t.Fatalf("saveLogs: %v", err)
	}

	loaded, err := loadLogs(t.Context(), store, q)
	if err != nil {
		t.Fatalf("loadLogs: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected cached logs, got nil")
	}
	if len(loaded) != len(logs) {
		t.Fatalf("len = %d, want %d", len(loaded), len(logs))
	}
	if loaded[0].BlockNumber != logs[0].BlockNumber || !bytes.Equal(loaded[0].Data, logs[0].Data) {
		t.Errorf("loaded[0] = %+v, want %+v", loaded[0], logs[0])
	}
}

func TestLoadLogsMissing(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	q := Filter{FromBlock: 0}.rangeQuery(0, 10)
	loaded, err := loadLogs(t.Context(), store, q)
	if err != nil {
		t.Fatalf("loadLogs: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil for missing key, got %d logs", len(loaded))
	}
}
