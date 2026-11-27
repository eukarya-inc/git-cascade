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

// versionTaggedDigest matches name:X.Y.Z[suffix]@sha256:<digest>, where name
// may carry a registry (with port) and path, and the tag is three numeric
// components with an optional leading v (v3.2.0) and an optional suffix
// (-alpine, .4-bookworm, _11-jre, rc2-slim).
var versionTaggedDigest = regexp.MustCompile(`^(?:[^/@\s]+/)*[^/:@\s]+:v?\d+\.\d+\.\d+(?:[-._]?[A-Za-z0-9][\w.-]*)?@sha256:[0-9a-f]{64}$`)

// imageName returns ref without its tag and digest, e.g.
// "gcr.io/distroless/static:nonroot@sha256:..." → "gcr.io/distroless/static".
// A registry port (localhost:5000/app) is kept.
func imageName(ref string) string {
	ref, _, _ = strings.Cut(ref, "@")
	slash := strings.LastIndex(ref, "/")
	if colon := strings.LastIndex(ref, ":"); colon > slash {
		ref = ref[:colon]
	}
	return ref
}

// exemptImages is the exempt_images param: path.Match patterns matched
// against the image name (no tag or digest) of images that publish no
// X.Y.Z tags, such as gcr.io/distroless/*. A matching image still needs a
// sha256 digest.
type exemptImages []string

func parseExemptImages(rule config.Rule) (exemptImages, error) {
	patterns := rule.ListParams["exempt_images"]
	for _, p := range patterns {
		if _, err := path.Match(p, ""); err != nil {
			return nil, fmt.Errorf("rule %s: invalid exempt_images pattern %q: %w", rule.ID, p, err)
		}
	}
	return patterns, nil
}

func (e exemptImages) match(ref string) bool {
	name := imageName(ref)
	for _, p := range e {
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}

// unversionedFinalImage is a Dockerfile whose final stage's base image is not
// in name:X.Y.Z@sha256:<digest> form.
type unversionedFinalImage struct {
	Line     int    // 0-based line of the FROM that names the image
	Image    string // image reference as written
	Resolved string // Image with ARG defaults substituted
}

// findUnversionedFinalImage checks the image the final stage is built on:
// the last FROM, followed through `FROM <stage>` references back to the
// stage that names a real image. It returns nil when that image is in
// name:X.Y.Z@sha256:<digest> form, or when it can't be checked: no FROM,
// scratch, or an ARG with no default. An image matching exempt only needs
// a sha256 digest. A git-cascade:allow comment on the
// FROM that names the image (same line or the line above, as in other
// rules) suppresses it; so does one on the same line as the final FROM when
// that is a stage reference. The line above a stage-reference FROM is not
// considered, since it usually belongs to an unrelated instruction.
func findUnversionedFinalImage(content string, exempt exemptImages) *unversionedFinalImage {
	lines, froms := parseFroms(content)
	if len(froms) == 0 {
		return nil
	}
	final := froms[len(froms)-1]
	f := final
	for f.IsStage {
		f = froms[f.FromStage]
	}
	if !f.Resolvable || strings.EqualFold(f.Resolved, "scratch") {
		return nil
	}
	if versionTaggedDigest.MatchString(f.Resolved) {
		return nil
	}
	if exempt.match(f.Resolved) && sha256Digest.MatchString(f.Resolved) {
		return nil
	}
	if hasAllowComment(lines, f.Line) || hasAllowComment(lines[final.Line:final.Line+1], 0) {
		return nil
	}
	return &unversionedFinalImage{Line: f.Line, Image: f.Image, Resolved: f.Resolved}
}

type dockerfileVersionTagChecker struct{}

func (c *dockerfileVersionTagChecker) ID() string { return "dockerfile-version-tag" }

func (c *dockerfileVersionTagChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
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

	exempt, err := parseExemptImages(rule)
	if err != nil {
		return nil, err
	}

	files, err := loadDockerfiles(ctx, client, repo)
	if err != nil {
		return nil, err
	}
	if files.empty {
		return result(compliance.StatusSkip, "repository is empty"), nil
	}

	var violations []string
	for _, f := range files.files {
		if v := findUnversionedFinalImage(f.content, exempt); v != nil {
			violations = append(violations, describeFrom(f.path, v.Line, v.Image, v.Resolved))
		}
	}

	note := files.note()
	if len(files.files) == 0 {
		return result(compliance.StatusSkip, "no Dockerfiles found"+note), nil
	}
	if len(violations) > 0 {
		return result(compliance.StatusFail, fmt.Sprintf("%d final-stage image(s) not in name:X.Y.Z@sha256:<digest> form: %s%s", len(violations), strings.Join(violations, "; "), note)), nil
	}
	return result(compliance.StatusPass, "all final-stage images in name:X.Y.Z@sha256:<digest> form"+note), nil
}

func init() {
	compliance.Register(&dockerfileVersionTagChecker{})
}
