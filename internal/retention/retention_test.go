package retention

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/clock"
)

type fakeStore struct {
	mu      sync.Mutex
	cutoffs []time.Time
	batches []int
	err     error
}

func (f *fakeStore) DeleteBefore(_ context.Context, cutoff time.Time, batch int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cutoffs = append(f.cutoffs, cutoff)
	f.batches = append(f.batches, batch)
	return 42, f.err
}

func (f *fakeStore) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.cutoffs)
}

var now = time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)

func TestRunOnce(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	var logs bytes.Buffer
	r := New(store, clock.NewFake(now), slog.New(slog.NewTextHandler(&logs, nil)),
		Config{Keep: 30 * 24 * time.Hour, Interval: time.Hour, Batch: 1000})

	r.RunOnce(t.Context())

	require.Len(t, store.cutoffs, 1)
	assert.Equal(t, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC), store.cutoffs[0])
	assert.Equal(t, 1000, store.batches[0])
	assert.Contains(t, logs.String(), "deleted=42")
}

func TestRunOnceError(t *testing.T) {
	t.Parallel()
	store := &fakeStore{err: errors.New("statement timeout")}
	var logs bytes.Buffer
	r := New(store, clock.NewFake(now), slog.New(slog.NewTextHandler(&logs, nil)),
		Config{Keep: time.Hour, Interval: time.Hour, Batch: 10})

	r.RunOnce(t.Context())

	assert.Contains(t, logs.String(), "level=ERROR")
	assert.Contains(t, logs.String(), "statement timeout")
}

func TestRunRepeatsUntilCancelled(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	r := New(store, clock.NewFake(now), slog.New(slog.DiscardHandler),
		Config{Keep: time.Hour, Interval: 5 * time.Millisecond, Batch: 10})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	require.Eventually(t, func() bool { return store.calls() >= 3 }, 5*time.Second, time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}
