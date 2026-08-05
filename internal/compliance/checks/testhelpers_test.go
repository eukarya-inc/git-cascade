package checks

import (
	"archive/tar"
	"compress/gzip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"testing"

	"github.com/google/go-github/v90/github"
)

// fakeGitHub holds per-path responses for the GitHub contents and archive APIs.
type fakeGitHub struct {
	// files maps "owner/repo/filePath" to raw file content (nil = 404).
	files map[string][]byte
	// dirs maps "owner/repo/dirPath" to a list of entry names (for contents API).
	dirs map[string][]string
	// hasArchive tracks repos for which an archive should be served (even if empty).
	// Repos not in this set return 404 from the tarball endpoint.
	hasArchive map[string]bool
	// tarballStatus overrides the HTTP status code served by the tarball endpoint
	// when non-zero (e.g. 500 to test error handling). Redirect still fires first;
	// this applies to the final /tarball/ download handler.
	tarballStatus int
	// tarballBody overrides the raw bytes served by the tarball download endpoint.
	// Useful for injecting a corrupt/non-gzip body.
	tarballBody []byte
}

// newFakeGitHub creates a fakeGitHub with empty maps.
func newFakeGitHub() *fakeGitHub {
	return &fakeGitHub{
		files:      make(map[string][]byte),
		dirs:       make(map[string][]string),
		hasArchive: make(map[string]bool),
	}
}

// setFile registers file content for owner/repo/filepath.
// All registered files are included in the tarball served by the archive endpoint.
func (f *fakeGitHub) setFile(owner, repo, filePath string, content []byte) {
	f.files[owner+"/"+repo+"/"+filePath] = content
	f.hasArchive[owner+"/"+repo] = true
}

// setDir registers a directory listing for owner/repo/dirPath.
func (f *fakeGitHub) setDir(owner, repo, dirPath string, entryNames []string) {
	f.dirs[owner+"/"+repo+"/"+dirPath] = entryNames
}

// setGitRef is a no-op kept for call-site compatibility. The tarball approach
// does not need a separate HEAD-resolution step.
func (f *fakeGitHub) setGitRef(_, _, _, _ string) {}

// setGitTree marks owner/repo as having a valid archive (so the tarball endpoint
// returns 200 instead of 404). This covers repos with an empty file list where
// setFile is never called.
func (f *fakeGitHub) setGitTree(owner, repo, _ string, _ []string) {
	f.hasArchive[owner+"/"+repo] = true
}

// serve starts an httptest.Server that handles:
//   - /api/v3/repos/{owner}/{repo}/contents/{path}       (file/dir contents)
//   - /api/v3/repos/{owner}/{repo}/tarball/{ref}          (archive download redirect)
//   - /tarball/{owner}/{repo}                             (actual gzipped tar)
//
// go-github's WithEnterpriseURLs prefixes all paths with /api/v3/.
func (f *fakeGitHub) serve(t *testing.T) (*httptest.Server, *github.Client) {
	t.Helper()
	mux := http.NewServeMux()

	const repoPrefix = "/api/v3/repos/"
	mux.HandleFunc(repoPrefix, func(w http.ResponseWriter, r *http.Request) {
		rest := r.URL.Path[len(repoPrefix):]
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
		case "tarball":
			// Redirect to the tarball download endpoint using an absolute URL so
			// that the Location header is fully qualified (go-github parses it as-is).
			http.Redirect(w, r, "http://"+r.Host+"/tarball/"+owner+"/"+repo, http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	})

	// Serve the actual gzipped tarball (after the redirect).
	mux.HandleFunc("/tarball/", func(w http.ResponseWriter, r *http.Request) {
		rest := r.URL.Path[len("/tarball/"):]
		parts := splitN(rest, "/", 2)
		if len(parts) < 2 {
			http.NotFound(w, r)
			return
		}
		owner, repo := parts[0], parts[1]
		f.serveTarball(w, owner, repo)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	baseURL := srv.URL + "/"
	client, _ := github.NewClient(github.WithAuthToken("fake-token"), github.WithEnterpriseURLs(baseURL, baseURL))
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

// serveTarball builds an in-memory gzipped tar from all files registered for
// owner/repo and writes it to w. Returns 404 when no archive has been set up
// for the repo (simulates a missing/empty repository). GitHub tarballs wrap
// entries under a top-level directory; we use "owner-repo-testsha/" as that prefix.
func (f *fakeGitHub) serveTarball(w http.ResponseWriter, owner, repo string) {
	// Inject a non-200 status if requested.
	if f.tarballStatus != 0 {
		w.WriteHeader(f.tarballStatus)
		return
	}

	// Inject a raw body override (e.g. corrupt/non-gzip bytes).
	if f.tarballBody != nil {
		w.Header().Set("Content-Type", "application/x-gzip")
		w.Write(f.tarballBody)
		return
	}

	// Check whether this repo has an archive registered (even an empty one).
	if !f.hasArchive[owner+"/"+repo] {
		http.NotFound(w, nil)
		return
	}

	repoKey := owner + "/" + repo + "/"
	prefix := owner + "-" + repo + "-testsha/"

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for key, content := range f.files {
		if len(key) <= len(repoKey) || key[:len(repoKey)] != repoKey {
			continue
		}
		filePath := key[len(repoKey):]
		tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     prefix + filePath,
			Size:     int64(len(content)),
			Mode:     0o644,
		})
		tw.Write(content)
	}

	tw.Close()
	gw.Close()

	w.Header().Set("Content-Type", "application/x-gzip")
	w.Write(buf.Bytes())
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
