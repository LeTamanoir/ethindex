package ethindex

type blockRange struct {
	from uint64
	to   uint64
}

func chunkBlockRange(from, to, size uint64) []blockRange {
	var chunks []blockRange
	for start := from; start <= to; start += size {
		end := min(start+size-1, to)
		chunks = append(chunks, blockRange{start, end})
	}
	return chunks
}
