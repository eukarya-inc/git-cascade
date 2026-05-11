package github

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// TestRepoCacheDoDeduplicatesConcurrentCalls verifies that concurrent callers
// for the same key result in exactly one fn() invocation.
func TestRepoCacheDoDeduplicatesConcurrentCalls(t *testing.T) {
	var calls atomic.Int32
	fn := func() (any, error) {
		calls.Add(1)
		return "result", nil
	}

	c := &RepoCache{}
	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	results := make([]any, goroutines)
	for i := range goroutines {
		go func() {
			defer wg.Done()
			v, err := c.do("key", fn)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			results[i] = v
		}()
	}
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Errorf("fn called %d times, want 1", got)
	}
	for i, r := range results {
		if r != "result" {
			t.Errorf("goroutine %d got %v, want \"result\"", i, r)
		}
	}
}

// TestRepoCacheDoDoesNotCacheErrors verifies that an error result is not stored,
// so the next caller retries fn() from scratch.
func TestRepoCacheDoDoesNotCacheErrors(t *testing.T) {
	attempt := 0
	sentinel := errors.New("transient")
	fn := func() (any, error) {
		attempt++
		if attempt == 1 {
			return nil, sentinel
		}
		return "ok", nil
	}

	c := &RepoCache{}

	_, err := c.do("key", fn)
	if !errors.Is(err, sentinel) {
		t.Fatalf("first call: got err %v, want %v", err, sentinel)
	}

	v, err := c.do("key", fn)
	if err != nil {
		t.Fatalf("second call: unexpected error %v", err)
	}
	if v != "ok" {
		t.Errorf("second call: got %v, want \"ok\"", v)
	}
	if attempt != 2 {
		t.Errorf("fn called %d times, want 2", attempt)
	}
}

// TestRepoCacheDoRecoversPanic verifies that a panic inside fn() unblocks all
// waiters and returns an error rather than deadlocking.
func TestRepoCacheDoRecoversPanic(t *testing.T) {
	fn := func() (any, error) {
		panic("boom")
	}

	c := &RepoCache{}
	_, err := c.do("key", fn)
	if err == nil {
		t.Fatal("expected error from panic, got nil")
	}

	// The poisoned entry must have been removed; a second call should retry.
	attempt := 0
	fn2 := func() (any, error) {
		attempt++
		return "recovered", nil
	}
	v, err := c.do("key", fn2)
	if err != nil {
		t.Fatalf("second call after panic: unexpected error %v", err)
	}
	if v != "recovered" {
		t.Errorf("second call after panic: got %v, want \"recovered\"", v)
	}
}

// TestWithCacheRoundTrip verifies that WithCache and CacheFromContext are
// inverse operations.
func TestWithCacheRoundTrip(t *testing.T) {
	c := &RepoCache{}
	ctx := WithCache(context.Background(), c)
	got := CacheFromContext(ctx)
	if got != c {
		t.Errorf("CacheFromContext returned %p, want %p", got, c)
	}
}

// TestCacheFromContextNil verifies that CacheFromContext returns nil when no
// cache has been attached.
func TestCacheFromContextNil(t *testing.T) {
	if got := CacheFromContext(context.Background()); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}
