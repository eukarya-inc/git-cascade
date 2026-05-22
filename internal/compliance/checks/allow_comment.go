package checks

import "regexp"

// allowCommentPatterns matches inline suppression comments supported across common
// languages and file formats:
//   - #  git-cascade:allow  (shell, Python, Ruby, YAML, .env, HCL, Dockerfile)
//   - // git-cascade:allow  (Go, JS/TS, Java, Rust, Swift, Kotlin, PHP, C/C++)
//   - -- git-cascade:allow  (SQL, Lua, Haskell)
//   - <!-- git-cascade:allow (HTML, XML)
//   - /* git-cascade:allow  (CSS, C block comment start)
var allowCommentPatterns = []*regexp.Regexp{
	regexp.MustCompile(`#\s*git-cascade:allow`),
	regexp.MustCompile(`//\s*git-cascade:allow`),
	regexp.MustCompile(`<!--\s*git-cascade:allow`), // must precede -- to avoid partial match
	regexp.MustCompile(`--\s*git-cascade:allow`),
	regexp.MustCompile(`/\*\s*git-cascade:allow`),
}

// hasAllowComment reports whether the line at lineIdx (0-based) in lines, or
// the line immediately above it, contains a git-cascade:allow suppression comment.
// The line-above form supports multi-line constructs like PEM headers where the
// secret content and the comment cannot share the same line.
func hasAllowComment(lines []string, lineIdx int) bool {
	check := func(line string) bool {
		for _, re := range allowCommentPatterns {
			if re.MatchString(line) {
				return true
			}
		}
		return false
	}
	if lineIdx >= 0 && lineIdx < len(lines) && check(lines[lineIdx]) {
		return true
	}
	if lineIdx-1 >= 0 && lineIdx-1 < len(lines) && check(lines[lineIdx-1]) {
		return true
	}
	return false
}
