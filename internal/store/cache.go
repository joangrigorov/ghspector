package store

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// cache stores read results by key with a time-to-live, and collapses
// concurrent requests for the same key into one load.
//
// The previous version cached into per-feature slices with no shared lock.
// Two screens refreshing at once each issued the request, and whichever
// response landed last won; a slow first response could overwrite a fresh one.
// A keyed cache with single-flight removes both problems.
type cache struct {
	mu      sync.Mutex
	entries map[string]*cacheEntry
	ttl     time.Duration
	// now is swappable so tests can advance time without sleeping.
	now func() time.Time
}

// cacheEntry is an in-flight or completed load. ready is closed when value and
// err are set, which is what lets concurrent callers share one load.
type cacheEntry struct {
	value   any
	err     error
	expires time.Time
	ready   chan struct{}
	// loaded distinguishes a finished entry from an in-flight one, whose
	// expires is still the zero time.
	loaded bool
}

func newCache(ttl time.Duration) *cache {
	return &cache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
		now:     time.Now,
	}
}

// getOrLoad returns the cached value for key, loading it if absent or expired.
//
// Concurrent callers for the same key wait for the first load instead of
// issuing their own. Failures are not cached, so a transient error does not
// wedge the key until its TTL elapses.
func getOrLoad[T any](ctx context.Context, c *cache, key string, load func(context.Context) (T, error)) (T, error) {
	var zero T

	c.mu.Lock()
	c.sweepLocked()

	if e, ok := c.entries[key]; ok {
		// An in-flight entry is joined; a live entry is served from cache.
		if !e.loaded || c.now().Before(e.expires) {
			ready := e.ready
			c.mu.Unlock()

			select {
			case <-ready:
			case <-ctx.Done():
				return zero, ctx.Err()
			}
			return valueOf[T](e)
		}
		delete(c.entries, key)
	}

	// Claim the key. Whoever inserts the entry owns the load.
	e := &cacheEntry{ready: make(chan struct{})}
	c.entries[key] = e
	c.mu.Unlock()

	value, err := load(ctx)

	c.mu.Lock()
	if err != nil {
		// Leave nothing behind, so the next caller retries immediately.
		if current, ok := c.entries[key]; ok && current == e {
			delete(c.entries, key)
		}
		e.err = err
	} else {
		e.value = value
		e.expires = c.now().Add(c.ttl)
	}
	e.loaded = true
	close(e.ready)
	c.mu.Unlock()

	if err != nil {
		return zero, err
	}
	return value, nil
}

// valueOf reads a completed entry. The type assertion is expected to hold
// because a key is only ever loaded with one type; a mismatch is a programming
// error rather than a runtime condition.
func valueOf[T any](e *cacheEntry) (T, error) {
	var zero T
	if e.err != nil {
		return zero, e.err
	}
	v, ok := e.value.(T)
	if !ok {
		return zero, fmt.Errorf("store: cache key holds %T, want %T", e.value, zero)
	}
	return v, nil
}

// sweepLocked removes finished entries whose TTL has passed. Called on every
// lookup, which is cheap because the map stays small relative to the number of
// distinct requests.
func (c *cache) sweepLocked() {
	now := c.now()
	for k, e := range c.entries {
		if e.loaded && !now.Before(e.expires) {
			delete(c.entries, k)
		}
	}
}

// invalidate drops every entry, in-flight or not. In-flight loads still finish
// for whoever is already waiting; the result is simply not stored.
func (c *cache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*cacheEntry)
}
