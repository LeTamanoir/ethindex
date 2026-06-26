package ethindex

import (
	"encoding"
	"encoding/binary"
	"errors"

	"github.com/ethereum/go-ethereum/common"
)

// Checkpoint stores handler state at a specific chain head.
type Checkpoint struct {
	Head  BlockRef
	State []byte
}

var _ encoding.BinaryMarshaler = (*Checkpoint)(nil)
var _ encoding.BinaryUnmarshaler = (*Checkpoint)(nil)

var errInvalidCheckpoint = errors.New("invalid checkpoint encoding")

const checkpointHeaderSize = 8 + common.HashLength

func (c Checkpoint) MarshalBinary() ([]byte, error) {
	b := make([]byte, 0, checkpointHeaderSize+len(c.State))

	b = binary.LittleEndian.AppendUint64(b, c.Head.Number)
	b = append(b, c.Head.Hash[:]...)
	b = append(b, c.State...)

	return b, nil
}

func (c *Checkpoint) UnmarshalBinary(b []byte) error {
	if len(b) < checkpointHeaderSize {
		return errInvalidCheckpoint
	}

	c.Head.Number = binary.LittleEndian.Uint64(b)
	c.Head.Hash = common.BytesToHash(b[8 : 8+common.HashLength])
	c.State = append([]byte(nil), b[8+common.HashLength:]...)

	return nil
}
