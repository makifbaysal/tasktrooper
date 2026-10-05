package indexer

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
)

var indexableExtensions = map[string]struct{}{
	".go":    {},
	".ts":    {},
	".tsx":   {},
	".mts":   {},
	".cts":   {},
	".js":    {},
	".jsx":   {},
	".mjs":   {},
	".cjs":   {},
	".py":    {},
	".java":  {},
	".kt":    {},
	".kts":   {},
	".swift": {},
}

const IgnoreFileName = ".tasktrooperignore"

type IndexWalkOptions struct {
	IncludeGenerated bool
}

func WalkIndexableFiles(root string, opts IndexWalkOptions) ([]string, error) {
	paths, err := mapper.Walk(root, mapper.WalkOptions{
		UseGitignore:     true,
		ExtraIgnoreFiles: []string{IgnoreFileName},
	})
	if err != nil {
		return nil, err
	}
	filtered := make([]string, 0, len(paths))
	for _, rel := range paths {
		if !isIndexableExt(rel) {
			continue
		}
		if !opts.IncludeGenerated && (IsGeneratedOrMockPath(rel) || hasGeneratedHead(filepath.Join(root, rel))) {
			continue
		}
		filtered = append(filtered, rel)
	}
	return filtered, nil
}

func hasGeneratedHead(full string) bool {
	f, err := os.Open(full)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, generatedHeadBytes)
	n, _ := io.ReadFull(f, buf)
	return HasGeneratedMarker(buf[:n])
}

func isIndexableExt(path string) bool {
	_, ok := indexableExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

// IsIndexablePath has no config access, so it always treats generated code and
// mocks as not indexable even when indexer.index_generated is on.
func IsIndexablePath(path string) bool {
	return isIndexableExt(path) && !IsGeneratedOrMockPath(path)
}
