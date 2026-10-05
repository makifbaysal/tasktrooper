package mapper

import (
	"bufio"
	"fmt"
	"os"
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

	var paths []string
	err = filepath.WalkDir(absRoot, func(fullPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fullPath == absRoot {
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
