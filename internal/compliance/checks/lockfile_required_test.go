package checks

import (
	"testing"
)

func TestManifestToLockfile_Entries(t *testing.T) {
	// Verify the expected manifests are covered.
	expectedManifests := []string{"package.json", "go.mod", "Cargo.toml", "pyproject.toml"}
	for _, manifest := range expectedManifests {
		t.Run(manifest, func(t *testing.T) {
			lockfiles, ok := manifestToLockfile[manifest]
			if !ok {
				t.Errorf("manifest %q not in manifestToLockfile map", manifest)
				return
			}
			if len(lockfiles) == 0 {
				t.Errorf("manifest %q has no associated lockfiles", manifest)
			}
		})
	}
}

func TestManifestToLockfile_PackageJSON(t *testing.T) {
	lockfiles := manifestToLockfile["package.json"]
	wantLockfiles := []string{"package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lockb"}
	for _, want := range wantLockfiles {
		found := false
		for _, got := range lockfiles {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected lockfile %q in package.json lockfiles, got %v", want, lockfiles)
		}
	}
}

func TestManifestToLockfile_GoMod(t *testing.T) {
	lockfiles := manifestToLockfile["go.mod"]
	if len(lockfiles) == 0 {
		t.Fatal("go.mod should have at least one lockfile")
	}
	found := false
	for _, lf := range lockfiles {
		if lf == "go.sum" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected go.sum in go.mod lockfiles, got %v", lockfiles)
	}
}

func TestManifestToLockfile_Cargo(t *testing.T) {
	lockfiles := manifestToLockfile["Cargo.toml"]
	if len(lockfiles) == 0 {
		t.Fatal("Cargo.toml should have at least one lockfile")
	}
	found := false
	for _, lf := range lockfiles {
		if lf == "Cargo.lock" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Cargo.lock in Cargo.toml lockfiles, got %v", lockfiles)
	}
}

func TestManifestToLockfile_Pyproject(t *testing.T) {
	lockfiles := manifestToLockfile["pyproject.toml"]
	wantLockfiles := []string{"uv.lock", "poetry.lock", "requirements.txt"}
	for _, want := range wantLockfiles {
		found := false
		for _, got := range lockfiles {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected lockfile %q in pyproject.toml lockfiles, got %v", want, lockfiles)
		}
	}
}

func TestLockfileRequiredChecker_ID(t *testing.T) {
	c := &lockfileRequiredChecker{}
	if c.ID() != "lockfile-required" {
		t.Errorf("expected ID lockfile-required, got %q", c.ID())
	}
}
