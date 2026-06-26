package ethindex

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
)

// errSentinel is a predictable error used by the mock collaborators in
// error-path tests so the assertions can use errors.Is.
var errSentinel = errors.New("sentinel error")

// chainClient builds a mockClient serving a deterministic, consecutive chain
// starting at genesis. Headers for the given numbers are pre-built so that
// each header's ParentHash equals the previous header's hash.
func chainClient(finalizedNum uint64, extra ...uint64) (*mockClient, map[uint64]*types.Header) {
	headers := buildChain(finalizedNum, extra...)

	headerByNumber := func(_ context.Context, number *big.Int) (*types.Header, error) {
		if number.Int64() == int64(rpc.FinalizedBlockNumber) {
			return headers[finalizedNum], nil
		}
		return headers[number.Uint64()], nil
	}

	return &mockClient{
		headerByNumberFunc: headerByNumber,
		filterLogsFunc:     func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) { return nil, nil },
	}, headers
}

// buildChain builds a consecutive chain containing finalizedNum and every
// number in extra, returning a map keyed by block number.
func buildChain(finalizedNum uint64, extra ...uint64) map[uint64]*types.Header {
	nums := make(map[uint64]struct{})
	nums[finalizedNum] = struct{}{}
	for _, n := range extra {
		nums[n] = struct{}{}
	}

	max := finalizedNum
	for n := range nums {
		if n > max {
			max = n
		}
	}

	headers := make(map[uint64]*types.Header, len(nums))
	var prev *types.Header
	for n := uint64(0); n <= max; n++ {
		h := &types.Header{Number: big.NewInt(int64(n))}
		if prev != nil {
			h.ParentHash = prev.Hash()
		}
		if _, ok := nums[n]; ok {
			headers[n] = h
		}
		prev = h
	}
	return headers
}

// --- Edge paths ---

func TestIndexer_IgnoreOldHead(t *testing.T) {
	ctx := t.Context()

	const finalizedBlockNum = uint64(10)
	client, headers := chainClient(finalizedBlockNum)

	idx := NewIndexer(client, &mockHandler{}, Filter{FromBlock: finalizedBlockNum},
		newMockStore(), testLogger(), Config{})
	if err := idx.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Push a head whose number is equal to the current head: must be a no-op.
	if err := idx.Process(ctx, headers[finalizedBlockNum]); err != nil {
		t.Fatalf("Process equal head: %v", err)
	}

	// Push an older head: also a no-op.
	old := &types.Header{Number: big.NewInt(5)}
	if err := idx.Process(ctx, old); err != nil {
		t.Fatalf("Process old head: %v", err)
	}

	if idx.head.Number != finalizedBlockNum {
		t.Errorf("head = %d, want %d (unchanged)", idx.head.Number, finalizedBlockNum)
	}
}

func TestIndexer_FillGap(t *testing.T) {
	ctx := t.Context()

	const finalizedBlockNum = uint64(10)
	client, headers := chainClient(finalizedBlockNum, 11, 12, 13)

	handler := &mockHandler{}
	idx := NewIndexer(client, handler, Filter{FromBlock: finalizedBlockNum},
		newMockStore(), testLogger(), Config{})
	if err := idx.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Jump straight to 13; the indexer must fill 11, 12, 13 from the client.
	h13 := headers[13]
	if err := idx.Process(ctx, h13); err != nil {
		t.Fatalf("Process h13: %v", err)
	}

	if idx.head.Number != 13 {
		t.Errorf("head = %d, want 13", idx.head.Number)
	}
}

func TestIndexer_NoBackfillRequired(t *testing.T) {
	ctx := t.Context()

	const finalizedBlockNum = uint64(100)
	client, _ := chainClient(finalizedBlockNum)

	handler := &mockHandler{}
	idx := NewIndexer(client, handler, Filter{FromBlock: 200},
		newMockStore(), testLogger(), Config{})
	if err := idx.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if idx.head != (BlockRef{}) {
		t.Errorf("head = %+v, want zero (no backfill, no checkpoint)", idx.head)
	}
	if client.filterLogsCallCount() != 0 {
		t.Errorf("FilterLogs calls = %d, want 0", client.filterLogsCallCount())
	}
}

func TestIndexer_ReorgNoCheckpoint(t *testing.T) {
	ctx := t.Context()

	const finalizedBlockNum = uint64(100)
	client, _ := chainClient(finalizedBlockNum)

	// FromBlock > finalized so Sync saves no checkpoint (no backfill branch).
	idx := NewIndexer(client, &mockHandler{}, Filter{FromBlock: 200},
		newMockStore(), testLogger(), Config{})
	if err := idx.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Push a head at block 1 with a non-zero parent; head.Hash is the zero
	// hash, so the parent mismatch triggers a reorg. With no finalized
	// checkpoint in the store, handleReorg must surface a clear error.
	h1 := &types.Header{
		Number:     big.NewInt(1),
		ParentHash: common.HexToHash("0xdeadbeef"),
	}
	err := idx.Process(ctx, h1)
	if err == nil {
		t.Fatal("expected reorg-without-checkpoint error, got nil")
	}
	if !contains(err.Error(), "no finalized checkpoint found") {
		t.Errorf("expected 'no finalized checkpoint found', got %q", err.Error())
	}
}

// contains is a tiny helper to avoid importing strings just for one check.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// --- Error propagation paths ---

func TestIndexer_RestoreHandlerError(t *testing.T) {
	ctx := t.Context()

	const finalizedBlockNum = uint64(50)
	cp := checkpoint{
		Head:  BlockRef{Number: 50, Hash: common.HexToHash("0x123")},
		State: []byte("saved"),
	}
	cpb, err := cp.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	store := newMockStore()
	if err := store.Write(t.Context(), string(finalized), cpb); err != nil {
		t.Fatal(err)
	}

	restoreErr := errors.New("restore failed")
	handler := &mockHandler{restoreErr: restoreErr}

	client, _ := chainClient(finalizedBlockNum)
	idx := NewIndexer(client, handler, Filter{FromBlock: 10}, store, testLogger(), Config{})

	err = idx.Sync(ctx)
	if err == nil {
		t.Fatal("expected Sync to fail when Restore errors, got nil")
	}
	if !errors.Is(err, restoreErr) {
		t.Errorf("expected error to wrap restoreErr, got %q", err)
	}
	if !contains(err.Error(), "handler:") {
		t.Errorf("expected 'handler:' prefix, got %q", err.Error())
	}
}

func TestIndexer_SaveDanglingSnapshotError(t *testing.T) {
	ctx := t.Context()

	const finalizedBlockNum = uint64(10)
	client, headers := chainClient(finalizedBlockNum, 11)

	snapshotErr := errors.New("snapshot failed")
	handler := &mockHandler{}

	idx := NewIndexer(client, handler, Filter{FromBlock: finalizedBlockNum},
		newMockStore(), testLogger(), Config{})
	if err := idx.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Snapshot must succeed during Sync (to persist the finalized checkpoint)
	// but fail when the first live head triggers saveDanglingAsync.
	handler.snapshotErr = snapshotErr

	err := idx.Process(ctx, headers[11])
	if err == nil {
		t.Fatal("expected snapshot error, got nil")
	}
	if !errors.Is(err, snapshotErr) {
		t.Errorf("expected error to wrap snapshotErr, got %q", err)
	}
	if !contains(err.Error(), "snapshot:") {
		t.Errorf("expected 'snapshot:' prefix, got %q", err.Error())
	}
}

func TestIndexer_AsyncSaveErrorSurfaced(t *testing.T) {
	ctx := t.Context()

	const finalizedBlockNum = uint64(10)
	client, headers := chainClient(finalizedBlockNum, 11, 12, 13)

	store := newMockStore()
	handler := &mockHandler{}

	idx := NewIndexer(client, handler, Filter{FromBlock: finalizedBlockNum},
		store, testLogger(), Config{FinalityDepth: 2})
	if err := idx.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Now make store.Write fail so the async dangling save errors out.
	store.writeErr = errSentinel

	// h11: saveDanglingAsync launches a goroutine that fails the Write, but
	// Process returns nil because the save is async.
	if err := idx.Process(ctx, headers[11]); err != nil {
		t.Fatalf("Process h11: %v (save is async, must not surface yet)", err)
	}

	// h12: not enough depth to promote, still no error surfaced.
	if err := idx.Process(ctx, headers[12]); err != nil {
		t.Fatalf("Process h12: %v", err)
	}

	// h13: head(13) >= dangling(11) + finalityDepth(2) -> promote -> waitPending
	// surfaces the async save error.
	err := idx.Process(ctx, headers[13])
	if err == nil {
		t.Fatal("expected async save error to be surfaced on promote, got nil")
	}
	if !errors.Is(err, errSentinel) {
		t.Errorf("expected error to wrap errSentinel, got %q", err)
	}
}

func TestIndexer_PromoteDanglingMoveError(t *testing.T) {
	ctx := t.Context()

	const finalizedBlockNum = uint64(10)
	client, headers := chainClient(finalizedBlockNum, 11, 12, 13)

	store := newMockStore()
	store.moveErr = errors.New("move failed")
	handler := &mockHandler{}

	idx := NewIndexer(client, handler, Filter{FromBlock: finalizedBlockNum},
		store, testLogger(), Config{FinalityDepth: 2})
	if err := idx.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	for _, h := range []*types.Header{headers[11], headers[12]} {
		if err := idx.Process(ctx, h); err != nil {
			t.Fatalf("Process h%d: %v", h.Number, err)
		}
	}

	// h13 triggers promote; Move fails and is wrapped as "move:".
	err := idx.Process(ctx, headers[13])
	if err == nil {
		t.Fatal("expected move error on promote, got nil")
	}
	if !contains(err.Error(), "move:") {
		t.Errorf("expected 'move:' prefix, got %q", err.Error())
	}
}

func TestIndexer_BackfillProcessError(t *testing.T) {
	ctx := t.Context()

	const finalizedBlockNum = uint64(20)
	client, _ := chainClient(finalizedBlockNum)

	processErr := errors.New("process failed")
	handler := &mockHandler{processErr: processErr}

	idx := NewIndexer(client, handler, Filter{FromBlock: 10},
		newMockStore(), testLogger(), Config{})
	err := idx.Sync(ctx)
	if err == nil {
		t.Fatal("expected Sync to fail when Process errors, got nil")
	}
	if !errors.Is(err, processErr) {
		t.Errorf("expected error to wrap processErr, got %q", err)
	}
	if !contains(err.Error(), "process logs:") {
		t.Errorf("expected 'process logs:' prefix, got %q", err.Error())
	}
}

func TestIndexer_BackfillFilterLogsError(t *testing.T) {
	ctx := t.Context()

	const finalizedBlockNum = uint64(20)
	filterErr := errors.New("rpc down")

	client := &mockClient{
		headerByNumberFunc: func(_ context.Context, number *big.Int) (*types.Header, error) {
			if number.Int64() == int64(rpc.FinalizedBlockNumber) {
				return &types.Header{Number: big.NewInt(int64(finalizedBlockNum))}, nil
			}
			return nil, nil
		},
		filterLogsFunc: func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) {
			return nil, filterErr
		},
	}

	idx := NewIndexer(client, &mockHandler{}, Filter{FromBlock: 10},
		newMockStore(), testLogger(), Config{})
	err := idx.Sync(ctx)
	if err == nil {
		t.Fatal("expected Sync to fail when FilterLogs errors, got nil")
	}
	if !errors.Is(err, filterErr) {
		t.Errorf("expected error to wrap filterErr, got %q", err)
	}
	if !contains(err.Error(), "filter logs:") {
		t.Errorf("expected 'filter logs:' in chain, got %q", err.Error())
	}
}

func TestIndexer_HeadersRangeError(t *testing.T) {
	ctx := t.Context()

	const finalizedBlockNum = uint64(10)
	headerErr := errors.New("header rpc failed")

	client := &mockClient{
		headerByNumberFunc: func(_ context.Context, number *big.Int) (*types.Header, error) {
			if number.Int64() == int64(rpc.FinalizedBlockNumber) {
				return &types.Header{Number: big.NewInt(int64(finalizedBlockNum))}, nil
			}
			// Any non-finalized block number fails, so fillGap -> headersRange fails.
			return nil, headerErr
		},
		filterLogsFunc: func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) {
			return nil, nil
		},
	}

	idx := NewIndexer(client, &mockHandler{}, Filter{FromBlock: finalizedBlockNum},
		newMockStore(), testLogger(), Config{})
	if err := idx.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Push a head with a gap; fillGap will call headersRange which fails.
	h13 := &types.Header{Number: big.NewInt(13)}
	err := idx.Process(ctx, h13)
	if err == nil {
		t.Fatal("expected headers range error, got nil")
	}
	if !errors.Is(err, headerErr) {
		t.Errorf("expected error to wrap headerErr, got %q", err)
	}
	if !contains(err.Error(), "headers range:") {
		t.Errorf("expected 'headers range:' prefix, got %q", err.Error())
	}
}

// --- Cache / resume paths ---

func TestIndexer_LogsRangeCacheHit(t *testing.T) {
	ctx := t.Context()

	const finalizedBlockNum = uint64(20)
	client, _ := chainClient(finalizedBlockNum)

	// Pre-seed the store with cached logs for the entire backfill range.
	store := newMockStore()
	cached := []types.Log{
		{BlockNumber: 10, Data: []byte("a")},
		{BlockNumber: 20, Data: []byte("b")},
	}
	q := Filter{FromBlock: 10}.rangeQuery(10, finalizedBlockNum)
	if err := saveLogs(ctx, store, q, cached); err != nil {
		t.Fatal(err)
	}

	handler := &mockHandler{}
	idx := NewIndexer(client, handler, Filter{FromBlock: 10},
		store, testLogger(), Config{})
	if err := idx.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Backfill must have been served entirely from the cache.
	if client.filterLogsCallCount() != 0 {
		t.Errorf("FilterLogs calls = %d, want 0 (cache hit)", client.filterLogsCallCount())
	}
	if len(handler.processed) != len(cached) {
		t.Errorf("processed logs = %d, want %d", len(handler.processed), len(cached))
	}
}

func TestIndexer_RestartResumeBackfill(t *testing.T) {
	ctx := t.Context()

	// Two chunks: [10..15] and [16..20]. Pre-seed only the first chunk;
	// the indexer must resume it from cache and fetch only the second.
	const finalizedBlockNum = uint64(20)
	const chunkSize = uint64(6)

	client, _ := chainClient(finalizedBlockNum)

	store := newMockStore()
	firstChunk := []types.Log{{BlockNumber: 12, Data: []byte("cached")}}
	q1 := Filter{FromBlock: 10}.rangeQuery(10, 15)
	if err := saveLogs(ctx, store, q1, firstChunk); err != nil {
		t.Fatal(err)
	}

	handler := &mockHandler{}
	idx := NewIndexer(client, handler, Filter{FromBlock: 10},
		store, testLogger(), Config{MaxBlockRange: chunkSize})
	if err := idx.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Only the second chunk should have hit FilterLogs.
	if got := client.filterLogsCallCount(); got != 1 {
		t.Errorf("FilterLogs calls = %d, want 1 (first chunk cached)", got)
	}
	// chainClient returns nil logs for the second chunk, so only the cached
	// first chunk's logs are processed.
	if len(handler.processed) != len(firstChunk) {
		t.Errorf("processed logs = %d, want %d", len(handler.processed), len(firstChunk))
	}
}

// --- State-machine panic guards ---

func TestIndexer_SyncTwicePanics(t *testing.T) {
	ctx := t.Context()

	client, _ := chainClient(10)
	idx := NewIndexer(client, &mockHandler{}, Filter{FromBlock: 10},
		newMockStore(), testLogger(), Config{})

	if err := idx.Sync(ctx); err != nil {
		t.Fatalf("first Sync: %v", err)
	}

	if !panics(func() { _ = idx.Sync(ctx) }) {
		t.Fatal("expected second Sync to panic, but it did not")
	}
}

func TestIndexer_ProcessBeforeSyncPanics(t *testing.T) {
	ctx := t.Context()

	client, _ := chainClient(10)
	idx := NewIndexer(client, &mockHandler{}, Filter{FromBlock: 10},
		newMockStore(), testLogger(), Config{})

	h := &types.Header{Number: big.NewInt(11)}
	if !panics(func() { _ = idx.Process(ctx, h) }) {
		t.Fatal("expected Process before Sync to panic, but it did not")
	}
}

func TestIndexer_ConcurrentProcessPanics(t *testing.T) {
	ctx := t.Context()

	const finalizedBlockNum = uint64(10)
	client, headers := chainClient(finalizedBlockNum, 11, 12)

	idx := NewIndexer(client, &mockHandler{}, Filter{FromBlock: finalizedBlockNum},
		newMockStore(), testLogger(), Config{})
	if err := idx.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Override FilterLogs to block until released, so the first Process stays
	// in stateProcessing long enough for the second Process to race on the CAS.
	release := make(chan struct{})
	client.filterLogsFunc = func(_ context.Context, _ ethereum.FilterQuery) ([]types.Log, error) {
		<-release
		return nil, nil
	}

	var panicked atomic.Bool
	var wg sync.WaitGroup
	wg.Add(2)

	run := func(h *types.Header) {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				panicked.Store(true)
			}
		}()
		_ = idx.Process(ctx, h)
	}

	go run(headers[11])
	go run(headers[12])

	close(release)
	wg.Wait()

	if !panicked.Load() {
		t.Fatal("expected at least one concurrent Process to panic, but none did")
	}
}

func TestIndexer_HeadersRangeInvalidRangePanics(t *testing.T) {
	ctx := t.Context()

	client, _ := chainClient(10)
	idx := NewIndexer(client, &mockHandler{}, Filter{FromBlock: 10},
		newMockStore(), testLogger(), Config{})

	if !panics(func() { _, _ = idx.headersRange(ctx, 5, 1) }) {
		t.Fatal("expected headersRange(from > to) to panic, but it did not")
	}
}

func panics(fn func()) (yes bool) {
	defer func() {
		yes = recover() != nil
	}()
	fn()
	return
}

// --- Lifecycle ---

func TestIndexer_Lifecycle(t *testing.T) {
	ctx := t.Context()

	const finalizedBlockNum = uint64(10)
	// Serve a long enough chain for backfill + live + reorg recovery.
	client, headers := chainClient(finalizedBlockNum, 11, 12, 13, 14)

	store := newMockStore()
	handler := &mockHandler{}

	idx := NewIndexer(client, handler, Filter{FromBlock: finalizedBlockNum},
		store, testLogger(), Config{FinalityDepth: 2})

	// 1. Backfill to the finalized head.
	if err := idx.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if idx.head.Number != finalizedBlockNum {
		t.Fatalf("after backfill head = %d, want %d", idx.head.Number, finalizedBlockNum)
	}

	// 2. Live heads: h11 saves a dangling checkpoint, h12 advances, h13
	//    promotes the dangling (h11) to finalized.
	for _, h := range []*types.Header{headers[11], headers[12], headers[13]} {
		if err := idx.Process(ctx, h); err != nil {
			t.Fatalf("Process h%d: %v", h.Number, err)
		}
	}

	cp, ok, err := loadCheckpoint(ctx, store, finalized)
	if err != nil {
		t.Fatalf("load finalized after promote: %v", err)
	}
	if !ok || cp.Head.Number != 11 {
		t.Fatalf("finalized checkpoint = %+v ok=%v, want head 11", cp, ok)
	}

	// 3. Reorg: push a block at 14 with the wrong parent. The indexer must
	//    restore the finalized checkpoint (at 11) and reprocess 12, 13, 14
	//    from the canonical chain served by the client.
	handler.state = []byte("corrupted")

	h14bad := &types.Header{
		Number:     big.NewInt(14),
		ParentHash: common.HexToHash("0xdeadbeef"),
	}
	if err := idx.Process(ctx, h14bad); err != nil {
		t.Fatalf("Process reorg h14: %v", err)
	}

	// After reorg recovery the head must match the canonical 14, and the
	// handler state must have been restored from the finalized checkpoint.
	if idx.head.Number != 14 {
		t.Errorf("after reorg head = %d, want 14", idx.head.Number)
	}
	if string(handler.state) == "corrupted" {
		t.Error("handler state was not restored after reorg")
	}

	// 4. Continue live indexing past the reorg.
	h14canonical := headers[14]
	if err := idx.Process(ctx, &types.Header{
		Number:     big.NewInt(15),
		ParentHash: h14canonical.Hash(),
	}); err != nil {
		t.Fatalf("Process h15 after reorg: %v", err)
	}
	if idx.head.Number != 15 {
		t.Errorf("after continue head = %d, want 15", idx.head.Number)
	}
}
