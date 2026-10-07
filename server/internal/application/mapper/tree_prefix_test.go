package mapper

import "path/filepath"

func (s *TreeSuite) TestExpandTreeAcceptsAnyPrefixSpelling() {
	sep := string(filepath.Separator)
	for _, prefix := range []string{"pkg", "pkg/", "./pkg", "." + sep + "pkg" + sep, "web/../pkg"} {
		out := ExpandTree(prefix, s.paths, 4)
		s.Contains(out, "main.go", prefix)
		s.NotContains(out, "app.ts", prefix)
	}
}
