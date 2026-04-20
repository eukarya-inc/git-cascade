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
}

// newFakeGitHub creates a fakeGitHub with empty maps.
func newFakeGitHub() *fakeGitHub {
	return &fakeGitHub{
		files: make(map[string][]byte),
		dirs:  make(map[string][]string),
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

// serve starts an httptest.Server that handles /api/v3/repos/{owner}/{repo}/contents/{path}.
// go-github's WithEnterpriseURLs prefixes all paths with /api/v3/.
func (f *fakeGitHub) serve(t *testing.T) (*httptest.Server, *github.Client) {
	t.Helper()
	mux := http.NewServeMux()

	const prefix = "/api/v3/repos/"
	mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
		// URL pattern: /api/v3/repos/{owner}/{repo}/contents/{path...}
		rest := r.URL.Path[len(prefix):]
		// Split: owner / repo / "contents" / path...
		parts := splitN(rest, "/", 4)
		if len(parts) < 4 || parts[2] != "contents" {
			http.NotFound(w, r)
			return
		}
		owner, repo, filePath := parts[0], parts[1], parts[3]
		key := owner + "/" + repo + "/" + filePath

		// Check if it's a directory we know about.
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

		// Check if it's a file we know about.
		if content, ok := f.files[key]; ok {
			if content == nil {
				http.NotFound(w, r)
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

		// Not found by default.
		http.NotFound(w, r)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(nil).WithAuthToken("fake-token")
	// Point client at our test server.
	baseURL := srv.URL + "/"
	client, _ = client.WithEnterpriseURLs(baseURL, baseURL)
	return srv, client
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

