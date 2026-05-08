package checks

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"testing"

	"github.com/google/go-github/v84/github"
)

// fakeGitHub holds per-path responses for the GitHub contents API.
// Paths are matched as <owner>/<repo>/<filePath>.
type fakeGitHub struct {
	// files maps a path key to raw file content (nil = 404).
	files map[string][]byte
	// dirs maps a directory path key to a list of entry names.
	dirs map[string][]string
	// gitTrees maps "owner/repo/sha" to a flat list of blob paths (for the Git trees API).
	gitTrees map[string][]string
	// gitRefs maps "owner/repo/refName" to a SHA (for the Git refs API).
	gitRefs map[string]string
}

// newFakeGitHub creates a fakeGitHub with empty maps.
func newFakeGitHub() *fakeGitHub {
	return &fakeGitHub{
		files:    make(map[string][]byte),
		dirs:     make(map[string][]string),
		gitTrees: make(map[string][]string),
		gitRefs:  make(map[string]string),
	}
}

// setFile registers file content for owner/repo/filepath.
func (f *fakeGitHub) setFile(owner, repo, filePath string, content []byte) {
	f.files[owner+"/"+repo+"/"+filePath] = content
}

// setDir registers a directory listing for owner/repo/dirPath.
// entryNames are the file names inside the directory.
func (f *fakeGitHub) setDir(owner, repo, dirPath string, entryNames []string) {
	f.dirs[owner+"/"+repo+"/"+dirPath] = entryNames
}

// setGitRef registers a branch HEAD SHA for the Git refs API.
// branch should be the short branch name (e.g. "main"), not the full ref.
func (f *fakeGitHub) setGitRef(owner, repo, branch, sha string) {
	f.gitRefs[owner+"/"+repo+"/refs/heads/"+branch] = sha
}

// setGitTree registers a flat list of blob file paths for a tree SHA.
// The tree is returned by the Git trees API when queried with recursive=1.
func (f *fakeGitHub) setGitTree(owner, repo, treeSHA string, filePaths []string) {
	f.gitTrees[owner+"/"+repo+"/"+treeSHA] = filePaths
}

// serve starts an httptest.Server that handles:
//   - /api/v3/repos/{owner}/{repo}/contents/{path}  (file/dir contents)
//   - /api/v3/repos/{owner}/{repo}/git/refs/{ref}   (branch HEAD SHA)
//   - /api/v3/repos/{owner}/{repo}/git/trees/{sha}  (recursive file tree)
//
// go-github's WithEnterpriseURLs prefixes all paths with /api/v3/.
func (f *fakeGitHub) serve(t *testing.T) (*httptest.Server, *github.Client) {
	t.Helper()
	mux := http.NewServeMux()

	const prefix = "/api/v3/repos/"
	mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
		rest := r.URL.Path[len(prefix):]
		// Split into at most 4 parts: owner / repo / section / rest
		parts := splitN(rest, "/", 4)
		if len(parts) < 3 {
			http.NotFound(w, r)
			return
		}
		owner, repo, section := parts[0], parts[1], parts[2]
		tail := ""
		if len(parts) == 4 {
			tail = parts[3]
		}

		switch section {
		case "contents":
			f.serveContents(w, owner, repo, tail)
		case "git":
			f.serveGit(w, owner, repo, tail)
		default:
			http.NotFound(w, r)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(nil).WithAuthToken("fake-token")
	baseURL := srv.URL + "/"
	client, _ = client.WithEnterpriseURLs(baseURL, baseURL)
	return srv, client
}

// serveContents handles /repos/{owner}/{repo}/contents/{filePath}.
func (f *fakeGitHub) serveContents(w http.ResponseWriter, owner, repo, filePath string) {
	key := owner + "/" + repo + "/" + filePath

	if entries, ok := f.dirs[key]; ok {
		var items []map[string]any
		for _, name := range entries {
			entryPath := filePath + "/" + name
			if filePath == "" {
				entryPath = name
			}
			items = append(items, map[string]any{
				"type": "file",
				"name": name,
				"path": entryPath,
			})
		}
		if items == nil {
			items = []map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items)
		return
	}

	if content, ok := f.files[key]; ok {
		if content == nil {
			http.NotFound(w, nil)
			return
		}
		encoded := base64.StdEncoding.EncodeToString(content)
		resp := map[string]any{
			"type":     "file",
			"name":     path.Base(filePath),
			"path":     filePath,
			"encoding": "base64",
			"content":  encoded + "\n",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	http.NotFound(w, nil)
}

// serveGit handles /repos/{owner}/{repo}/git/{refs/... | trees/...}.
func (f *fakeGitHub) serveGit(w http.ResponseWriter, owner, repo, tail string) {
	// tail is e.g. "refs/heads/main" or "trees/abc123"
	parts := splitN(tail, "/", 2)
	if len(parts) < 2 {
		http.NotFound(w, nil)
		return
	}
	kind, rest := parts[0], parts[1]

	switch kind {
	case "ref":
		// GetRef uses /git/ref/{ref} (singular). rest is e.g. "heads/main".
		// We store keys as "owner/repo/refs/heads/branch" so prefix "refs/".
		key := owner + "/" + repo + "/refs/" + rest
		sha, ok := f.gitRefs[key]
		if !ok {
			http.NotFound(w, nil)
			return
		}
		resp := map[string]any{
			"ref": "refs/" + rest,
			"object": map[string]any{
				"type": "commit",
				"sha":  sha,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

	case "trees":
		// rest is the tree SHA
		key := owner + "/" + repo + "/" + rest
		filePaths, ok := f.gitTrees[key]
		if !ok {
			http.NotFound(w, nil)
			return
		}
		var entries []map[string]any
		for _, p := range filePaths {
			entries = append(entries, map[string]any{
				"path": p,
				"type": "blob",
				"sha":  "deadbeef",
			})
		}
		resp := map[string]any{
			"sha":       rest,
			"tree":      entries,
			"truncated": false,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

	default:
		http.NotFound(w, nil)
	}
}

// splitN splits s by sep into at most n parts.
func splitN(s, sep string, n int) []string {
	var result []string
	for i := 0; i < n-1; i++ {
		idx := indexOf(s, sep)
		if idx < 0 {
			break
		}
		result = append(result, s[:idx])
		s = s[idx+len(sep):]
	}
	result = append(result, s)
	return result
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

