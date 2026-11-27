package checks

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v90/github"
)

// fromLine matches FROM instructions in Dockerfiles.
// Captures: 1 = image reference (after an optional --platform flag),
// 2 = stage name from a trailing `AS name`, if any.
var fromLine = regexp.MustCompile(`(?i)^\s*FROM\s+(?:--platform=\S+\s+)?(\S+)(?:\s+AS\s+(\S+))?`)

// argLine matches ARG instructions. Captures the declarations after ARG.
var argLine = regexp.MustCompile(`(?i)^\s*ARG\s+(.+)$`)

// argRef matches ${VAR}, ${VAR:-word}, ${VAR-word}, ${VAR:+word}, ${VAR+word}
// and $VAR.
var argRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:?[-+])([^}]*))?\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// sha256Digest matches a @sha256: pinned reference.
var sha256Digest = regexp.MustCompile(`@sha256:[0-9a-f]{64}`)

// notDockerfileExts are extensions that mark a Dockerfile-named file as
// something else: source code (dockerfile_digest.go), docs (Dockerfile.md),
// or data/config files.
var notDockerfileExts = map[string]bool{
	".dockerignore": true,
	".md":           true, ".markdown": true, ".txt": true, ".rst": true, ".adoc": true, ".html": true,
	".go": true, ".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".tsx": true, ".jsx": true,
	".py": true, ".rb": true, ".rs": true, ".java": true, ".kt": true, ".php": true, ".sh": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true, ".xml": true, ".lock": true,
	".snap": true, ".golden": true, ".bak": true, ".orig": true,
}

// isDockerfile reports whether p names a Dockerfile: Dockerfile,
// Dockerfile.<x>, Dockerfile-<x>, Dockerfile_<x>, <x>.Dockerfile, or the
// same forms of Containerfile (case-insensitive). Names ending in a source,
// doc, or data extension (see notDockerfileExts) are excluded.
func isDockerfile(p string) bool {
	name := strings.ToLower(path.Base(p))
	for _, base := range []string{"dockerfile", "containerfile"} {
		if name == base || strings.HasSuffix(name, "."+base) {
			return true
		}
	}
	if notDockerfileExts[path.Ext(name)] {
		return false
	}
	for _, base := range []string{"dockerfile", "containerfile"} {
		for _, sep := range []string{".", "-", "_"} {
			if strings.HasPrefix(name, base+sep) {
				return true
			}
		}
	}
	return false
}

// skipDockerfileDir reports whether p lies in a directory whose Dockerfiles
// the repository does not own or ship: third-party code (node_modules,
// vendor, third_party) and test fixtures (testdata).
func skipDockerfileDir(p string) bool {
	for _, seg := range strings.Split(path.Dir(p), "/") {
		switch seg {
		case "node_modules", "vendor", "third_party", "testdata":
			return true
		}
	}
	return false
}

// unpinnedBaseImage is one FROM instruction whose image is not pinned to a
// sha256 digest.
type unpinnedBaseImage struct {
	Line     int    // 0-based index into the content's lines (first line of a continued instruction)
	Image    string // image reference as written, e.g. "nginx:${NGINX_VERSION}"
	Resolved string // Image with ARG defaults substituted, e.g. "nginx:1.27"
}

// logicalLine is a Dockerfile instruction with backslash continuations joined.
type logicalLine struct {
	start int // 0-based index of its first physical line
	text  string
}

// joinContinuations joins lines ending in a backslash with the next line.
func joinContinuations(lines []string) []logicalLine {
	var out []logicalLine
	for i := 0; i < len(lines); i++ {
		ll := logicalLine{start: i}
		var b strings.Builder
		for {
			line := strings.TrimRight(lines[i], " \t\r")
			if !strings.HasSuffix(line, "\\") || i+1 >= len(lines) {
				b.WriteString(line)
				break
			}
			b.WriteString(strings.TrimSuffix(line, "\\"))
			b.WriteString(" ")
			i++
		}
		ll.text = b.String()
		out = append(out, ll)
	}
	return out
}

// findUnpinnedBaseImages scans a Dockerfile's content for FROM images not
// pinned to a sha256 digest. ARG references are resolved from the defaults
// of ARGs declared before the first FROM (the only ARGs a FROM can see);
// a default may itself reference earlier ARGs. Skipped: scratch, references
// to earlier build stages, references that depend on an ARG with no default
// (supplied at build time, so they can't be checked statically), and lines
// with a git-cascade:allow comment.
func findUnpinnedBaseImages(content string) []unpinnedBaseImage {
	lines := strings.Split(content, "\n")
	// Global ARGs: a nil value means declared without a (resolvable) default.
	args := map[string]*string{}
	stages := map[string]bool{} // build stage names seen so far (lowercase)
	seenFrom := false
	var out []unpinnedBaseImage

	for _, ll := range joinContinuations(lines) {
		if !seenFrom {
			if m := argLine.FindStringSubmatch(ll.text); m != nil {
				for _, decl := range strings.Fields(m[1]) {
					name, val, hasDefault := strings.Cut(decl, "=")
					args[name] = nil
					if hasDefault {
						if v, ok := expandArgs(strings.Trim(val, `"'`), args); ok {
							args[name] = &v
						}
					}
				}
				continue
			}
		}

		m := fromLine.FindStringSubmatch(ll.text)
		if m == nil {
			continue
		}
		seenFrom = true
		image := m[1]
		if m[2] != "" {
			stages[strings.ToLower(m[2])] = true
		}

		resolved, ok := expandArgs(image, args)
		if !ok {
			continue
		}
		if strings.EqualFold(resolved, "scratch") {
			continue
		}
		// FROM <stage> refers to an earlier stage, not a registry image. The
		// stage being defined on this line doesn't count (FROM x AS x).
		if stages[strings.ToLower(resolved)] && !strings.EqualFold(resolved, m[2]) {
			continue
		}
		if sha256Digest.MatchString(resolved) {
			continue
		}
		if hasAllowComment(lines, ll.start) {
			continue
		}
		out = append(out, unpinnedBaseImage{Line: ll.start, Image: image, Resolved: resolved})
	}
	return out
}

// expandArgs substitutes ARG references in s using args, following the
// Dockerfile forms ${VAR}, $VAR, ${VAR:-word}, ${VAR-word}, ${VAR:+word} and
// ${VAR+word}. A variable counts as set when it has a default. It returns
// false when a plain reference names a variable that isn't set (undeclared,
// or declared without a default, so only known at build time).
func expandArgs(s string, args map[string]*string) (string, bool) {
	ok := true
	out := argRef.ReplaceAllStringFunc(s, func(ref string) string {
		sm := argRef.FindStringSubmatch(ref)
		name, op, word := sm[1], sm[2], sm[3]
		if name == "" {
			name = sm[4]
		}
		val, set := "", false
		if p := args[name]; p != nil {
			val, set = *p, true
		}
		switch op {
		case ":-":
			if set && val != "" {
				return val
			}
			return word
		case "-":
			if set {
				return val
			}
			return word
		case ":+":
			if set && val != "" {
				return word
			}
			return ""
		case "+":
			if set {
				return word
			}
			return ""
		}
		if !set {
			ok = false
		}
		return val
	})
	return out, ok
}

type dockerfileDigestChecker struct{}

func (c *dockerfileDigestChecker) ID() string { return "dockerfile-digest" }

func (c *dockerfileDigestChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	ref := repo.DefaultBranch
	result := func(status compliance.Status, msg string) *compliance.Result {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   status,
			Severity: rule.Severity,
			Message:  msg,
		}
	}

	tree, err := gh.CachedListTree(ctx, client, repo.Owner, repo.Name, ref)
	if err != nil {
		return nil, err
	}
	if tree == nil {
		return result(compliance.StatusSkip, "repository is empty"), nil
	}

	var paths []string
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] && isDockerfile(p) && !skipDockerfileDir(p) {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	for _, p := range tree.Paths {
		add(p)
	}
	if tree.Truncated {
		// The listing may have stopped before the usual locations; always
		// cover them so a huge repo is never checked less than the root.
		root, err := gh.CachedListDirectoryContents(ctx, client, repo.Owner, repo.Name, "", ref)
		if err != nil {
			return nil, err
		}
		for _, e := range root {
			if e.GetType() == "file" {
				add(e.GetPath())
			}
		}
		add("docker/Dockerfile")
	}

	contents, err := fetchAll(ctx, client, repo.Owner, repo.Name, ref, paths)
	if err != nil {
		return nil, err
	}

	var violations []string
	found := false
	for i, p := range paths {
		if contents[i] == nil {
			continue
		}
		found = true
		for _, v := range findUnpinnedBaseImages(string(contents[i])) {
			desc := fmt.Sprintf("%s:%d: FROM %s", p, v.Line+1, v.Image)
			if v.Resolved != v.Image {
				desc += fmt.Sprintf(" (= %s)", v.Resolved)
			}
			violations = append(violations, desc)
		}
	}

	note := ""
	if tree.Truncated {
		note = " (repository tree listing was truncated by GitHub; some Dockerfiles may not have been checked)"
	}

	if !found {
		return result(compliance.StatusSkip, "no Dockerfiles found"+note), nil
	}
	if len(violations) > 0 {
		return result(compliance.StatusFail, fmt.Sprintf("%d FROM image(s) not pinned to SHA256 digest: %s%s", len(violations), strings.Join(violations, "; "), note)), nil
	}
	return result(compliance.StatusPass, "all Dockerfile FROM images pinned to SHA256 digest"+note), nil
}

func init() {
	compliance.Register(&dockerfileDigestChecker{})
}
