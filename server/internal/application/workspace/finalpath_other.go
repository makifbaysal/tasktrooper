//go:build !windows

package workspace

import "path/filepath"

func evalExisting(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
