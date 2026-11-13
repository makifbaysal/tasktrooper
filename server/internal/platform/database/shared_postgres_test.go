package database_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// One cluster per test binary. Every migration suite used to start its own —
// an initdb, a server start and the whole migration history, about ten
// seconds each, a dozen times over — only to then create an empty database of
// its own and migrate that up to the version it tests. The suites already name
// and drop their databases themselves, so they share the server safely; they
// run one after another, never in parallel.
var sharedPG struct {
	once    sync.Once
	dir     string
	cluster *database.Embedded
	err     error
}

func sharedPostgres(t testing.TB) *database.Embedded {
	t.Helper()
	sharedPG.once.Do(func() {
		sharedPG.dir, sharedPG.err = os.MkdirTemp("", "tasktrooper-pg-")
		if sharedPG.err != nil {
			return
		}
		sharedPG.cluster, sharedPG.err = database.StartEmbedded(context.Background(), database.EmbeddedConfig{
			DataDir: filepath.Join(sharedPG.dir, "postgres"),
			// Its own path, not the library's shared cache: two test binaries
			// extracting into the same cache race each other in `go test ./...`.
			RuntimePath: filepath.Join(sharedPG.dir, "runtime"),
		})
	})
	if sharedPG.err != nil {
		t.Fatalf("start shared embedded postgres: %v", sharedPG.err)
	}
	return sharedPG.cluster
}

func TestMain(m *testing.M) {
	code := m.Run()
	if sharedPG.cluster != nil {
		_ = sharedPG.cluster.Stop()
	}
	if sharedPG.dir != "" {
		_ = os.RemoveAll(sharedPG.dir)
	}
	os.Exit(code)
}
