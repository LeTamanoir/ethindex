package ethindex

import (
	"math/big"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

func TestFilter_RangeQuery(t *testing.T) {
	addr := common.HexToAddress("0x1234")
	topic := common.HexToHash("0xabcd")

	f := Filter{FromBlock: 42, Addresses: []common.Address{addr}, Topics: [][]common.Hash{{topic}}}

	q := f.rangeQuery(100, 200)

	if q.FromBlock.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("FromBlock = %s, want 100", q.FromBlock)
	}
	if q.ToBlock.Cmp(big.NewInt(200)) != 0 {
		t.Errorf("ToBlock = %s, want 200", q.ToBlock)
	}
	if q.BlockHash != nil {
		t.Errorf("BlockHash = %v, want nil", q.BlockHash)
	}
	if !reflect.DeepEqual(q.Addresses, f.Addresses) {
		t.Errorf("Addresses = %v, want %v", q.Addresses, f.Addresses)
	}
	if !reflect.DeepEqual(q.Topics, f.Topics) {
		t.Errorf("Topics = %v, want %v", q.Topics, f.Topics)
	}
}

func TestFilter_BlockQuery(t *testing.T) {
	addr := common.HexToAddress("0x1234")
	topic := common.HexToHash("0xabcd")
	hash := common.HexToHash("0xfeed")

	f := Filter{FromBlock: 42, Addresses: []common.Address{addr}, Topics: [][]common.Hash{{topic}}}

	q := f.blockQuery(hash)

	if q.FromBlock != nil {
		t.Errorf("FromBlock = %v, want nil", q.FromBlock)
	}
	if q.ToBlock != nil {
		t.Errorf("ToBlock = %v, want nil", q.ToBlock)
	}
	if q.BlockHash == nil || *q.BlockHash != hash {
		t.Errorf("BlockHash = %v, want %v", q.BlockHash, hash)
	}
	if !reflect.DeepEqual(q.Addresses, f.Addresses) {
		t.Errorf("Addresses = %v, want %v", q.Addresses, f.Addresses)
	}
	if !reflect.DeepEqual(q.Topics, f.Topics) {
		t.Errorf("Topics = %v, want %v", q.Topics, f.Topics)
	}

	// Ensure FromBlock is ignored for block-hash-anchored queries.
	_ = ethereum.FilterQuery(q)
}
