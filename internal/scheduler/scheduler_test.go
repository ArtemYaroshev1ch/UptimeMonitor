package scheduler

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/clock"
)

// fakeStore hands out queued targets (or an endless supply) and records
// saved results.
type fakeStore struct {
	mu       sync.Mutex
	queue    []domain.CheckTarget
	endless  bool
	claimErr error
	saveErr  error
	limits   []int
	saved    map[uuid.UUID]domain.CheckResult
}

func newFakeStore(targets ...domain.CheckTarget) *fakeStore {
	return &fakeStore{queue: targets, saved: map[uuid.UUID]domain.CheckResult{}}
}

func (f *fakeStore) ClaimDue(_ context.Context, limit int) ([]domain.CheckTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limits = append(f.limits, limit)
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	if f.endless {
		out := make([]domain.CheckTarget, limit)
		for i := range out {
			out[i] = domain.CheckTarget{MonitorID: uuid.New()}
		}
		return out, nil
	}
	n := min(limit, len(f.queue))
	out := f.queue[:n]
	f.queue = f.queue[n:]
	return out, nil
}

func (f *fakeStore) SaveResult(_ context.Context, id uuid.UUID, res domain.CheckResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved[id] = res
	return nil
}

func (f *fakeStore) savedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.saved)
}

// fakeChecker succeeds at once, or blocks until release is closed (or its
// context ends) when release is set. It tracks how many checks run at once.
type fakeChecker struct {
	release      chan struct{}
	running, max atomic.Int64
	aborted      atomic.Int64
}

func (f *fakeChecker) Check(ctx context.Context, _ domain.CheckTarget) domain.CheckResult {
	n := f.running.Add(1)
	defer f.running.Add(-1)
	for {
		m := f.max.Load()
		if n <= m || f.max.CompareAndSwap(m, n) {
			break
		}
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			f.aborted.Add(1)
			return domain.CheckResult{ErrorKind: domain.ErrorTimeout}
		}
	}
	return domain.CheckResult{Success: true}
}

func testConfig() Config {
	return Config{
		Concurrency:     3,
		BatchSize:       50,
		Tick:            5 * time.Millisecond,
		ShutdownTimeout: 5 * time.Second,
		SaveTimeout:     time.Second,
	}
}

// start runs s in the background; the returned function cancels it and
// waits for Run to return.
func start(t *testing.T, s *Scheduler) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	return func() {
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(10 * time.Second):
			t.Fatal("Run did not return")
		}
	}
}

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestSchedulerChecksAndSaves(t *testing.T) {
	t.Parallel()
	targets := []domain.CheckTarget{{MonitorID: uuid.New()}, {MonitorID: uuid.New()}, {MonitorID: uuid.New()}, {MonitorID: uuid.New()}}
	store := newFakeStore(targets...)
	s := New(store, &fakeChecker{}, discard(), clock.System{}, testConfig())

	stop := start(t, s)
	require.Eventually(t, func() bool { return store.savedCount() == len(targets) }, 5*time.Second, 5*time.Millisecond)
	stop()

	for _, tg := range targets {
		assert.True(t, store.saved[tg.MonitorID].Success)
	}
	assert.False(t, s.Heartbeat().IsZero())
}

// The scheduler never runs more checks than the pool size and never claims
// more monitors than it has free slots for.
func TestSchedulerRespectsConcurrency(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.endless = true
	checker := &fakeChecker{release: make(chan struct{})}
	cfg := testConfig()
	s := New(store, checker, discard(), clock.System{}, cfg)

	stop := start(t, s)
	require.Eventually(t, func() bool { return checker.running.Load() == int64(cfg.Concurrency) }, 5*time.Second, time.Millisecond)
	time.Sleep(50 * time.Millisecond) // several more ticks while saturated
	assert.Equal(t, int64(cfg.Concurrency), checker.max.Load())
	close(checker.release)
	stop()

	store.mu.Lock()
	defer store.mu.Unlock()
	for _, limit := range store.limits {
		assert.LessOrEqual(t, limit, cfg.Concurrency)
		assert.Positive(t, limit, "no claim while the pool is full")
	}
}

func TestSchedulerBatchSize(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	cfg := testConfig()
	cfg.Concurrency, cfg.BatchSize = 100, 7
	s := New(store, &fakeChecker{}, discard(), clock.System{}, cfg)

	stop := start(t, s)
	require.Eventually(t, func() bool { store.mu.Lock(); defer store.mu.Unlock(); return len(store.limits) > 0 }, time.Second, time.Millisecond)
	stop()

	assert.Equal(t, 7, store.limits[0])
}

// Shutdown lets running checks finish and save their results.
func TestSchedulerGracefulShutdown(t *testing.T) {
	t.Parallel()
	store := newFakeStore(domain.CheckTarget{MonitorID: uuid.New()})
	checker := &fakeChecker{release: make(chan struct{})}
	s := New(store, checker, discard(), clock.System{}, testConfig())
	stop := start(t, s)
	require.Eventually(t, func() bool { return checker.running.Load() == 1 }, 5*time.Second, time.Millisecond)

	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("Run returned while a check was still running")
	case <-time.After(50 * time.Millisecond):
	}

	close(checker.release)
	<-stopped
	assert.Equal(t, 1, store.savedCount(), "the running check was saved")
	assert.Zero(t, checker.aborted.Load())
}

// After ShutdownTimeout, checks that are still running are cancelled.
func TestSchedulerShutdownTimeout(t *testing.T) {
	t.Parallel()
	store := newFakeStore(domain.CheckTarget{MonitorID: uuid.New()})
	checker := &fakeChecker{release: make(chan struct{})} // never released
	cfg := testConfig()
	cfg.ShutdownTimeout = 20 * time.Millisecond
	s := New(store, checker, discard(), clock.System{}, cfg)
	stop := start(t, s)
	require.Eventually(t, func() bool { return checker.running.Load() == 1 }, 5*time.Second, time.Millisecond)

	stop()

	assert.Equal(t, int64(1), checker.aborted.Load())
}

func TestSchedulerClaimError(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.claimErr = errors.New("connection refused")
	var logs bytes.Buffer
	s := New(store, &fakeChecker{}, slog.New(slog.NewTextHandler(&logs, nil)), clock.System{}, testConfig())

	stop := start(t, s)
	require.Eventually(t, func() bool { store.mu.Lock(); defer store.mu.Unlock(); return len(store.limits) >= 2 }, time.Second, time.Millisecond)
	stop()

	assert.True(t, s.Heartbeat().IsZero(), "no heartbeat without a successful claim")
	assert.Contains(t, logs.String(), "connection refused")
}

func TestSchedulerDeletedMonitorIsNotAnError(t *testing.T) {
	t.Parallel()
	store := newFakeStore(domain.CheckTarget{MonitorID: uuid.New()})
	store.saveErr = domain.ErrNotFound
	var logs bytes.Buffer
	s := New(store, &fakeChecker{}, slog.New(slog.NewTextHandler(&logs, nil)), clock.System{}, testConfig())

	stop := start(t, s)
	require.Eventually(t, func() bool { store.mu.Lock(); defer store.mu.Unlock(); return len(store.queue) == 0 }, time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	stop()

	assert.NotContains(t, logs.String(), "level=ERROR")
}

func TestHeartbeatUsesClock(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := New(newFakeStore(), &fakeChecker{}, discard(), clock.NewFake(now), testConfig())

	s.beat()

	assert.True(t, now.Equal(s.Heartbeat()))
}
