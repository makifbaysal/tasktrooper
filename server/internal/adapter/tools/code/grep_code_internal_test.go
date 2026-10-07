package code

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type GrepCodeParseSuite struct {
	suite.Suite
}

func TestGrepCodeParseSuite(t *testing.T) {
	suite.Run(t, new(GrepCodeParseSuite))
}

func (s *GrepCodeParseSuite) TestWindowsDriveColonDoesNotSplitThePath() {
	out := "C:\\repo\\src\\main.go\x0012:func main() { a := \"x:y\" }\r\n"

	got := parseRipgrepOutput(out, `C:\repo`)

	s.Require().Len(got, 1)
	s.Equal(12, got[0].Line)
	s.Equal(`func main() { a := "x:y" }`, got[0].Content)
}

func (s *GrepCodeParseSuite) TestRelativePathAndContentColons() {
	got := parseRipgrepOutput("/repo/a/b.ts\x003:const t = 'a:b:c'\n/repo/c.ts\x0010:x\n", "/repo")

	s.Equal([]grepMatch{
		{FilePath: "a/b.ts", Line: 3, Content: "const t = 'a:b:c'"},
		{FilePath: "c.ts", Line: 10, Content: "x"},
	}, got)
}

func (s *GrepCodeParseSuite) TestGlobFollowsRipgrepSemantics() {
	cases := []struct {
		glob  string
		rel   string
		match bool
	}{
		{"*.go", "a/b/c.go", true},
		{"*.go", "a/b/c.ts", false},
		{"src/*.ts", "src/a.ts", true},
		{"src/*.ts", "src/x/a.ts", false},
		{"src/**/*.ts", "src/x/y/a.ts", true},
		{"src/**/*.ts", "src/a.ts", true},
		{"**/test_*.py", "pkg/test_a.py", true},
		{"*.{ts,tsx}", "ui/App.tsx", true},
		{"*.{ts,tsx}", "ui/App.js", false},
		{"file[0-9].txt", "file7.txt", true},
		{"file[!0-9].txt", "file7.txt", false},
		{"a.b", "axb", false},
	}
	for _, tc := range cases {
		re, err := globRegexp(tc.glob, false)
		s.Require().NoError(err, tc.glob)
		s.Equal(tc.match, globMatches(re, tc.glob, tc.rel), "%s vs %s", tc.glob, tc.rel)
	}
}
