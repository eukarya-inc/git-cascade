package github

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/go-github/v90/github"
)

type ctxKey struct{}

// WithCache attaches a RepoCache to the context so checkers can share it.
func WithCache(ctx context.Context, c *RepoCache) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// CacheFromContext retrieves the RepoCache from the context, or nil if absent.
func CacheFromContext(ctx context.Context) *RepoCache {
	v, _ := ctx.Value(ctxKey{}).(*RepoCache)
	return v
}

// repoCache memoises ListDirectoryContents and FetchFileContent results within
// a single scan run. Concurrent callers requesting the same key block until the
// first caller's network round-trip completes, then all receive the same result.
// The zero value is ready to use.
type RepoCache struct {
	groups sync.Map // key → *call
}

type call struct {
	wg  sync.WaitGroup
	val any
	err error
}

func (c *RepoCache) do(key string, fn func() (any, error)) (retVal any, retErr error) {
	// Fast path: a completed (non-error) result is already stored.
	if v, ok := c.groups.Load(key); ok {
		cl := v.(*call)
		cl.wg.Wait()
		return cl.val, cl.err
	}

	cl := &call{}
	cl.wg.Add(1)
	actual, loaded := c.groups.LoadOrStore(key, cl)
	if loaded {
		// Another goroutine won the race; wait for it.
		existing := actual.(*call)
		existing.wg.Wait()
		return existing.val, existing.err
	}

	// We own this call. Use a deferred recover so a panic in fn() still
	// unblocks all waiters rather than deadlocking them.
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("cache: panic in fn: %v", r)
			cl.err = retErr
			// Remove the poisoned entry so future callers retry.
			c.groups.Delete(key)
			cl.wg.Done()
		}
	}()

	cl.val, cl.err = fn()

	if cl.err != nil {
		// Do not cache errors: remove the entry so subsequent callers retry.
		// This prevents a transient or context-cancelled result from poisoning
		// all future lookups for this key.
		c.groups.Delete(key)
	}

	cl.wg.Done()
	return cl.val, cl.err
}

type dirResult struct {
	entries []*github.RepositoryContent
	err     error
}

type fileResult struct {
	content []byte
	err     error
}

// ListDirectoryContents returns cached results for (owner, repo, path, ref),
// making exactly one network call regardless of how many goroutines ask.
func (c *RepoCache) ListDirectoryContents(ctx context.Context, client *github.Client, owner, repo, path, ref string) ([]*github.RepositoryContent, error) {
	key := "dir:" + owner + "/" + repo + ":" + ref + ":" + path
	v, err := c.do(key, func() (any, error) {
		entries, err := ListDirectoryContents(ctx, client, owner, repo, path, ref)
		return &dirResult{entries: entries, err: err}, nil
	})
	if err != nil {
		return nil, err
	}
	r := v.(*dirResult)
	return r.entries, r.err
}

// FetchFileContent returns cached results for (owner, repo, path, ref),
// making exactly one network call regardless of how many goroutines ask.
func (c *RepoCache) FetchFileContent(ctx context.Context, client *github.Client, owner, repo, path, ref string) ([]byte, error) {
	key := "file:" + owner + "/" + repo + ":" + ref + ":" + path
	v, err := c.do(key, func() (any, error) {
		content, err := FetchFileContent(ctx, client, owner, repo, path, ref)
		return &fileResult{content: content, err: err}, nil
	})
	if err != nil {
		return nil, err
	}
	r := v.(*fileResult)
	return r.content, r.err
}

// CachedListDirectoryContents calls the cache if one is in ctx, otherwise
// calls ListDirectoryContents directly. Checkers should use this instead of
// calling ListDirectoryContents directly.
func CachedListDirectoryContents(ctx context.Context, client *github.Client, owner, repo, path, ref string) ([]*github.RepositoryContent, error) {
	if c := CacheFromContext(ctx); c != nil {
		return c.ListDirectoryContents(ctx, client, owner, repo, path, ref)
	}
	return ListDirectoryContents(ctx, client, owner, repo, path, ref)
}

// CachedFetchFileContent calls the cache if one is in ctx, otherwise calls
// FetchFileContent directly. Checkers should use this instead of calling
// FetchFileContent directly.
func CachedFetchFileContent(ctx context.Context, client *github.Client, owner, repo, path, ref string) ([]byte, error) {
	if c := CacheFromContext(ctx); c != nil {
		return c.FetchFileContent(ctx, client, owner, repo, path, ref)
	}
	return FetchFileContent(ctx, client, owner, repo, path, ref)
}
