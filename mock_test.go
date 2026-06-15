package ethindex

import (
	"context"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
)

type mockSubscription struct {
	errCh   chan error
	unsubCh chan struct{}
}

func newMockSubscription() *mockSubscription {
	return &mockSubscription{
		errCh:   make(chan error),
		unsubCh: make(chan struct{}),
	}
}

func (s *mockSubscription) Unsubscribe() {
	select {
	case <-s.unsubCh:
	default:
		close(s.unsubCh)
	}
}

func (s *mockSubscription) Err() <-chan error {
	return s.errCh
}

type mockClient struct {
	headerByNumberFunc   func(ctx context.Context, number *big.Int) (*types.Header, error)
	filterLogsFunc       func(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
	subscribeNewHeadFunc func(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error)
}

func (m *mockClient) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	if m.headerByNumberFunc != nil {
		return m.headerByNumberFunc(ctx, number)
	}
	return nil, nil
}

func (m *mockClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	if m.filterLogsFunc != nil {
		return m.filterLogsFunc(ctx, q)
	}
	return nil, nil
}

func (m *mockClient) SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error) {
	if m.subscribeNewHeadFunc != nil {
		return m.subscribeNewHeadFunc(ctx, ch)
	}
	return nil, nil
}

type mockHandler struct {
	mu          sync.Mutex
	filter      Filter
	processed   []types.Log
	state       []byte
	processErr  error
	snapshotErr error
	restoreErr  error
}

func (m *mockHandler) Snapshot(ctx context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state, m.snapshotErr
}

func (m *mockHandler) Restore(ctx context.Context, state []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = state
	return m.restoreErr
}

func (m *mockHandler) Filter() Filter {
	return m.filter
}

func (m *mockHandler) Process(ctx context.Context, log types.Log) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.processErr != nil {
		return m.processErr
	}
	m.processed = append(m.processed, log)
	return nil
}

type mockCache struct {
	mu    sync.Mutex
	store map[string]any
}

func newMockCache() *mockCache {
	return &mockCache{
		store: make(map[string]any),
	}
}

func (m *mockCache) Load(name string, out any) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	val, ok := m.store[name]
	if !ok {
		return false, nil
	}

	// Since we are mocking encoding/decoding, we just do a simple type assertion and copy if types match.
	// For testing indexer we usually use it for saving/loading slices of types.Log or checkpoint struct.
	// Since types are known, we can do simple assignments.
	switch o := out.(type) {
	case *[]types.Log:
		*o = val.([]types.Log)
	case *checkpoint:
		*o = val.(checkpoint)
	}

	return true, nil
}

func (m *mockCache) Save(name string, v any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[name] = v
	return nil
}

func (m *mockCache) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.store, name)
	return nil
}
