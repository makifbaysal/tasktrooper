//go:build darwin

package proctree

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"time"
)

func pidsWithCwdUnder(root string) ([]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "lsof", "-a", "-d", "cwd", "-Fpn").Output()
	if err != nil {
		// lsof exits 1 when some processes are inaccessible yet still lists
		// the rest.
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 || len(out) == 0 {
			return nil, err
		}
	}
	var pids []int
	current := 0
	for _, line := range bytes.Split(out, []byte("\n")) {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			current, _ = strconv.Atoi(string(line[1:]))
		case 'n':
			if current > 0 && isWithin(root, string(line[1:])) {
				pids = append(pids, current)
			}
		}
	}
	return pids, nil
}
