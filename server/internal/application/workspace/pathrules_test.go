package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Every spelling below was a way past a guard that compared paths as plain
// strings: on Windows `delete_file {"path":"..\\ws."}` deleted the whole task
// workspace, and write_file ".GIT/hooks/pre-commit" planted a hook the server
// ran on its next commit. They are judged with goos passed in, so the Windows
// and macOS rules are proven on whatever host runs the tests.
func (s *WorkspaceSuite) TestEditableSegmentsRefusesEveryAliasOfTheRootAndGit() {
	const (
		isRoot = "root"
		isOK   = "ok"
	)
	cases := []struct {
		name   string
		goos   string
		root   string
		target string
		want   string // isRoot, isOK, or the protected segment
	}{
		{"windows root itself", "windows", `C:\WS`, `C:\WS`, isRoot},
		{"windows ..\\WS case variant", "windows", `C:\WS`, `C:\ws`, isRoot},
		{"windows ..\\ws. trailing dot", "windows", `C:\WS`, `C:\ws.`, isRoot},
		{"windows trailing space", "windows", `C:\WS`, `C:\ws `, isRoot},
		{"windows all-dots segment", "windows", `C:\WS`, `C:\WS\...`, isRoot},
		{"windows .GIT", "windows", `C:\WS`, `C:\WS\.GIT\hooks\pre-commit`, ".GIT"},
		{"windows .git.", "windows", `C:\WS`, `C:\WS\.git.\config`, ".git."},
		{"windows .git trailing space", "windows", `C:\WS`, `C:\WS\.git \config`, ".git "},
		{"windows 8.3 GIT~1", "windows", `C:\WS`, `C:\WS\GIT~1\hooks\pre-commit`, "GIT~1"},
		{"windows 8.3 git~2", "windows", `C:\WS`, `C:\WS\git~2`, "git~2"},
		{"windows 8.3 hashed", "windows", `C:\WS`, `C:\WS\GI7EBA~1\config`, "GI7EBA~1"},
		{"windows NTFS index stream", "windows", `C:\WS`, `C:\WS\.git::$INDEX_ALLOCATION\hooks\x`, ".git::$INDEX_ALLOCATION"},
		{"windows any stream", "windows", `C:\WS`, `C:\WS\src\a.ts:payload`, "a.ts:payload"},
		{"windows .git under a root alias", "windows", `C:\WS`, `C:\ws.\.git\hooks\pre-commit`, ".git"},
		{"windows nested .Git", "windows", `C:\WS`, `C:\WS\sub\.Git\config`, ".Git"},
		{"windows forward slashes", "windows", `C:\WS`, `c:/ws/.git/config`, ".git"},
		{"windows ordinary file", "windows", `C:\WS`, `C:\WS\src\a.ts`, isOK},
		{"windows .github is not .git", "windows", `C:\WS`, `C:\WS\.github\workflows\ci.yml`, isOK},
		{"windows gitx~1 is not .git", "windows", `C:\WS`, `C:\WS\gitx~1`, isOK},
		{"windows filesystem root", "windows", `D:\`, `D:\.Git\config`, ".Git"},
		{"darwin .GIT", "darwin", "/Users/me/ws", "/Users/me/ws/.GIT/hooks/pre-commit", ".GIT"},
		{"darwin .Git", "darwin", "/Users/me/ws", "/Users/me/ws/.Git", ".Git"},
		{"darwin HFS-ignorable code point", "darwin", "/Users/me/ws", "/Users/me/ws/.g\u200cit/config", ".g\u200cit"},
		{"darwin root case variant", "darwin", "/Users/me/ws", "/Users/me/WS", isRoot},
		{"darwin .git. is another name", "darwin", "/Users/me/ws", "/Users/me/ws/.git.", isOK},
		{"darwin colon is a name character", "darwin", "/Users/me/ws", "/Users/me/ws/a:b", isOK},
		{"linux .git", "linux", "/ws", "/ws/.git/hooks/pre-commit", ".git"},
		{"linux root", "linux", "/ws", "/ws", isRoot},
		{"linux .GIT is another directory", "linux", "/ws", "/ws/.GIT/hooks/pre-commit", isOK},
		{"linux GIT~1 is another directory", "linux", "/ws", "/ws/GIT~1", isOK},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			segs, err := editableSegments(tc.root, tc.target, tc.goos)
			var protected *ProtectedPathError
			switch tc.want {
			case isOK:
				s.Require().NoError(err)
				s.NotEmpty(segs)
			case isRoot:
				s.ErrorIs(err, ErrWorkspaceRoot)
			default:
				s.Require().True(errors.As(err, &protected), "got %v", err)
				s.Equal(tc.want, protected.Segment)
			}
		})
	}
}

// A prefix test against root+separator turned a filesystem root into `D:\\`
// or `//`, which no path starts with, so an allowed root of D:\ or / refused
// everything.
func (s *WorkspaceSuite) TestIsWithinComparesSegmentsNotPrefixes() {
	cases := []struct {
		goos, root, path string
		want             bool
	}{
		{"windows", `D:\`, `D:\src\a.ts`, true},
		{"windows", `D:\`, `d:\src`, true},
		{"windows", `D:\`, `E:\src`, false},
		{"windows", `C:\ws`, `C:\WS\a`, true},
		{"windows", `C:\ws\`, `C:\ws\a`, true},
		{"windows", `C:\ws`, `C:\wsx\a`, false},
		{"windows", `C:\ws`, `C:\ws \a`, false},
		{"windows", `\\srv\share`, `\\srv\share\x`, true},
		{"windows", `\\srv\share`, `\\srv\other\x`, false},
		{"linux", "/", "/etc/passwd", true},
		{"linux", "/", "/", true},
		{"linux", "/ws", "/wsx/a", false},
		{"linux", "/ws", "/WS/a", false},
		{"darwin", "/ws", "/WS/a", false},
	}
	for _, tc := range cases {
		s.Equal(tc.want, isWithin(tc.path, tc.root, tc.goos), "%s: %q within %q", tc.goos, tc.path, tc.root)
	}
}

func (s *WorkspaceSuite) TestResidualUnderTakesAnAbsoluteWindowsPathRelativeToTheRoot() {
	segs, ok := residualUnder(`C:\ws`, `c:\WS\src\a.ts`, "windows", matchFold)
	s.Require().True(ok)
	s.Equal([]string{"src", "a.ts"}, segs)

	_, ok = residualUnder(`C:\ws`, `D:\ws\src`, "windows", matchFold)
	s.False(ok)
}

func (s *WorkspaceSuite) TestIndexPathForMatchesStoredRows() {
	s.Equal("src/foo.ts", indexPathFor(`src\foo.ts`, "windows"))
	s.Equal(`src\foo.ts`, indexPathFor(`src\foo.ts`, "linux"))
	s.Equal("src", indexPathFor("./src/", "linux"))
	s.Equal("", indexPathFor(".", "darwin"))
}

func (s *WorkspaceSuite) TestCanonicalDriveUpperCasesTheDriveLetterOnWindowsOnly() {
	s.Equal(`C:\Users\me\acme`, canonicalDrive(`c:\Users\me\acme`, "windows"))
	s.Equal(`\\srv\share\acme`, canonicalDrive(`\\srv\share\acme`, "windows"))
	s.Equal("c:/x", canonicalDrive("c:/x", "linux"))
}

func (s *WorkspaceSuite) TestBaseNameSplitsOnBothSeparators() {
	for in, want := range map[string]string{
		`C:\Users\me\acme`:                 "acme",
		`\\srv\share\acme\`:                "acme",
		"/data/workspaces/repos/acme-web/": "acme-web",
		"acme":                             "acme",
		`C:\`:                              "",
		"/":                                "",
		"":                                 "",
	} {
		s.Equal(want, BaseName(in), in)
	}
}

func (s *WorkspaceSuite) TestCleanDirNameForRefusesWindowsOnlyAliases() {
	s.Equal("", cleanDirNameFor(`a\b`, "linux"))
	s.Equal("", cleanDirNameFor(`C:\Users\me\acme`, "darwin"))
	s.Equal("", cleanDirNameFor("acme:stream", "windows"))
	s.Equal("", cleanDirNameFor("...", "windows"))
	s.Equal("acme:x", cleanDirNameFor("acme:x", "linux"))
	s.Equal("...", cleanDirNameFor("...", "linux"))
}

func (s *WorkspaceSuite) TestStripExtendedPrefix() {
	s.Equal(`C:\ws\a`, stripExtendedPrefix(`\\?\C:\ws\a`))
	s.Equal(`\\srv\share\a`, stripExtendedPrefix(`\\?\UNC\srv\share\a`))
	s.Equal(`C:\ws`, stripExtendedPrefix(`C:\ws`))
}

// --- on disk ---

func (s *WorkspaceSuite) newRepo() string {
	root := filepath.Join(s.tmpRoot, "repo")
	s.Require().NoError(os.MkdirAll(filepath.Join(root, ".git", "hooks"), 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(root, "src"), 0o755))
	return root
}

func (s *WorkspaceSuite) TestResolveEditableRefusesTheRootItself() {
	root := s.newRepo()
	s.Require().NoError(os.Symlink(root, filepath.Join(root, "self")))

	for _, rel := range []string{"", ".", "src/..", "self"} {
		_, err := ResolveEditableWithinRoot(root, rel)
		s.ErrorIs(err, ErrWorkspaceRoot, rel)
	}
}

// No name rule can enumerate a symlink: identity catches what spelling cannot.
func (s *WorkspaceSuite) TestResolveEditableRefusesALinkIntoGit() {
	root := s.newRepo()
	s.Require().NoError(os.Symlink(filepath.Join(root, ".git"), filepath.Join(root, "gitlink")))

	_, err := ResolveEditableWithinRoot(root, "gitlink/hooks/pre-commit")
	var protected *ProtectedPathError
	s.Require().True(errors.As(err, &protected), "got %v", err)
	s.Equal("gitlink", protected.Segment)
}

func (s *WorkspaceSuite) TestResolveEditableRefusesACaseVariantOfGitWhereTheFilesystemFoldsCase() {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		s.T().Skip(".GIT is a different directory on " + runtime.GOOS)
	}
	root := s.newRepo()

	_, err := ResolveEditableWithinRoot(root, ".GIT/hooks/pre-commit")
	var protected *ProtectedPathError
	s.True(errors.As(err, &protected), "got %v", err)
}

func (s *WorkspaceSuite) TestResolveEditableAllowsOrdinaryPaths() {
	root := s.newRepo()

	abs, err := ResolveEditableWithinRoot(root, "src/new/file.ts")
	s.Require().NoError(err)
	s.Equal(filepath.Join(root, "src", "new", "file.ts"), abs)
}

func (s *WorkspaceSuite) TestRemoveDirWithinRefusesTheRootByAnotherSpelling() {
	root := s.newRepo()
	s.Require().NoError(os.Symlink(root, filepath.Join(root, "self")))
	spellings := []string{root + string(filepath.Separator), filepath.Join(root, "self")}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		spellings = append(spellings, filepath.Join(filepath.Dir(root), strings.ToUpper(filepath.Base(root))))
	}

	for _, p := range spellings {
		s.ErrorContains(RemoveDirWithin(root, p), "workspace root", p)
		s.DirExists(filepath.Join(root, ".git"))
	}
}

func (s *WorkspaceSuite) TestIsWithinRootAcceptsAFilesystemRoot() {
	fsRoot := filepath.VolumeName(s.tmpRoot) + string(filepath.Separator)

	ok, err := IsWithinRoot(s.tmpRoot, fsRoot)
	s.Require().NoError(err)
	s.True(ok)

	resolved, err := ValidateProjectRoot(s.tmpRoot, filepath.Join(s.tmpRoot, "workspaces"), []string{fsRoot})
	s.Require().NoError(err)
	s.Equal(CanonicalPath(s.tmpRoot), resolved)
}

// A model on Windows sends `C:\ws\src\a.ts`; joining that onto C:\ws named a
// path that cannot exist. The same holds for /ws/src/a.ts anywhere.
func (s *WorkspaceSuite) TestResolveWithinRootTakesAnAbsolutePathInsideTheRootAsIs() {
	root := s.newRepo()

	resolved, err := ResolveWithinRoot(root, filepath.Join(root, "src", "a.ts"))
	s.Require().NoError(err)
	s.Equal(filepath.Join(root, "src", "a.ts"), resolved)

	resolved, err = ResolveWithinRoot(root, filepath.ToSlash(filepath.Join(root, "src")))
	s.Require().NoError(err)
	s.Equal(filepath.Join(root, "src"), resolved)
}

func (s *WorkspaceSuite) TestIndexPathStripsTheWorkspaceRoot() {
	root := s.newRepo()

	s.Equal("src/a.ts", IndexPath(root, filepath.Join(root, "src", "a.ts")))
	s.Equal("src/a.ts", IndexPath(root, "./src/a.ts"))
	s.Equal("src/a.ts", IndexPath("", "src/a.ts"))
}

func (s *WorkspaceSuite) TestResolveScopedWorkDirJoinsARelativeRequestOntoTheScope() {
	root := s.newRepo()

	got, err := ResolveScopedWorkDir("src", root)
	s.Require().NoError(err)
	s.Equal(filepath.Join(root, "src"), got)

	got, err = ResolveScopedWorkDir("./src/../src", root)
	s.Require().NoError(err)
	s.Equal(filepath.Join(root, "src"), got)

	_, err = ResolveScopedWorkDir("../elsewhere", root)
	s.Error(err)
}

// os.MkdirTemp hands out /var/... on macOS while the same directory is
// /private/var/... once resolved; either spelling is inside the scope, and the
// answer is spelled the scope's way so a later textual check agrees.
func (s *WorkspaceSuite) TestResolveScopedWorkDirAcceptsAnotherSpellingOfTheScope() {
	root := s.newRepo()
	resolvedRoot, err := filepath.EvalSymlinks(root)
	s.Require().NoError(err)

	got, err := ResolveScopedWorkDir(filepath.Join(resolvedRoot, "src"), root)
	s.Require().NoError(err)
	s.Equal(filepath.Join(root, "src"), got)

	link := filepath.Join(s.tmpRoot, "link-to-repo")
	s.Require().NoError(os.Symlink(root, link))
	got, err = ResolveScopedWorkDir(filepath.Join(link, "src"), root)
	s.Require().NoError(err)
	s.Equal(filepath.Join(root, "src"), got)

	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		got, err = ResolveScopedWorkDir(filepath.Join(filepath.Dir(root), "REPO", "src"), root)
		s.Require().NoError(err)
		s.Equal(filepath.Join(root, "src"), got)
	}
}

// A path that is not absolute on this OS was written by a host of another
// OS. Treating it as usable let an allowed root of "*" adopt it, and
// filepath.Abs grafted it onto this host's working directory or drive.
func (s *WorkspaceSuite) TestForeignOSPathIsNeverUsableAndIsReanchored() {
	foreign, name := `C:\Users\me\code\acme`, "acme"
	if runtime.GOOS == "windows" {
		foreign = "/data/workspaces/repos/acme"
	}
	wsRoot := filepath.Join(s.tmpRoot, "workspaces")

	s.False(UsableHostPath(foreign, wsRoot, []string{"*"}))

	got, reanchored := HostRootPath(foreign, wsRoot, []string{"*"})
	s.True(reanchored)
	s.Equal(filepath.Join(wsRoot, "repos", name), got)
}
