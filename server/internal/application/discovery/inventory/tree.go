// Package inventory is the one file listing every discovery detector reads,
// so a working copy is walked once and every detector sees the same tree.
package inventory

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"time"
)

const (
	MaxFiles     = 40000
	maxReadBytes = 1 << 20
	gitTimeout   = 15 * time.Second
)

// Never listed even when git reports them: a submodule or a nested checkout
// of build output would otherwise dominate every detector.
var skipDirs = map[string]bool{
	".git": true, ".claude": true, "worktrees": true,
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	".next": true, ".nuxt": true, "out": true, "target": true, "Pods": true,
	".venv": true, "venv": true, "__pycache__": true, ".terraform": true,
	"coverage": true, ".idea": true, ".vscode": true, ".gradle": true,
	"DerivedData": true, ".dart_tool": true, ".svelte-kit": true, ".turbo": true,
	".pytest_cache": true, ".mypy_cache": true, "bin": true, "obj": true,
}

var ErrTooLarge = errors.New("file exceeds the discovery read limit")

type Tree struct {
	// Root is the absolute working-copy path; "" for an in-memory tree.
	Root      string
	FS        fs.FS
	Files     []string
	Truncated bool
	ViaGit    bool

	byBase map[string][]string
	set    map[string]bool
}

// Load lists a working copy through git when it is one — git applies the
// repository's own .gitignore, which no skip list can match — and falls back
// to a filesystem walk otherwise.
func Load(ctx context.Context, root string) (*Tree, error) {
	st, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, errors.New("working copy is not a directory: " + root)
	}
	fsys := os.DirFS(root)
	files, viaGit := gitFiles(ctx, root)
	if !viaGit {
		files = walk(ctx, fsys)
	}
	return build(root, fsys, files, viaGit), nil
}

// FromFS lists an in-memory tree; tests build fixtures with fstest.MapFS.
func FromFS(fsys fs.FS) *Tree {
	return build("", fsys, walk(context.Background(), fsys), false)
}

func build(root string, fsys fs.FS, files []string, viaGit bool) *Tree {
	sort.Strings(files)
	t := &Tree{Root: root, FS: fsys, ViaGit: viaGit, byBase: map[string][]string{}, set: map[string]bool{}}
	for _, f := range files {
		if len(t.Files) >= MaxFiles {
			t.Truncated = true
			break
		}
		t.Files = append(t.Files, f)
		t.set[f] = true
		base := path.Base(f)
		t.byBase[base] = append(t.byBase[base], f)
	}
	return t
}

func gitFiles(ctx context.Context, root string) ([]string, bool) {
	cctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	var files []string
	for _, p := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		p = strings.TrimSpace(p)
		if p == "" || skipped(p) {
			continue
		}
		if _, err := os.Stat(root + "/" + p); err != nil {
			continue
		}
		files = append(files, p)
	}
	return files, len(files) > 0
}

func walk(ctx context.Context, fsys fs.FS) []string {
	var files []string
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable subtree costs that subtree, not the walk
		}
		if ctx.Err() != nil {
			return fs.SkipAll
		}
		if p == "." {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if len(files) > MaxFiles {
			return fs.SkipAll
		}
		files = append(files, p)
		return nil
	})
	return files
}

func skipped(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if skipDirs[part] {
			return true
		}
	}
	return false
}

func (t *Tree) Has(p string) bool { return t.set[clean(p)] }

// ByBase returns every listed path whose base name is name.
func (t *Tree) ByBase(name string) []string { return t.byBase[name] }

func (t *Tree) WithSuffix(suffix string) []string {
	var out []string
	for _, f := range t.Files {
		if strings.HasSuffix(f, suffix) {
			out = append(out, f)
		}
	}
	return out
}

// Under returns the files inside dir ("." = everything).
func (t *Tree) Under(dir string) []string {
	dir = clean(dir)
	if dir == "." {
		return t.Files
	}
	prefix := dir + "/"
	i := sort.SearchStrings(t.Files, prefix)
	var out []string
	for ; i < len(t.Files) && strings.HasPrefix(t.Files[i], prefix); i++ {
		out = append(out, t.Files[i])
	}
	return out
}

// HasDir reports whether any listed file lives under dir.
func (t *Tree) HasDir(dir string) bool {
	dir = clean(dir)
	if dir == "." {
		return len(t.Files) > 0
	}
	prefix := dir + "/"
	i := sort.SearchStrings(t.Files, prefix)
	return i < len(t.Files) && strings.HasPrefix(t.Files[i], prefix)
}

// Read returns a listed file's content, refusing anything over 1 MiB so a
// committed dataset cannot stall a scan.
func (t *Tree) Read(p string) ([]byte, error) {
	p = clean(p)
	f, err := t.FS.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxReadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxReadBytes {
		return nil, ErrTooLarge
	}
	return data, nil
}

func (t *Tree) ReadString(p string) string {
	data, err := t.Read(p)
	if err != nil {
		return ""
	}
	return string(data)
}

// Size is the byte size of a listed file, 0 when unreadable.
func (t *Tree) Size(p string) int64 {
	info, err := fs.Stat(t.FS, clean(p))
	if err != nil {
		return 0
	}
	return info.Size()
}

// Dir is path.Dir with "." for top-level files.
func Dir(p string) string { return path.Dir(clean(p)) }

// Join joins a component directory and a relative path the way the tree
// stores them.
func Join(dir, rel string) string {
	if dir == "" || dir == "." {
		return clean(rel)
	}
	return clean(dir + "/" + rel)
}

func clean(p string) string {
	p = strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(p, "\\", "/")), "/")
	if p == "" {
		return "."
	}
	return p
}
