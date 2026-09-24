package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// LegacyCloudSourceStore reads the pre-cloud-accounts tables read-only, for
// cloud.Service.Boot's one-time backfill: app_settings (vercel_token,
// vercel_team_id), gcloud_credentials, repository_vercel_projects,
// repository_hosting_links, repository_gcloud_resources. It never writes to
// any of them.
type LegacyCloudSourceStore struct {
	pool *DB

	cipherOnce sync.Once
	cipher     *secrets.Cipher
	cipherErr  error
}

func NewLegacyCloudSourceStore(pool *DB) *LegacyCloudSourceStore {
	return &LegacyCloudSourceStore{pool: pool}
}

var _ port.LegacyCloudSource = (*LegacyCloudSourceStore)(nil)

// SetCipher mirrors CloudAccountStore.SetCipher: it must run before
// runtime.scrubProcessSecrets wipes MCP_SECRETS_KEY from the process
// environment, because Boot's backfill is the first (and only) reader of
// these legacy secrets in a process that has adopted cloud_accounts.
func (s *LegacyCloudSourceStore) SetCipher(c *secrets.Cipher, err error) {
	s.cipherOnce.Do(func() { s.cipher, s.cipherErr = c, err })
}

func (s *LegacyCloudSourceStore) getCipher() (*secrets.Cipher, error) {
	s.cipherOnce.Do(func() {
		s.cipher, s.cipherErr = secrets.NewCipherFromEnv()
	})
	return s.cipher, s.cipherErr
}

func (s *LegacyCloudSourceStore) setting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get %s: %w", key, err)
	}
	return value, true, nil
}

// secretSetting decrypts an app_settings value stored the way
// SettingsStore.setSecret writes it: base64 over an AES-GCM ciphertext.
func (s *LegacyCloudSourceStore) secretSetting(ctx context.Context, key string) (string, bool, error) {
	value, ok, err := s.setting(ctx, key)
	if err != nil || !ok || value == "" {
		return "", false, err
	}
	cipher, err := s.getCipher()
	if err != nil {
		return "", false, err
	}
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", false, fmt.Errorf("decode %s: %w", key, err)
	}
	plain, err := cipher.Decrypt(raw)
	if err != nil {
		return "", false, fmt.Errorf("decrypt %s: %w", key, err)
	}
	return plain, true, nil
}

func (s *LegacyCloudSourceStore) LegacyVercelCredential(ctx context.Context) (domain.LegacyVercelCredential, bool, error) {
	token, ok, err := s.secretSetting(ctx, "vercel_token")
	if err != nil || !ok {
		return domain.LegacyVercelCredential{}, false, err
	}
	teamID, _, err := s.setting(ctx, "vercel_team_id")
	if err != nil {
		return domain.LegacyVercelCredential{}, false, err
	}
	return domain.LegacyVercelCredential{Token: token, TeamID: teamID}, true, nil
}

func (s *LegacyCloudSourceStore) LegacyGCloudCredential(ctx context.Context) (domain.LegacyGCloudCredential, bool, error) {
	var projectID string
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT project_id, data FROM gcloud_credentials`).Scan(&projectID, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LegacyGCloudCredential{}, false, nil
	}
	if err != nil {
		return domain.LegacyGCloudCredential{}, false, fmt.Errorf("get gcloud credential: %w", err)
	}
	cipher, err := s.getCipher()
	if err != nil {
		return domain.LegacyGCloudCredential{}, false, err
	}
	plain, err := cipher.Decrypt(data)
	if err != nil {
		return domain.LegacyGCloudCredential{}, false, fmt.Errorf("decrypt gcloud credential: %w", err)
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(plain), &fields); err != nil {
		return domain.LegacyGCloudCredential{}, false, fmt.Errorf("unmarshal gcloud credential: %w", err)
	}
	return domain.LegacyGCloudCredential{ProjectID: projectID, Fields: fields}, true, nil
}

func (s *LegacyCloudSourceStore) ListLegacyVercelProjectLinks(ctx context.Context) ([]domain.LegacyVercelProjectLink, error) {
	rows, err := s.pool.Query(ctx, `SELECT repository_id, sub_project_path, project_id, project_name, production_url FROM repository_vercel_projects`)
	if err != nil {
		return nil, fmt.Errorf("list legacy vercel project links: %w", err)
	}
	defer rows.Close()
	var out []domain.LegacyVercelProjectLink
	for rows.Next() {
		var l domain.LegacyVercelProjectLink
		if err := rows.Scan(&l.RepositoryID, &l.SubProjectPath, &l.ProjectID, &l.ProjectName, &l.ProductionURL); err != nil {
			return nil, fmt.Errorf("scan legacy vercel project link: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *LegacyCloudSourceStore) ListLegacyVercelHostingLinks(ctx context.Context) ([]domain.LegacyHostingLink, error) {
	rows, err := s.pool.Query(ctx, `SELECT repository_id, area, provider, external_id, external_name, production_url
		FROM repository_hosting_links WHERE provider = $1`, domain.DeployProviderVercel)
	if err != nil {
		return nil, fmt.Errorf("list legacy hosting links: %w", err)
	}
	defer rows.Close()
	var out []domain.LegacyHostingLink
	for rows.Next() {
		var l domain.LegacyHostingLink
		if err := rows.Scan(&l.RepositoryID, &l.Area, &l.Provider, &l.ExternalID, &l.ExternalName, &l.ProductionURL); err != nil {
			return nil, fmt.Errorf("scan legacy hosting link: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *LegacyCloudSourceStore) ListLegacyGCloudResourceBindings(ctx context.Context) ([]domain.LegacyGCloudResourceBinding, error) {
	rows, err := s.pool.Query(ctx, `SELECT repository_id, sub_project_path, resource_type, resource_name, display_name, location
		FROM repository_gcloud_resources`)
	if err != nil {
		return nil, fmt.Errorf("list legacy gcloud resource bindings: %w", err)
	}
	defer rows.Close()
	var out []domain.LegacyGCloudResourceBinding
	for rows.Next() {
		var b domain.LegacyGCloudResourceBinding
		if err := rows.Scan(&b.RepositoryID, &b.SubProjectPath, &b.ResourceType, &b.ResourceName, &b.DisplayName, &b.Location); err != nil {
			return nil, fmt.Errorf("scan legacy gcloud resource binding: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
