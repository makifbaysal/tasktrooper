package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// DataSourceStore persists observability data sources (migration 186) and the
// recipes a person added beside the embedded catalog.
type DataSourceStore struct {
	pool *DB

	cipherOnce sync.Once
	cipher     *secrets.Cipher
	cipherErr  error
}

func NewDataSourceStore(pool *DB) *DataSourceStore {
	return &DataSourceStore{pool: pool}
}

var (
	_ port.DataSourceStore          = (*DataSourceStore)(nil)
	_ port.ObservabilityRecipeStore = (*DataSourceStore)(nil)
)

// SetCipher injects the cipher derived at boot, before
// runtime.scrubProcessSecrets wipes MCP_SECRETS_KEY; see CloudAccountStore.
func (s *DataSourceStore) SetCipher(c *secrets.Cipher, err error) {
	s.cipherOnce.Do(func() { s.cipher, s.cipherErr = c, err })
}

func (s *DataSourceStore) getCipher() (*secrets.Cipher, error) {
	s.cipherOnce.Do(func() {
		s.cipher, s.cipherErr = secrets.NewCipherFromEnv()
	})
	return s.cipher, s.cipherErr
}

const dataSourceCols = `id, kind, name, config, secrets_set, status, status_detail, info, verified_at, created_at, updated_at`

func scanDataSource(row pgx.Row) (domain.DataSource, error) {
	var ds domain.DataSource
	var configJSON, infoJSON []byte
	if err := row.Scan(
		&ds.ID, &ds.Kind, &ds.Name, &configJSON, &ds.SecretsSet, &ds.Status, &ds.StatusDetail, &infoJSON,
		&ds.VerifiedAt, &ds.CreatedAt, &ds.UpdatedAt,
	); err != nil {
		return domain.DataSource{}, err
	}
	if err := json.Unmarshal(configJSON, &ds.Config); err != nil {
		return domain.DataSource{}, fmt.Errorf("unmarshal data source config: %w", err)
	}
	if err := json.Unmarshal(infoJSON, &ds.Info); err != nil {
		return domain.DataSource{}, fmt.Errorf("unmarshal data source info: %w", err)
	}
	return ds, nil
}

func (s *DataSourceStore) ListDataSources(ctx context.Context) ([]domain.DataSource, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+dataSourceCols+` FROM data_sources ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list data sources: %w", err)
	}
	defer rows.Close()
	out := []domain.DataSource{}
	for rows.Next() {
		ds, err := scanDataSource(rows)
		if err != nil {
			return nil, fmt.Errorf("scan data source: %w", err)
		}
		out = append(out, ds)
	}
	return out, rows.Err()
}

func (s *DataSourceStore) GetDataSource(ctx context.Context, id uuid.UUID) (domain.DataSource, error) {
	ds, err := scanDataSource(s.pool.QueryRow(ctx, `SELECT `+dataSourceCols+` FROM data_sources WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DataSource{}, fmt.Errorf("get data source: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.DataSource{}, fmt.Errorf("get data source: %w", err)
	}
	return ds, nil
}

const insertDataSourceSQL = `
INSERT INTO data_sources (id, kind, name, config, secret_enc, secrets_set, status, status_detail, info, verified_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING ` + dataSourceCols

func (s *DataSourceStore) CreateDataSource(ctx context.Context, ds domain.DataSource, secretFields map[string]string) (domain.DataSource, error) {
	if ds.ID == uuid.Nil {
		ds.ID = uuid.New()
	}
	if ds.Status == "" {
		ds.Status = domain.DataSourceUnverified
	}
	enc, names, err := s.encryptSecrets(secretFields)
	if err != nil {
		return domain.DataSource{}, fmt.Errorf("create data source: %w", err)
	}
	configJSON, infoJSON, err := marshalDataSource(ds)
	if err != nil {
		return domain.DataSource{}, fmt.Errorf("create data source: %w", err)
	}
	out, err := scanDataSource(s.pool.QueryRow(ctx, insertDataSourceSQL,
		ds.ID, ds.Kind, ds.Name, configJSON, enc, names, ds.Status, ds.StatusDetail, infoJSON, ds.VerifiedAt))
	if err != nil {
		return domain.DataSource{}, fmt.Errorf("create data source: %w", err)
	}
	return out, nil
}

// nextUpdatedAt is RecordVerification's guard token, so it must change on
// every write even when two land within one clock tick.
const nextUpdatedAt = `GREATEST(now(), updated_at + interval '1 microsecond')`

const updateDataSourceSQL = `
UPDATE data_sources SET
	kind = $2, name = $3, config = $4, status = $5, status_detail = $6, info = $7, verified_at = $8,
	updated_at = ` + nextUpdatedAt + `
WHERE id = $1
RETURNING ` + dataSourceCols

const updateDataSourceWithSecretsSQL = `
UPDATE data_sources SET
	kind = $2, name = $3, config = $4, status = $5, status_detail = $6, info = $7, verified_at = $8,
	secret_enc = $9, secrets_set = $10, updated_at = ` + nextUpdatedAt + `
WHERE id = $1
RETURNING ` + dataSourceCols

const recordVerificationSQL = `
UPDATE data_sources SET
	status = $2, status_detail = $3, info = $4, verified_at = COALESCE($5, verified_at)
WHERE id = $1 AND updated_at = $6
RETURNING ` + dataSourceCols

func (s *DataSourceStore) UpdateDataSource(ctx context.Context, ds domain.DataSource, secretFields map[string]string) (domain.DataSource, error) {
	configJSON, infoJSON, err := marshalDataSource(ds)
	if err != nil {
		return domain.DataSource{}, fmt.Errorf("update data source: %w", err)
	}
	var row pgx.Row
	if secretFields != nil {
		enc, names, err := s.encryptSecrets(secretFields)
		if err != nil {
			return domain.DataSource{}, fmt.Errorf("update data source: %w", err)
		}
		row = s.pool.QueryRow(ctx, updateDataSourceWithSecretsSQL,
			ds.ID, ds.Kind, ds.Name, configJSON, ds.Status, ds.StatusDetail, infoJSON, ds.VerifiedAt, enc, names)
	} else {
		row = s.pool.QueryRow(ctx, updateDataSourceSQL,
			ds.ID, ds.Kind, ds.Name, configJSON, ds.Status, ds.StatusDetail, infoJSON, ds.VerifiedAt)
	}
	out, err := scanDataSource(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DataSource{}, fmt.Errorf("update data source: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.DataSource{}, fmt.Errorf("update data source: %w", err)
	}
	return out, nil
}

func (s *DataSourceStore) RecordVerification(ctx context.Context, id uuid.UUID, v domain.DataSourceVerification, expectedUpdatedAt time.Time) (domain.DataSource, error) {
	infoJSON, err := json.Marshal(v.Info)
	if err != nil {
		return domain.DataSource{}, fmt.Errorf("record data source verification: marshal info: %w", err)
	}
	out, err := scanDataSource(s.pool.QueryRow(ctx, recordVerificationSQL,
		id, v.Status, v.StatusDetail, infoJSON, v.VerifiedAt, expectedUpdatedAt))
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM data_sources WHERE id = $1)`, id).Scan(&exists); err != nil {
			return domain.DataSource{}, fmt.Errorf("record data source verification: %w", err)
		}
		if !exists {
			return domain.DataSource{}, fmt.Errorf("record data source verification: %w", port.ErrNotFound)
		}
		return domain.DataSource{}, fmt.Errorf("record data source verification: %w", port.ErrStaleDataSource)
	}
	if err != nil {
		return domain.DataSource{}, fmt.Errorf("record data source verification: %w", err)
	}
	return out, nil
}

func (s *DataSourceStore) DeleteDataSource(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM data_sources WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete data source: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete data source: %w", port.ErrNotFound)
	}
	return nil
}

// DataSourceSecrets is the only path that decrypts; every other read stops at
// dataSourceCols, which never includes secret_enc. A source without secrets
// answers an empty map without needing the cipher.
func (s *DataSourceStore) DataSourceSecrets(ctx context.Context, id uuid.UUID) (map[string]string, error) {
	var enc []byte
	err := s.pool.QueryRow(ctx, `SELECT secret_enc FROM data_sources WHERE id = $1`, id).Scan(&enc)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("data source secrets: %w", port.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("data source secrets: %w", err)
	}
	if len(enc) == 0 {
		return map[string]string{}, nil
	}
	cipher, err := s.getCipher()
	if err != nil {
		return nil, err
	}
	plain, err := cipher.Decrypt(enc)
	if err != nil {
		return nil, fmt.Errorf("decrypt data source secrets: %w", err)
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(plain), &out); err != nil {
		return nil, fmt.Errorf("unmarshal data source secrets: %w", err)
	}
	return out, nil
}

func (s *DataSourceStore) ListUserRecipes(ctx context.Context) ([]domain.Recipe, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, body FROM observability_recipes ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list observability recipes: %w", err)
	}
	defer rows.Close()
	var out []domain.Recipe
	for rows.Next() {
		var id string
		var body []byte
		if err := rows.Scan(&id, &body); err != nil {
			return nil, fmt.Errorf("scan observability recipe: %w", err)
		}
		var r domain.Recipe
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, fmt.Errorf("unmarshal observability recipe %q: %w", id, err)
		}
		r.ID = id
		r.Origin = domain.RecipeOriginUser
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *DataSourceStore) encryptSecrets(fields map[string]string) ([]byte, []string, error) {
	names := make([]string, 0, len(fields))
	for k := range fields {
		names = append(names, k)
	}
	sort.Strings(names)
	if len(fields) == 0 {
		return nil, names, nil
	}
	cipher, err := s.getCipher()
	if err != nil {
		return nil, nil, err
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal secrets: %w", err)
	}
	enc, err := cipher.Encrypt(string(raw))
	if err != nil {
		return nil, nil, fmt.Errorf("encrypt secrets: %w", err)
	}
	return enc, names, nil
}

func marshalDataSource(ds domain.DataSource) (configJSON, infoJSON []byte, err error) {
	if configJSON, err = json.Marshal(nonNilMap(ds.Config)); err != nil {
		return nil, nil, fmt.Errorf("marshal config: %w", err)
	}
	if infoJSON, err = json.Marshal(ds.Info); err != nil {
		return nil, nil, fmt.Errorf("marshal info: %w", err)
	}
	return configJSON, infoJSON, nil
}

var _ port.CloudSourceLinkStore = (*DataSourceStore)(nil)

func (s *DataSourceStore) ListCloudSourceLinks(ctx context.Context) ([]domain.CloudSourceLink, error) {
	rows, err := s.pool.Query(ctx, `SELECT cloud_account_id, kind, data_source_id FROM cloud_account_data_sources ORDER BY created_at, cloud_account_id, kind`)
	if err != nil {
		return nil, fmt.Errorf("list cloud source links: %w", err)
	}
	defer rows.Close()
	out := []domain.CloudSourceLink{}
	for rows.Next() {
		var l domain.CloudSourceLink
		if err := rows.Scan(&l.CloudAccountID, &l.Kind, &l.DataSourceID); err != nil {
			return nil, fmt.Errorf("scan cloud source link: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *DataSourceStore) LinkCloudSource(ctx context.Context, accountID uuid.UUID, kind domain.DataSourceKind, dataSourceID uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO cloud_account_data_sources (cloud_account_id, kind, data_source_id) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
		accountID, kind, dataSourceID)
	if err != nil {
		return false, fmt.Errorf("link cloud source: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
