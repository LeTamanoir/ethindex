package ethindex

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestCheckpoint_MarshalUnmarshalRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		cp   checkpoint
	}{
		{
			name: "nil state",
			cp:   checkpoint{Head: BlockRef{Number: 100, Hash: common.HexToHash("0xabc")}, State: nil},
		},
		{
			name: "empty state",
			cp:   checkpoint{Head: BlockRef{Number: 100, Hash: common.HexToHash("0xabc")}, State: []byte{}},
		},
		{
			name: "non-empty state",
			cp:   checkpoint{Head: BlockRef{Number: 999, Hash: common.HexToHash("0xdead")}, State: []byte("handler state blob")},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := tc.cp.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary: %v", err)
			}

			var got checkpoint
			if err := got.UnmarshalBinary(b); err != nil {
				t.Fatalf("UnmarshalBinary: %v", err)
			}

			if got.Head.Number != tc.cp.Head.Number {
				t.Errorf("Number = %d, want %d", got.Head.Number, tc.cp.Head.Number)
			}
			if got.Head.Hash != tc.cp.Head.Hash {
				t.Errorf("Hash = %x, want %x", got.Head.Hash, tc.cp.Head.Hash)
			}
			if !bytes.Equal(got.State, tc.cp.State) {
				t.Errorf("State = %q, want %q", got.State, tc.cp.State)
			}
		})
	}
}

func TestCheckpoint_UnmarshalBinaryTooShort(t *testing.T) {
	// Minimum valid size is 8 (number) + common.HashLength (32) = 40.
	short := make([]byte, 8+common.HashLength-1)

	var cp checkpoint
	err := cp.UnmarshalBinary(short)
	if !errors.Is(err, errInvalidCheckpoint) {
		t.Errorf("UnmarshalBinary(short) err = %v, want errInvalidCheckpoint", err)
	}
}

func TestCheckpoint_UnmarshalBinaryAcceptsNoState(t *testing.T) {
	// Exactly 8 + HashLength bytes: header only, no state. Must succeed.
	cp := checkpoint{Head: BlockRef{Number: 7, Hash: common.HexToHash("0xfeed")}}
	b, err := cp.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	var got checkpoint
	if err := got.UnmarshalBinary(b); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	if got.Head.Number != 7 {
		t.Errorf("Number = %d, want 7", got.Head.Number)
	}
	if len(got.State) != 0 {
		t.Errorf("State = %v, want empty", got.State)
	}
}
