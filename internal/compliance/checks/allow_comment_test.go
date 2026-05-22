package checks

import "testing"

func TestHasAllowComment(t *testing.T) {
	cases := []struct {
		lines   []string
		lineIdx int
		want    bool
	}{
		// inline comment styles
		{[]string{`token = "abc" # git-cascade:allow`}, 0, true},
		{[]string{`token = "abc" // git-cascade:allow`}, 0, true},
		{[]string{`token = "abc"; -- git-cascade:allow`}, 0, true},
		{[]string{`<tag v="abc"> <!-- git-cascade:allow -->`}, 0, true},
		{[]string{`val: "abc"; /* git-cascade:allow */`}, 0, true},
		// whitespace between marker and keyword
		{[]string{`token = "abc" #   git-cascade:allow`}, 0, true},
		// comment on preceding line (multi-line constructs like PEM headers)
		{[]string{`# git-cascade:allow`, `secret_value`}, 1, true},
		{[]string{`// git-cascade:allow`, `secret_value`}, 1, true},
		// no comment — must not suppress
		{[]string{`token = "abc"`}, 0, false},
		// comment two lines above — not supported
		{[]string{`# git-cascade:allow`, ``, `secret_value`}, 2, false},
		// out-of-bounds index — must not panic
		{[]string{}, 0, false},
		{[]string{`token = "abc"`}, -1, false},
	}
	for _, tc := range cases {
		got := hasAllowComment(tc.lines, tc.lineIdx)
		if got != tc.want {
			t.Errorf("hasAllowComment(%v, %d) = %v, want %v", tc.lines, tc.lineIdx, got, tc.want)
		}
	}
}

func TestAllowCommentPatterns(t *testing.T) {
	// Verify each pattern independently against a representative sample.
	cases := []struct {
		line    string
		wantIdx int // index into allowCommentPatterns; -1 = no pattern should match
	}{
		{`AWS_KEY=AKIA... # git-cascade:allow`, 0},
		{`password = "x" // git-cascade:allow`, 1},
		{`<tag> <!-- git-cascade:allow -->`, 2},
		{`INSERT ...; -- git-cascade:allow`, 3},
		{`content: "x"; /* git-cascade:allow */`, 4},
		{`just a normal line`, -1},
		{`# some other comment`, -1},
	}
	for _, tc := range cases {
		matched := -1
		for i, re := range allowCommentPatterns {
			if re.MatchString(tc.line) {
				matched = i
				break
			}
		}
		if matched != tc.wantIdx {
			t.Errorf("line %q: matched pattern index %d, want %d", tc.line, matched, tc.wantIdx)
		}
	}
}
