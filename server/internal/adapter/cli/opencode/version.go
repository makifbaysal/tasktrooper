package opencode

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
)

const versionTimeout = 15 * time.Second

var semverPattern = regexp.MustCompile(`(\d+)\.\d+\.\d+`)

// majorVersion reads the major from `--version` output: 1.x prints the bare
// version, 2.x prints "opencode v2.0.13".
func majorVersion(out string) (int, bool) {
	m := semverPattern.FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	major, err := strconv.Atoi(m[1])
	return major, err == nil
}

// generation remembers which opencode the binary is, keyed on the file's
// identity so an in-place upgrade from 1.x to 2.x is noticed on the next run
// without a server restart.
type generation struct {
	bin string

	mu    sync.Mutex
	key   string
	major int
}

func (g *generation) serverBased(ctx context.Context) bool {
	return g.current(ctx) >= 2
}

func (g *generation) current(ctx context.Context) int {
	key := fileKey(g.bin)
	g.mu.Lock()
	defer g.mu.Unlock()
	if key != "" && key == g.key {
		return g.major
	}
	major, err := readMajor(ctx, g.bin)
	if err != nil {
		log.Warn().Err(err).Str("binary", g.bin).
			Msg("could not read the opencode version; running it the 1.x way")
		major = 1
	}
	g.key, g.major = key, major
	if major >= 2 {
		log.Info().Str("binary", g.bin).Int("major", major).
			Msg("opencode 2.x: each session runs on a private opencode server so it gets the tasktrooper tools")
	}
	return major
}

func fileKey(bin string) string {
	info, err := os.Stat(bin)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
}

func readMajor(ctx context.Context, bin string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--version")
	cmd.Env = core.ProbeEnv()
	cmd.Dir = os.TempDir()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("%s --version: %w%s", bin, err, core.Tail(buf.String()))
	}
	major, ok := majorVersion(buf.String())
	if !ok {
		return 0, fmt.Errorf("%s --version printed no version%s", bin, core.Tail(buf.String()))
	}
	return major, nil
}
