package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
)

func TestTelemetrySettingsKeepsTheKeyEncryptedAndHonoursNilAndEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pg, err := newTestDatabase(ctx)
	require.NoError(t, err)
	defer pg.Stop()
	pool, err := pgxpool.New(ctx, pg.DSN())
	require.NoError(t, err)
	defer pool.Close()

	cipher, err := secrets.NewCipher(make([]byte, 32))
	require.NoError(t, err)
	store := postgres.NewSettingsStore(postgres.NewDB(pool), "./data/workspaces", "en", false)
	store.SetCipher(cipher, nil)

	u, k, err := store.TelemetrySettings(ctx)
	require.NoError(t, err)
	require.Empty(t, u)
	require.Empty(t, k)

	key := "tt_live_key"
	require.NoError(t, store.SetTelemetrySettings(ctx, "https://telemetry.example", &key))
	u, k, err = store.TelemetrySettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "https://telemetry.example", u)
	require.Equal(t, key, k)

	var stored string
	require.NoError(t, pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = 'telemetry_api_key'`).Scan(&stored))
	require.NotContains(t, stored, key, "the key is encrypted at rest")

	require.NoError(t, store.SetTelemetrySettings(ctx, "https://other.example", nil))
	u, k, _ = store.TelemetrySettings(ctx)
	require.Equal(t, "https://other.example", u)
	require.Equal(t, key, k, "a nil key keeps the stored one")

	empty := ""
	require.NoError(t, store.SetTelemetrySettings(ctx, "https://other.example", &empty))
	_, k, _ = store.TelemetrySettings(ctx)
	require.Empty(t, k, "an empty key clears")

	require.NoError(t, store.SetTelemetrySettings(ctx, "", nil))
	u, _, _ = store.TelemetrySettings(ctx)
	require.Empty(t, u)
}
