//go:build linux

package proctree

import (
	"os"
	"strconv"
	"strings"
)

func pidsWithCwdUnder(root string) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		cwd, err := os.Readlink("/proc/" + e.Name() + "/cwd")
		if err != nil || strings.HasSuffix(cwd, " (deleted)") {
			continue
		}
		if isWithin(root, cwd) {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}
