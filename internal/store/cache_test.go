package store

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock is a manually advanced clock for the cache.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestGetOrLoadCachesUntilTTL(t *testing.T) {
	clock := newFakeClock()
	c := newCache(10 * time.Second)
	c.now = clock.Now

	var calls int
	load := func(context.Context) (string, error) {
		calls++
		return "value", nil
	}

	for i := 0; i < 5; i++ {
		got, err := getOrLoad(context.Background(), c, "k", load)
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if got != "value" {
			t.Fatalf("iteration %d: got %q", i, got)
		}
	}
	if calls != 1 {
		t.Fatalf("load ran %d times, want 1", calls)
	}

	clock.Advance(11 * time.Second)
	if _, err := getOrLoad(context.Background(), c, "k", load); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("after TTL expiry load ran %d times, want 2", calls)
	}
}

// TestGetOrLoadCollapsesConcurrentLoads is the single-flight invariant: a burst
// of identical requests costs one network call.
func TestGetOrLoadCollapsesConcurrentLoads(t *testing.T) {
	c := newCache(time.Minute)

	const goroutines = 32
	release := make(chan struct{})
	var calls int64

	load := func(context.Context) (int, error) {
		atomic.AddInt64(&calls, 1)
		<-release
		return 42, nil
	}

	var wg sync.WaitGroup
	results := make([]int, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, err := getOrLoad(context.Background(), c, "shared", load)
			if err != nil {
				t.Errorf("goroutine %d: %v", i, err)
				return
			}
			results[i] = v
		}(i)
	}

	// Give the goroutines time to pile up on the same key, then let the single
	// load finish.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Fatalf("load ran %d times, want 1", got)
	}
	for i, v := range results {
		if v != 42 {
			t.Fatalf("goroutine %d got %d, want 42", i, v)
		}
	}
}

// TestFailedLoadsAreNotCached keeps a transient error from wedging a key.
func TestFailedLoadsAreNotCached(t *testing.T) {
	c := newCache(time.Hour)
	boom := errors.New("boom")

	var calls int
	load := func(context.Context) (string, error) {
		calls++
		if calls == 1 {
			return "", boom
		}
		return "recovered", nil
	}

	if _, err := getOrLoad(context.Background(), c, "k", load); !errors.Is(err, boom) {
		t.Fatalf("first call error = %v, want boom", err)
	}
	got, err := getOrLoad(context.Background(), c, "k", load)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got != "recovered" {
		t.Fatalf("second call = %q, want recovered", got)
	}
}

// TestConcurrentFailureIsShared checks that waiters on a failing load see the
// error rather than a zero value.
func TestConcurrentFailureIsShared(t *testing.T) {
	c := newCache(time.Minute)
	boom := errors.New("nope")
	release := make(chan struct{})

	load := func(context.Context) (string, error) {
		<-release
		return "", boom
	}

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = getOrLoad(context.Background(), c, "k", load)
		}(i)
	}
	time.Sleep(30 * time.Millisecond)
	close(release)
	wg.Wait()

	for i, err := range errs {
		if !errors.Is(err, boom) {
			t.Errorf("goroutine %d error = %v, want boom", i, err)
		}
	}
}

func TestInvalidateForcesReload(t *testing.T) {
	c := newCache(time.Hour)
	var calls int
	load := func(context.Context) (int, error) {
		calls++
		return calls, nil
	}

	if v, _ := getOrLoad(context.Background(), c, "k", load); v != 1 {
		t.Fatalf("first value = %d", v)
	}
	c.invalidate()
	if v, _ := getOrLoad(context.Background(), c, "k", load); v != 2 {
		t.Fatalf("after invalidate value = %d, want 2", v)
	}
}

func TestWaitingRespectsContextCancellation(t *testing.T) {
	c := newCache(time.Minute)
	release := make(chan struct{})
	finished := make(chan struct{})

	// Occupy the key with a load that will not finish until the test releases it.
	started := make(chan struct{})
	go func() {
		defer close(finished)
		_, _ = getOrLoad(context.Background(), c, "k", func(context.Context) (int, error) {
			close(started)
			<-release
			return 1, nil
		})
	}()
	<-started
	defer func() {
		close(release)
		<-finished
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := getOrLoad(ctx, c, "k", func(context.Context) (int, error) {
		t.Error("a cancelled waiter started its own load")
		return 0, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestSweepRemovesExpiredEntries(t *testing.T) {
	clock := newFakeClock()
	c := newCache(time.Second)
	c.now = clock.Now

	for _, key := range []string{"a", "b", "c"} {
		if _, err := getOrLoad(context.Background(), c, key, func(context.Context) (int, error) {
			return 1, nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	clock.Advance(2 * time.Second)
	if _, err := getOrLoad(context.Background(), c, "d", func(context.Context) (int, error) {
		return 2, nil
	}); err != nil {
		t.Fatal(err)
	}

	c.mu.Lock()
	remaining := len(c.entries)
	c.mu.Unlock()
	if remaining != 1 {
		t.Fatalf("cache holds %d entries after sweep, want 1", remaining)
	}
}

func TestValueOfTypeMismatchIsReported(t *testing.T) {
	c := newCache(time.Hour)
	if _, err := getOrLoad(context.Background(), c, "k", func(context.Context) (int, error) {
		return 7, nil
	}); err != nil {
		t.Fatal(err)
	}

	c.mu.Lock()
	e := c.entries["k"]
	c.mu.Unlock()

	if _, err := valueOf[string](e); err == nil {
		t.Fatal("expected a type-mismatch error")
	}
}
