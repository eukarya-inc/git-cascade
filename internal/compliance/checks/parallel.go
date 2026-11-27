package checks

import (
	"context"
	"sync"

	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v90/github"
)

// fetchFirstExisting probes all paths concurrently and returns the first path
// whose content is non-nil, along with the content. Returns ("", nil, nil) when
// none of the paths exist. The first non-nil result wins; in-flight requests for
// other paths are cancelled via context so we don't waste API quota.
func fetchFirstExisting(ctx context.Context, client *github.Client, owner, repo, ref string, paths []string) (string, error) {
	if len(paths) == 0 {
		return "", nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type hit struct {
		path string
	}

	hitCh := make(chan hit, len(paths))
	errCh := make(chan error, len(paths))

	var wg sync.WaitGroup
	wg.Add(len(paths))
	for _, p := range paths {
		go func() {
			defer wg.Done()
			content, err := gh.CachedFetchFileContent(ctx, client, owner, repo, p, ref)
			if err != nil {
				// Ignore context-cancelled errors from the winning sibling.
				if ctx.Err() != nil {
					return
				}
				errCh <- err
				return
			}
			if content != nil {
				hitCh <- hit{path: p}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(hitCh)
		close(errCh)
	}()

	// Return the first hit or first error, whichever arrives first.
	for {
		select {
		case h, ok := <-hitCh:
			if !ok {
				// Channel closed with no hits — check for errors.
				hitCh = nil
				continue
			}
			cancel() // stop siblings
			return h.path, nil
		case err, ok := <-errCh:
			if !ok {
				errCh = nil
			} else {
				return "", err
			}
		}
		if hitCh == nil && errCh == nil {
			return "", nil
		}
	}
}

// fetchAllConcurrency bounds the in-flight requests of one fetchAll call.
const fetchAllConcurrency = 4

// fetchAll fetches every path concurrently (at most fetchAllConcurrency at a
// time) and returns the contents in the same order as paths; a missing file
// yields nil. The first error cancels the remaining fetches and is returned.
func fetchAll(ctx context.Context, client *github.Client, owner, repo, ref string, paths []string) ([][]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	out := make([][]byte, len(paths))
	sem := make(chan struct{}, fetchAllConcurrency)
	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
	)
	for i, p := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			content, err := gh.CachedFetchFileContent(ctx, client, owner, repo, p, ref)
			if err != nil {
				once.Do(func() { firstErr = err; cancel() })
				return
			}
			out[i] = content
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
