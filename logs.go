package ethindex

import (
	"encoding"
	"encoding/binary"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type Logs []types.Log

var _ encoding.BinaryMarshaler = (*Logs)(nil)
var _ encoding.BinaryUnmarshaler = (*Logs)(nil)

var errInvalidLogs = errors.New("invalid logs")

func (ls Logs) MarshalBinary() ([]byte, error) {
	size := 0
	for _, l := range ls {
		size += common.AddressLength + 8 + len(l.Topics)*common.HashLength + 8 + len(l.Data) + 8 + common.HashLength + 8 + common.HashLength + 8 + 8
	}

	b := make([]byte, 0, 8+size)

	b = binary.LittleEndian.AppendUint64(b, uint64(len(ls)))
	for _, l := range ls {
		b = append(b, l.Address[:]...)
		b = binary.LittleEndian.AppendUint64(b, uint64(len(l.Topics)))
		for i := range l.Topics {
			b = append(b, l.Topics[i][:]...)
		}
		b = binary.LittleEndian.AppendUint64(b, uint64(len(l.Data)))
		b = append(b, l.Data...)
		b = binary.LittleEndian.AppendUint64(b, uint64(l.BlockNumber))
		b = append(b, l.TxHash[:]...)
		b = binary.LittleEndian.AppendUint64(b, uint64(l.TxIndex))
		b = append(b, l.BlockHash[:]...)
		b = binary.LittleEndian.AppendUint64(b, uint64(l.BlockTimestamp))
		b = binary.LittleEndian.AppendUint64(b, uint64(l.Index))
	}

	return b, nil
}

func (ls *Logs) UnmarshalBinary(b []byte) error {
	if len(b) < 8 {
		return errInvalidLogs
	}
	logsLen := int(binary.LittleEndian.Uint64(b[:8]))
	b = b[8:]

	*ls = make([]types.Log, logsLen)

	for i := range logsLen {
		l := &(*ls)[i]
		if len(b) < common.AddressLength {
			return errInvalidLogs
		}
		l.Address.SetBytes(b[:common.AddressLength])
		b = b[common.AddressLength:]
		if len(b) < 8 {
			return errInvalidLogs
		}
		topicsLen := int(binary.LittleEndian.Uint64(b[:8]))
		b = b[8:]
		if len(b) < topicsLen*common.HashLength {
			return errInvalidLogs
		}
		l.Topics = make([]common.Hash, topicsLen)
		for i := range l.Topics {
			l.Topics[i].SetBytes(b[:common.HashLength])
			b = b[common.HashLength:]
		}
		if len(b) < 8 {
			return errInvalidLogs
		}
		dataLen := int(binary.LittleEndian.Uint64(b[:8]))
		b = b[8:]
		if len(b) < dataLen {
			return errInvalidLogs
		}
		l.Data = append(l.Data[:0], b[:dataLen]...)
		b = b[dataLen:]
		if len(b) < 8 {
			return errInvalidLogs
		}
		l.BlockNumber = binary.LittleEndian.Uint64(b[:8])
		b = b[8:]
		if len(b) < common.HashLength {
			return errInvalidLogs
		}
		l.TxHash.SetBytes(b[:common.HashLength])
		b = b[common.HashLength:]
		if len(b) < 8 {
			return errInvalidLogs
		}
		l.TxIndex = uint(binary.LittleEndian.Uint64(b[:8]))
		b = b[8:]
		if len(b) < common.HashLength {
			return errInvalidLogs
		}
		l.BlockHash.SetBytes(b[:common.HashLength])
		b = b[common.HashLength:]
		if len(b) < 8 {
			return errInvalidLogs
		}
		l.BlockTimestamp = uint64(binary.LittleEndian.Uint64(b[:8]))
		b = b[8:]
		if len(b) < 8 {
			return errInvalidLogs
		}
		l.Index = uint(binary.LittleEndian.Uint64(b[:8]))
		b = b[8:]
	}

	if len(b) != 0 {
		return errInvalidLogs
	}

	return nil
}
