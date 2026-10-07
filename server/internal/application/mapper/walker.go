package mapper

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var defaultIgnoreDirs = map[string]struct{}{
	".git":         {},
	".svn":         {},
	".hg":          {},
	"node_modules": {},
	"vendor":       {},
	"__pycache__":  {},
	".venv":        {},
	"venv":         {},
	"dist":         {},
	"build":        {},
	".next":        {},
	".turbo":       {},
	"target":       {},
	".idea":        {},
	".vscode":      {},
}

type WalkOptions struct {
	UseGitignore bool
	// ExtraIgnoreFiles are root-relative files in .gitignore syntax whose
	// patterns apply on top of .gitignore (or alone when UseGitignore is off).
	ExtraIgnoreFiles []string
	// Subdir, root-relative, limits the walk to that directory; returned paths
	// stay root-relative and the root's ignore rules still apply, so the
	// result is the full walk filtered to the prefix without reading the rest.
	Subdir string
}

func Walk(root string, opts WalkOptions) ([]string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve walk root: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("stat walk root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("walk root is not a directory")
	}

	var gitignore []string
	if opts.UseGitignore {
		gitignore, err = loadGitignore(absRoot)
		if err != nil {
			return nil, err
		}
	}

	for _, name := range opts.ExtraIgnoreFiles {
		extra, loadErr := loadIgnoreFile(absRoot, name)
		if loadErr != nil {
			return nil, loadErr
		}
		gitignore = append(gitignore, extra...)
	}

	start := absRoot
	if sub := walkSubdir(opts.Subdir); sub != "" {
		if sub == ".." || strings.HasPrefix(sub, "../") || subdirIgnored(sub, gitignore) {
			return nil, nil
		}
		start = filepath.Join(absRoot, filepath.FromSlash(sub))
		info, statErr := os.Lstat(start)
		if os.IsNotExist(statErr) {
			return nil, nil
		}
		if statErr != nil {
			return nil, fmt.Errorf("stat walk subdir: %w", statErr)
		}
		direct, linkErr := reachedWithoutSymlink(absRoot, filepath.Dir(start), path.Dir(sub))
		if linkErr != nil {
			return nil, fmt.Errorf("resolve walk subdir: %w", linkErr)
		}
		if !direct {
			return nil, nil
		}
		// A symlink at the prefix itself is listed, as the full walk lists it,
		// and never followed.
		if !info.IsDir() {
			return []string{sub}, nil
		}
	}

	var paths []string
	err = filepath.WalkDir(start, func(fullPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fullPath == absRoot || fullPath == start {
			return nil
		}

		rel, err := filepath.Rel(absRoot, fullPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if entry.IsDir() {
			base := entry.Name()
			if _, ok := defaultIgnoreDirs[base]; ok {
				return filepath.SkipDir
			}
			if shouldIgnore(rel, gitignore) {
				return filepath.SkipDir
			}
			return nil
		}

		if shouldIgnore(rel, gitignore) {
			return nil
		}

		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk root: %w", err)
	}
	return paths, nil
}

func GitignorePatterns(root string) ([]string, error) {
	return loadGitignore(root)
}

func loadGitignore(root string) ([]string, error) {
	return loadIgnoreFile(root, ".gitignore")
}

func loadIgnoreFile(root, name string) ([]string, error) {
	path := filepath.Join(root, name)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "!") {
			continue
		}
		line = strings.TrimSuffix(line, "/")
		patterns = append(patterns, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	return patterns, nil
}

func shouldIgnore(relPath string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchGitignorePattern(relPath, pattern) {
			return true
		}
	}
	return false
}

func matchGitignorePattern(relPath, pattern string) bool {
	if pattern == "" {
		return false
	}
	if strings.HasPrefix(pattern, "/") {
		pattern = strings.TrimPrefix(pattern, "/")
		if relPath == pattern {
			return true
		}
		return strings.HasPrefix(relPath, pattern+"/")
	}
	if strings.HasPrefix(pattern, "*") {
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(relPath, suffix)
	}
	if relPath == pattern {
		return true
	}
	if strings.HasPrefix(relPath, pattern+"/") {
		return true
	}
	parts := strings.Split(relPath, "/")
	for _, part := range parts {
		if part == pattern {
			return true
		}
	}
	return false
}

func walkSubdir(sub string) string {
	cleaned := path.Clean(filepath.ToSlash(strings.TrimSpace(sub)))
	if cleaned == "." {
		return ""
	}
	return strings.Trim(cleaned, "/")
}

// reachedWithoutSymlink reports whether dir, which is relDir under absRoot,
// resolves to that same place under the resolved root. A full walk never
// follows a symlink, so a prefix through one lists nothing — and one that
// points out of the repository must never list what is behind it.
func reachedWithoutSymlink(absRoot, dir, relDir string) (bool, error) {
	if relDir == "." {
		return true, nil
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return false, err
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return realDir == filepath.Join(realRoot, filepath.FromSlash(relDir)), nil
}

// subdirIgnored: a full walk never enters an ignored directory, so a prefix
// inside one lists nothing rather than whatever it happens to contain.
func subdirIgnored(sub string, gitignore []string) bool {
	parts := strings.Split(sub, "/")
	for i, part := range parts {
		if _, ok := defaultIgnoreDirs[part]; ok {
			return true
		}
		if shouldIgnore(strings.Join(parts[:i+1], "/"), gitignore) {
			return true
		}
	}
	return false
}
