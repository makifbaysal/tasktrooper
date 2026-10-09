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
	"runtime"
	"strconv"
	"strings"
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
	return StartCluster(ctx, Options{DataDir: dataDir, CacheDir: cacheDir})
}

// Options place one cluster. RuntimeDir is where the library writes its
// scratch files (the initdb password file) and wipes on every start; empty
// means CacheDir/runtime. A second cluster sharing CacheDir with a running one
// names its own, so its start does not clear the other's scratch mid-initdb.
type Options struct {
	DataDir    string
	CacheDir   string
	RuntimeDir string
}

func StartCluster(ctx context.Context, opts Options) (string, func(), error) {
	dataDir, cacheDir := opts.DataDir, opts.CacheDir
	if strings.TrimSpace(dataDir) == "" {
		return "", nil, errors.New("embedded postgres needs a data directory")
	}
	pgData := filepath.Join(dataDir, "postgres")
	if strings.TrimSpace(cacheDir) == "" {
		cacheDir = filepath.Join(dataDir, "postgres-bin")
	}
	runtimeDir := opts.RuntimeDir
	if strings.TrimSpace(runtimeDir) == "" {
		runtimeDir = filepath.Join(cacheDir, "runtime")
	}
	if err := os.MkdirAll(pgData, 0o700); err != nil {
		return "", nil, fmt.Errorf("create postgres data dir: %w", err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", nil, fmt.Errorf("create postgres cache dir: %w", err)
	}

	backupDir := filepath.Join(dataDir, preDropTenancyBackup)
	if port, ok := liveCluster(ctx, pgData); ok {
		// Copying the files of a running cluster would not be a backup.
		if !backupSettled(pgData, backupDir) {
			log.Warn().Str("data_dir", pgData).
				Msg("postgres is already running on this data directory, so no copy was taken before migration 133")
		}
		log.Info().Uint32("port", port).Msg("reusing the embedded postgres already running on this data directory")
		return dsn(port), func() { stopAdopted(cacheDir, pgData, runPgCtl) }, nil
	}
	// A dead postmaster.pid blocks pg_ctl, which is how a crash turns into a
	// desktop that never starts again.
	clearStalePID(pgData)

	// Migration 133 cannot be undone, so a cluster that cannot be copied does
	// not start rather than migrate unprotected. The copy is only consistent
	// while the cluster is stopped, which it is here.
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
	} else if err := markExtracted(cacheDir); err != nil {
		log.Warn().Err(err).Str("cache_dir", cacheDir).Msg("could not mark the postgres binaries as extracted; they will be extracted again")
	}

	pg := embedded.NewDatabase(clusterConfig(port, pgData, cacheDir, runtimeDir))

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

func clusterConfig(port uint32, pgData, cacheDir, runtimeDir string) embedded.Config {
	return embedded.DefaultConfig().
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
		RuntimePath(runtimeDir).
		StartTimeout(90 * time.Second).
		Logger(logWriter{}).
		Locale("C").
		Encoding("UTF8").
		StartParameters(startParameters())
}

// startParameters tune a cluster that sits idle on a laptop most of the day:
// its background workers (autovacuum launcher, checkpointer, WAL writer,
// background writer) otherwise wake every 200ms to every few minutes with
// nothing to do. They are server start options, so a cluster initialised
// on stock settings picks them up on its next start. Durability is untouched:
// fsync and synchronous_commit keep their defaults, and max_connections stays
// at the stock 100, well above the server's pool.
func startParameters() map[string]string {
	return map[string]string{
		"autovacuum_naptime":    "10min",
		"checkpoint_timeout":    "30min",
		"wal_writer_delay":      "1s",
		"bgwriter_delay":        "10s",
		"bgwriter_lru_maxpages": "0",
		"log_checkpoints":       "off",
		"jit":                   "off",
	}
}

// wrapStartError surfaces exe/initdb failures the OS itself caused (unsigned
// binaries quarantined by antivirus, a missing VC++ runtime). It does not fix
// them, only makes the cause distinguishable in the log.
func wrapStartError(err error) error {
	var execErr *exec.Error
	var pathErr *fs.PathError
	if errors.As(err, &execErr) || errors.As(err, &pathErr) {
		return fmt.Errorf("start embedded postgres: %w (the OS could not launch the postgres binary; on Windows this usually means it was blocked by antivirus/Defender or a required system runtime library is missing)", err)
	}
	return fmt.Errorf("start embedded postgres: %w", err)
}

// pgCtlStopTimeout is pg_ctl's own -t wait; the context bound sits above it.
const pgCtlStopTimeout = 30 * time.Second

func pgCtlPath(cacheDir string) string {
	name := "pg_ctl"
	if runtime.GOOS == "windows" {
		name = "pg_ctl.exe"
	}
	return filepath.Join(cacheDir, "bin", name)
}

func runPgCtl(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// stopAdopted stops a cluster this process did not start. pg.Stop() is not an
// option for it: the library only stops what it launched, and without this a
// cluster adopted after a hard kill would outlive every later server.
func stopAdopted(cacheDir, pgData string, run func(ctx context.Context, name string, args ...string) ([]byte, error)) {
	ctx, cancel := context.WithTimeout(context.Background(), pgCtlStopTimeout+10*time.Second)
	defer cancel()
	out, err := run(ctx, pgCtlPath(cacheDir), "stop", "-D", pgData, "-m", "fast", "-w", "-t", strconv.Itoa(int(pgCtlStopTimeout/time.Second)))
	if err != nil {
		log.Error().Err(err).Str("output", strings.TrimSpace(string(out))).Msg("stopping the adopted embedded postgres failed")
		return
	}
	log.Info().Msg("stopped the adopted embedded postgres")
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

// markExtracted gives Windows the bin/pg_ctl the library stats to decide
// whether BinariesPath still needs extracting. Only unix has a file by that
// name, so on Windows it extracts again on every start, and replacing the
// .exe files fails while any postgres.exe from this directory still runs. The
// empty stand-in is never what runs: exec resolves the extension-less path
// through PATHEXT to pg_ctl.exe.
func markExtracted(cacheDir string) error {
	marker := filepath.Join(cacheDir, "bin", "pg_ctl")
	if fileExists(marker) || !fileExists(marker+".exe") {
		return nil
	}
	return os.WriteFile(marker, nil, 0o644)
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

// logWriter routes the cluster's own output into the process log instead of
// the terminal, where it would interleave with the LISTENING line the desktop
// parses.
type logWriter struct{}

func (logWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			log.Debug().Str("source", "postgres").Msg(line)
		}
	}
	return len(p), nil
}
