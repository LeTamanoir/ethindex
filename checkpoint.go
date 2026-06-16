package ethindex

import (
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

type blockHeader struct {
	Number uint64
	Hash   common.Hash
}

type checkpoint struct {
	Header blockHeader
	State  []byte
}

func (c checkpoint) MarshalBinary() ([]byte, error) {
	b := make([]byte, 0, 0+
		/* Header.Number */ 8+
		/* Header.Hash */ common.HashLength+
		/* State */ 8+len(c.State))

	b = binary.LittleEndian.AppendUint64(b, c.Header.Number)

	b = append(b, c.Header.Hash.Bytes()...)

	b = binary.LittleEndian.AppendUint64(b, uint64(len(c.State)))
	b = append(b, c.State...)

	return b, nil
}

func (c *checkpoint) UnmarshalBinary(b []byte) error {
	need := func(n int) error {
		if len(b) < n {
			return fmt.Errorf("buffer too short: need %d, have %d", n, len(b))
		}
		return nil
	}

	if err := need(8); err != nil {
		return err
	}
	c.Header.Number = binary.LittleEndian.Uint64(b[:8])
	b = b[8:]

	if err := need(common.HashLength); err != nil {
		return err
	}
	c.Header.Hash = common.BytesToHash(b[:common.HashLength])
	b = b[common.HashLength:]

	if err := need(8); err != nil {
		return err
	}
	stateLen := binary.LittleEndian.Uint64(b[:8])
	b = b[8:]

	if err := need(int(stateLen)); err != nil {
		return err
	}
	c.State = make([]byte, stateLen)
	copy(c.State, b[:stateLen])

	return nil
}
