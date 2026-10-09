package executor

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	pgstore "github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/embeddedpg"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// indexStoreDir is under data_dir, never the local edition's DATA_DIR/postgres:
// the executor's cluster is its own, on its own port, and holds nothing but a
// cache that every index.ensure can rebuild.
const indexStoreDir = "index-pg"

const indexStoreMaxConns = 4

// indexStoreOpener starts the executor's own Postgres the first time the
// index is used, migrates it like the server's, and keeps it for the life of
// the process. Most runs never touch the index, so most executors never start
// it.
type indexStoreOpener struct {
	dataDir  string
	cacheDir string

	mu    sync.Mutex
	store *pgstore.LocalIndexStore
	pool  *pgxpool.Pool
	stop  func()
}

var _ port.LocalIndexStoreOpener = (*indexStoreOpener)(nil)

func newIndexStoreOpener(dataDir, cacheDir string) *indexStoreOpener {
	return &indexStoreOpener{dataDir: dataDir, cacheDir: cacheDir}
}

func (o *indexStoreOpener) Open(ctx context.Context) (port.LocalIndexStore, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.store != nil {
		return o.store, nil
	}
	base := filepath.Join(o.dataDir, indexStoreDir)
	cacheDir := o.cacheDir
	if cacheDir == "" {
		cacheDir = filepath.Join(base, "postgres-bin")
	}
	dsn, stop, err := embeddedpg.StartCluster(ctx, embeddedpg.Options{
		DataDir:    base,
		CacheDir:   cacheDir,
		RuntimeDir: filepath.Join(base, "runtime"),
	})
	if err != nil {
		return nil, err
	}
	pool, err := pgstore.NewPool(ctx, dsn, indexStoreMaxConns)
	if err != nil {
		stop()
		return nil, fmt.Errorf("connect to the index store: %w", err)
	}
	if err := database.RunMigrations(ctx, pool); err != nil {
		pool.Close()
		stop()
		return nil, fmt.Errorf("migrate the index store: %w", err)
	}
	db := pgstore.NewDB(pool)
	store := pgstore.NewLocalIndexStore(db)
	caps := pgstore.DetectVectorCapabilities(ctx, db)
	if caps.Vector {
		caps.Vector = pgstore.BootstrapWorkspaceVectors(ctx, db)
	}
	store.SetCapabilities(caps)
	o.store, o.pool, o.stop = store, pool, stop
	log.Info().Str("data_dir", base).Bool("pgvector", caps.Vector).Bool("pg_trgm", caps.Trgm).Msg("local index store started")
	return store, nil
}

func (o *indexStoreOpener) Close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.pool != nil {
		o.pool.Close()
	}
	if o.stop != nil {
		o.stop()
	}
	o.store, o.pool, o.stop = nil, nil, nil
}
