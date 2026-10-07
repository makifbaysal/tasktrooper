package indexer

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

type FileChangeSet struct {
	Added     []string
	Changed   []string
	Removed   []string
	Unchanged []string
}

func HashFile(root, relPath string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, relPath))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func DetectChangedFiles(root string, currentPaths []string, storedHashes map[string]string) FileChangeSet {
	return detectChangedFiles(root, currentPaths, storedHashes, nil)
}

// detectChangedFiles with a non-nil vouched reads no file it can classify
// without: one with no stored hash is Added, and one vouched for (its stored
// hash still describes its bytes) is Unchanged.
func detectChangedFiles(root string, currentPaths []string, storedHashes map[string]string, vouched func(rel string) bool) FileChangeSet {
	currentSet := make(map[string]struct{}, len(currentPaths))
	var result FileChangeSet

	for _, rel := range currentPaths {
		currentSet[rel] = struct{}{}
		stored, known := storedHashes[rel]
		if vouched != nil {
			if !known {
				result.Added = append(result.Added, rel)
				continue
			}
			if vouched(rel) {
				result.Unchanged = append(result.Unchanged, rel)
				continue
			}
		}
		hash, err := HashFile(root, rel)
		if err != nil {
			result.Changed = append(result.Changed, rel)
			continue
		}
		if !known {
			result.Added = append(result.Added, rel)
			continue
		}
		if stored != hash {
			result.Changed = append(result.Changed, rel)
			continue
		}
		result.Unchanged = append(result.Unchanged, rel)
	}

	for path := range storedHashes {
		if _, ok := currentSet[path]; !ok {
			result.Removed = append(result.Removed, path)
		}
	}

	return result
}

func BuildFileHashes(root string, paths []string) ([]FileHashEntry, error) {
	hashes := make([]FileHashEntry, 0, len(paths))
	for _, rel := range paths {
		hash, err := HashFile(root, rel)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, FileHashEntry{
			FilePath: rel,
			Hash:     hash,
		})
	}
	return hashes, nil
}

type FileHashEntry struct {
	FilePath string
	Hash     string
}
