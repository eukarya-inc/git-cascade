package compliance

import (
	"context"
	"sort"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v84/github"
)

// fakeChecker is a minimal Checker for registry tests.
type fakeChecker struct{ id string }

func (f *fakeChecker) ID() string { return f.id }
func (f *fakeChecker) Check(_ context.Context, _ *github.Client, _ gh.Repository, _ config.Rule) (*Result, error) {
	return nil, nil
}

// isolatedRegistry runs fn with a clean registry copy and restores it after.
func withCleanRegistry(t *testing.T, fn func()) {
	t.Helper()
	saved := make(map[string]Checker, len(registry))
	for k, v := range registry {
		saved[k] = v
	}
	t.Cleanup(func() { registry = saved })
	fn()
}

func TestRegisterAndGetChecker(t *testing.T) {
	withCleanRegistry(t, func() {
		registry = map[string]Checker{} // start clean

		c := &fakeChecker{id: "test-rule"}
		Register(c)

		got := GetChecker("test-rule")
		if got == nil {
			t.Fatal("expected registered checker, got nil")
		}
		if got.ID() != "test-rule" {
			t.Errorf("expected ID=test-rule, got %q", got.ID())
		}
	})
}

func TestGetChecker_NotFound(t *testing.T) {
	withCleanRegistry(t, func() {
		registry = map[string]Checker{}

		if got := GetChecker("no-such-rule"); got != nil {
			t.Errorf("expected nil for unknown rule, got %v", got)
		}
	})
}

func TestRegister_Overwrite(t *testing.T) {
	withCleanRegistry(t, func() {
		registry = map[string]Checker{}

		c1 := &fakeChecker{id: "dup"}
		c2 := &fakeChecker{id: "dup"}
		Register(c1)
		Register(c2)

		got := GetChecker("dup")
		if got != c2 {
			t.Error("expected second registration to overwrite the first")
		}
	})
}

func TestListCheckers(t *testing.T) {
	withCleanRegistry(t, func() {
		registry = map[string]Checker{}

		Register(&fakeChecker{id: "alpha"})
		Register(&fakeChecker{id: "beta"})
		Register(&fakeChecker{id: "gamma"})

		ids := ListCheckers()
		if len(ids) != 3 {
			t.Fatalf("expected 3 checkers, got %d", len(ids))
		}

		sort.Strings(ids)
		want := []string{"alpha", "beta", "gamma"}
		for i, w := range want {
			if ids[i] != w {
				t.Errorf("ids[%d] = %q, want %q", i, ids[i], w)
			}
		}
	})
}

func TestListCheckers_Empty(t *testing.T) {
	withCleanRegistry(t, func() {
		registry = map[string]Checker{}

		ids := ListCheckers()
		if len(ids) != 0 {
			t.Errorf("expected empty slice, got %v", ids)
		}
	})
}
