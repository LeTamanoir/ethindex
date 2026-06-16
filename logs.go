package ethindex

import (
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type Logs []types.Log

func (ls Logs) MarshalBinary() ([]byte, error) {
	b := make([]byte, 0, logsLen(ls))
	for _, l := range ls {
		b = appendLog(b, l)
	}
	return b, nil
}

func (ls *Logs) UnmarshalBinary(b []byte) (err error) {
	for len(b) > 0 {
		var l types.Log
		b, err = unmarshalLog(b, &l)
		if err != nil {
			return
		}
		*ls = append(*ls, l)
	}

	return nil
}

func logsLen(ls Logs) int {
	l := 0
	for i := range ls {
		l += (0 +
			/* Address */ common.AddressLength +
			/* Topics */ 8 + len(ls[i].Topics)*common.HashLength +
			/* Data */ 8 + len(ls[i].Data) +
			/* BlockNumber */ 8 +
			/* TxHash */ common.HashLength +
			/* TxIndex */ 8 +
			/* BlockHash */ common.HashLength +
			/* BlockTimestamp */ 8 +
			/* Index */ 8)
	}
	return l
}

func appendLog(b []byte, l types.Log) []byte {
	b = append(b, l.Address.Bytes()...)

	b = binary.LittleEndian.AppendUint64(b, uint64(len(l.Topics)))
	for i := range l.Topics {
		b = append(b, l.Topics[i].Bytes()...)
	}

	b = binary.LittleEndian.AppendUint64(b, uint64(len(l.Data)))
	b = append(b, l.Data...)

	b = binary.LittleEndian.AppendUint64(b, l.BlockNumber)

	b = append(b, l.TxHash.Bytes()...)

	b = binary.LittleEndian.AppendUint64(b, uint64(l.TxIndex))

	b = append(b, l.BlockHash.Bytes()...)

	b = binary.LittleEndian.AppendUint64(b, uint64(l.BlockTimestamp))
	b = binary.LittleEndian.AppendUint64(b, uint64(l.Index))

	return b
}

func unmarshalLog(b []byte, l *types.Log) (out []byte, err error) {
	need := func(n int) error {
		if len(b) < n {
			return fmt.Errorf("buffer too short: need %d, have %d", n, len(b))
		}
		return nil
	}

	if err = need(common.AddressLength); err != nil {
		return
	}
	l.Address = common.BytesToAddress(b[:common.AddressLength])
	b = b[common.AddressLength:]

	if err = need(8); err != nil {
		return
	}
	topicsLen := binary.LittleEndian.Uint64(b[:8])
	b = b[8:]
	if err = need(int(topicsLen) * common.HashLength); err != nil {
		return
	}
	l.Topics = make([]common.Hash, topicsLen)
	for i := range l.Topics {
		l.Topics[i] = common.BytesToHash(b[:common.HashLength])
		b = b[common.HashLength:]
	}

	if err = need(8); err != nil {
		return
	}
	dataLen := binary.LittleEndian.Uint64(b[:8])
	b = b[8:]
	if err = need(int(dataLen)); err != nil {
		return
	}
	l.Data = make([]byte, dataLen)
	copy(l.Data, b[:dataLen])
	b = b[dataLen:]

	if err = need(8); err != nil {
		return
	}
	l.BlockNumber = binary.LittleEndian.Uint64(b[:8])
	b = b[8:]

	if err = need(common.HashLength); err != nil {
		return
	}
	l.TxHash = common.BytesToHash(b[:common.HashLength])
	b = b[common.HashLength:]

	if err = need(8); err != nil {
		return
	}
	l.TxIndex = uint(binary.LittleEndian.Uint64(b[:8]))
	b = b[8:]

	if err = need(common.HashLength); err != nil {
		return
	}
	l.BlockHash = common.BytesToHash(b[:common.HashLength])
	b = b[common.HashLength:]

	if err = need(8); err != nil {
		return
	}
	l.BlockTimestamp = binary.LittleEndian.Uint64(b[:8])
	b = b[8:]

	if err = need(8); err != nil {
		return
	}
	l.Index = uint(binary.LittleEndian.Uint64(b[:8]))
	b = b[8:]

	return b, nil
}
