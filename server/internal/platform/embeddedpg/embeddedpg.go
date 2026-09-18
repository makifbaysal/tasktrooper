// Package embeddedpg runs the Postgres this machine's single user needs, out
// of a binary bundle downloaded on first start. It is what DATABASE_URL being
// empty means.
package embeddedpg

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	embedded "github.com/fergusstrange/embedded-postgres"
	"github.com/rs/zerolog/log"
)

const (
	user     = "tasktrooper"
	password = "tasktrooper"
	dbName   = "tasktrooper"

	version = embedded.V17
)

// Start boots Postgres on a free loopback port and returns its DSN plus the
// function that stops it. dataDir is DATA_DIR: the cluster lives in
// dataDir/postgres. cacheDir holds the downloaded archive and the binaries
// extracted from it; empty means dataDir/postgres-bin.
func Start(ctx context.Context, dataDir, cacheDir string) (string, func(), error) {
	if strings.TrimSpace(dataDir) == "" {
		return "", nil, errors.New("embedded postgres needs a data directory")
	}
	pgData := filepath.Join(dataDir, "postgres")
	if strings.TrimSpace(cacheDir) == "" {
		cacheDir = filepath.Join(dataDir, "postgres-bin")
	}
	if err := os.MkdirAll(pgData, 0o700); err != nil {
		return "", nil, fmt.Errorf("create postgres data dir: %w", err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", nil, fmt.Errorf("create postgres cache dir: %w", err)
	}

	backupDir := filepath.Join(dataDir, preDropTenancyBackup)
	if port, ok := liveCluster(ctx, pgData); ok {
		if !backupSettled(pgData, backupDir) {
			// Copying the files of a running cluster would not be a backup.
			log.Warn().Str("data_dir", pgData).
				Msg("postgres is already running on this data directory, so no copy was taken before migration 133")
		}
		log.Info().Uint32("port", port).Msg("reusing the embedded postgres already running on this data directory")
		return dsn(port), func() {}, nil
	}
	// pg_ctl refuses to start while a postmaster.pid is present even when the
	// process it names died with the machine, which is how a crash turns into a
	// desktop that never starts again.
	clearStalePID(pgData)

	// The cluster is stopped here, which is the only point a file copy is a
	// consistent backup. Migration 133 cannot be undone, so an install that
	// cannot be copied does not start rather than migrate unprotected.
	fresh := !fileExists(filepath.Join(pgData, "PG_VERSION"))
	copied, err := backupClusterOnce(pgData, backupDir)
	if err != nil {
		return "", nil, fmt.Errorf("copy %s to %s before migration 133 (free some disk space and start again): %w", pgData, backupDir, err)
	}
	if copied {
		log.Info().Str("backup_dir", backupDir).Msg("copied the postgres data directory before migration 133; restore it from here to go back")
	}

	port, err := freePort()
	if err != nil {
		return "", nil, err
	}

	downloaded := binariesReady(cacheDir)
	if !downloaded {
		log.Info().Str("cache_dir", cacheDir).Str("version", string(version)).
			Msg("downloading the postgres binaries (~30 MB, first start only)")
	}

	pg := embedded.NewDatabase(embedded.DefaultConfig().
		Version(version).
		Username(user).
		Password(password).
		Database(dbName).
		Port(port).
		DataPath(pgData).
		// Separate from RuntimePath, which Start() wipes on every boot: the
		// extracted binaries belong beside the archive so a second start skips
		// both the download and the extraction.
		BinariesPath(cacheDir).
		CachePath(cacheDir).
		RuntimePath(filepath.Join(cacheDir, "runtime")).
		StartTimeout(90 * time.Second).
		Logger(logWriter{}).
		Locale("C").
		Encoding("UTF8"))

	if err := pg.Start(); err != nil {
		return "", nil, wrapStartError(err)
	}
	if fresh {
		if err := markBackupNotNeeded(backupDir); err != nil {
			log.Warn().Err(err).Msg("could not record that this new cluster needs no pre-133 copy")
		}
	}
	if !downloaded {
		log.Info().Str("cache_dir", cacheDir).Msg("postgres binaries ready")
	}
	log.Info().Uint32("port", port).Str("data_dir", pgData).Msg("embedded postgres started")

	return dsn(port), func() {
		if err := pg.Stop(); err != nil {
			log.Error().Err(err).Msg("embedded postgres stop failed")
		}
	}, nil
}

// wrapStartError diagnoses the OS-level failure this library reports as
// nothing more than "unable to init/start postgres" text. On Windows the
// pg_ctl/initdb binaries downloaded into the cache directory are unsigned, so
// the OS itself can refuse to run them (a Defender/antivirus quarantine or a
// missing VC++ runtime), which surfaces here as an *exec.Error or
// *fs.PathError. This does not fix or work around the failure; it only makes
// the cause distinguishable in a shared log.
func wrapStartError(err error) error {
	var execErr *exec.Error
	var pathErr *fs.PathError
	if errors.As(err, &execErr) || errors.As(err, &pathErr) {
		return fmt.Errorf("start embedded postgres: %w (the OS could not launch the postgres binary; on Windows this usually means it was blocked by antivirus/Defender or a required system runtime library is missing)", err)
	}
	return fmt.Errorf("start embedded postgres: %w", err)
}

func dsn(port uint32) string {
	return fmt.Sprintf("postgres://%s:%s@127.0.0.1:%d/%s?sslmode=disable", user, password, port, dbName)
}

func freePort() (uint32, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate postgres port: %w", err)
	}
	defer l.Close()
	return uint32(l.Addr().(*net.TCPAddr).Port), nil
}

func binariesReady(cacheDir string) bool {
	if _, err := os.Stat(filepath.Join(cacheDir, "bin", "postgres.exe")); err == nil {
		return true
	}
	_, err := os.Stat(filepath.Join(cacheDir, "bin", "postgres"))
	return err == nil
}

// liveCluster reports the port of a postmaster still serving this data
// directory, which is what a restart after a hard kill of the parent finds.
func liveCluster(ctx context.Context, pgData string) (uint32, bool) {
	pid, port, ok := readPostmasterPID(pgData)
	if !ok || !processAlive(pid) {
		return 0, false
	}
	dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return 0, false
	}
	_ = conn.Close()
	return port, true
}

func clearStalePID(pgData string) {
	pidFile := filepath.Join(pgData, "postmaster.pid")
	pid, _, ok := readPostmasterPID(pgData)
	if !ok {
		_ = os.Remove(pidFile)
		return
	}
	if processAlive(pid) {
		return
	}
	log.Warn().Int("pid", pid).Msg("removing a stale postmaster.pid left by a previous crash")
	_ = os.Remove(pidFile)
}

func readPostmasterPID(pgData string) (pid int, port uint32, ok bool) {
	raw, err := os.ReadFile(filepath.Join(pgData, "postmaster.pid"))
	if err != nil {
		return 0, 0, false
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) < 4 {
		return 0, 0, false
	}
	pid, err = strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return 0, 0, false
	}
	p, err := strconv.ParseUint(strings.TrimSpace(lines[3]), 10, 32)
	if err != nil {
		return 0, 0, false
	}
	return pid, uint32(p), true
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// logWriter carries the cluster's own stdout/stderr into the process log
// instead of the terminal, where it would interleave with the LISTENING line
// the desktop parses.
type logWriter struct{}

func (logWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			log.Debug().Str("source", "postgres").Msg(line)
		}
	}
	return len(p), nil
}
