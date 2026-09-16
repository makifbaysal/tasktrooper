package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
)

type SettingsStore struct {
	pool              *DB
	defaultRoot       string
	defaultLang       string
	lockWorkspaceRoot bool

	cipherOnce sync.Once
	cipher     *secrets.Cipher
	cipherErr  error
}

func NewSettingsStore(pool *DB, defaultRoot, defaultLang string, lockWorkspaceRoot bool) *SettingsStore {
	if defaultRoot == "" {
		defaultRoot = "./data/workspaces"
	}
	if defaultLang == "" {
		defaultLang = "en"
	}
	return &SettingsStore{
		pool:              pool,
		defaultRoot:       defaultRoot,
		defaultLang:       defaultLang,
		lockWorkspaceRoot: lockWorkspaceRoot,
	}
}

// The public catalog, until the user points it at a repository of their own:
// the search_boilerplate_catalog tool reads whatever is set here on every call.
const defaultBoilerplateCatalogRepo = "github.com/makifbaysal/boilerplates"

func (s *SettingsStore) Get(ctx context.Context) (domain.AppSettings, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, value FROM app_settings`)
	if err != nil {
		return domain.AppSettings{}, fmt.Errorf("get settings: %w", err)
	}
	defer rows.Close()

	out := domain.AppSettings{
		WorkspaceRoot:          s.defaultRoot,
		DefaultLanguage:        s.defaultLang,
		BoilerplateCatalogRepo: defaultBoilerplateCatalogRepo,
		AnalizAssigneeBackend:  domain.AgentSystemArchitect,
		AnalizAssigneeFrontend: domain.AgentSystemArchitect,
		AnalizAssigneeMobile:   domain.AgentSystemArchitect,
	}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return domain.AppSettings{}, err
		}
		switch key {
		case "workspace_root":
			if !s.lockWorkspaceRoot && value != "" {
				out.WorkspaceRoot = value
			}
		case "default_language":
			if value != "" {
				out.DefaultLanguage = value
			}
		case "pipeline_container_runtime":
			out.PipelineContainerRuntime = value
		case "boilerplate_catalog_repo":
			if value != "" {
				out.BoilerplateCatalogRepo = value
			}
		case "analiz_assignee_backend":
			if value != "" {
				out.AnalizAssigneeBackend = value
			}
		case "analiz_assignee_frontend":
			if value != "" {
				out.AnalizAssigneeFrontend = value
			}
		case "analiz_assignee_mobile":
			if value != "" {
				out.AnalizAssigneeMobile = value
			}
		}
	}
	return out, rows.Err()
}

func (s *SettingsStore) Update(ctx context.Context, req domain.UpdateSettingsRequest) (domain.AppSettings, error) {
	if req.WorkspaceRoot != "" && !s.lockWorkspaceRoot {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO app_settings (key, value, updated_at) VALUES ('workspace_root', $1, now())
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
		`, req.WorkspaceRoot)
		if err != nil {
			return domain.AppSettings{}, fmt.Errorf("update workspace_root: %w", err)
		}
	}
	if req.DefaultLanguage != "" {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO app_settings (key, value, updated_at) VALUES ('default_language', $1, now())
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
		`, req.DefaultLanguage)
		if err != nil {
			return domain.AppSettings{}, fmt.Errorf("update default_language: %w", err)
		}
	}
	for key, value := range map[string]string{
		"pipeline_container_runtime": req.PipelineContainerRuntime,
		"boilerplate_catalog_repo":   req.BoilerplateCatalogRepo,
	} {
		if value == "" {
			continue
		}
		if value == "-" {
			value = ""
		}
		_, err := s.pool.Exec(ctx, `
			INSERT INTO app_settings (key, value, updated_at) VALUES ($1, $2, now())
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
		`, key, value)
		if err != nil {
			return domain.AppSettings{}, fmt.Errorf("update %s: %w", key, err)
		}
	}
	return s.Get(ctx)
}

// UpdateAnalizAssignment writes the backend/frontend/mobile analiz-assignment
// keys. Each argument follows the same leave-alone/reset contract as Update's
// pipeline_container_runtime/boilerplate_catalog_repo fields: "" leaves that
// area's row untouched, "-" clears it back to the default (system-architect),
// anything else overwrites it.
func (s *SettingsStore) UpdateAnalizAssignment(ctx context.Context, backend, frontend, mobile string) (domain.AppSettings, error) {
	for key, value := range map[string]string{
		"analiz_assignee_backend":  backend,
		"analiz_assignee_frontend": frontend,
		"analiz_assignee_mobile":   mobile,
	} {
		if value == "" {
			continue
		}
		if value == "-" {
			value = ""
		}
		_, err := s.pool.Exec(ctx, `
			INSERT INTO app_settings (key, value, updated_at) VALUES ($1, $2, now())
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
		`, key, value)
		if err != nil {
			return domain.AppSettings{}, fmt.Errorf("update %s: %w", key, err)
		}
	}
	return s.Get(ctx)
}

const githubTokenKey = "github_token"

// SetCipher hands the store the cipher derived at boot, before
// runtime.scrubProcessSecrets removes MCP_SECRETS_KEY from the process
// environment.
//
// Without this the store derived its own cipher lazily, on first use — which is
// always after the scrub, so os.Getenv came back empty and every credential
// path failed with "MCP_SECRETS_KEY or SERVER_API_KEY required". In production
// that read as "GitHub durumu alınamadı" and a silently disabled deploy
// console, not as a missing key.
//
// It consumes cipherOnce, so a later getCipher can no longer fall back to the
// scrubbed environment. Callers that never call this (tests, and any embedder
// that does not scrub) keep the lazy behaviour.
func (s *SettingsStore) SetCipher(c *secrets.Cipher, err error) {
	s.cipherOnce.Do(func() {
		s.cipher, s.cipherErr = c, err
	})
}

func (s *SettingsStore) getCipher() (*secrets.Cipher, error) {
	s.cipherOnce.Do(func() {
		s.cipher, s.cipherErr = secrets.NewCipherFromEnv()
	})
	return s.cipher, s.cipherErr
}

// secret reads and decrypts one encrypted app_settings value; "" when absent.
func (s *SettingsStore) secret(ctx context.Context, key string) (string, error) {
	var value string
	err := s.pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, key).Scan(&value)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("get %s: %w", key, err)
	}
	if value == "" {
		return "", nil
	}
	cipher, err := s.getCipher()
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("decode %s: %w", key, err)
	}
	return cipher.Decrypt(raw)
}

// setSecret encrypts and upserts one app_settings value.
func (s *SettingsStore) setSecret(ctx context.Context, key, plaintext string) error {
	cipher, err := s.getCipher()
	if err != nil {
		return err
	}
	ct, err := cipher.Encrypt(plaintext)
	if err != nil {
		return fmt.Errorf("encrypt %s: %w", key, err)
	}
	return s.setPlain(ctx, key, base64.StdEncoding.EncodeToString(ct))
}

// setPlain upserts one app_settings value as given.
func (s *SettingsStore) setPlain(ctx context.Context, key, value string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO app_settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, key, value)
	if err != nil {
		return fmt.Errorf("set %s: %w", key, err)
	}
	return nil
}

// forgetKeys removes the given app_settings rows.
func (s *SettingsStore) forgetKeys(ctx context.Context, keys ...string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM app_settings WHERE key = ANY($1)`, keys); err != nil {
		return fmt.Errorf("delete %v: %w", keys, err)
	}
	return nil
}

// GitHubToken, kayıtlı token'ı çözerek döndürür; kayıt yoksa "".
func (s *SettingsStore) GitHubToken(ctx context.Context) (string, error) {
	return s.secret(ctx, githubTokenKey)
}

// SetGitHubToken, token'ı şifreleyip kaydeder.
func (s *SettingsStore) SetGitHubToken(ctx context.Context, token string) error {
	return s.setSecret(ctx, githubTokenKey, token)
}

// DeleteGitHubToken, kayıtlı token'ı siler.
func (s *SettingsStore) DeleteGitHubToken(ctx context.Context) error {
	return s.forgetKeys(ctx, githubTokenKey)
}

// Vercel: the access token is encrypted exactly like the GitHub one; the
// default team is a plain scope id (team_…, or "" for the personal account).
const (
	vercelTokenKey = "vercel_token"
	vercelTeamKey  = "vercel_team_id"
)

func (s *SettingsStore) VercelToken(ctx context.Context) (string, error) {
	return s.secret(ctx, vercelTokenKey)
}

func (s *SettingsStore) SetVercelToken(ctx context.Context, token string) error {
	return s.setSecret(ctx, vercelTokenKey, token)
}

// DeleteVercelToken drops the token and the team it was scoped to together.
func (s *SettingsStore) DeleteVercelToken(ctx context.Context) error {
	return s.forgetKeys(ctx, vercelTokenKey, vercelTeamKey)
}

func (s *SettingsStore) VercelTeam(ctx context.Context) (string, error) {
	var value string
	err := s.pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, vercelTeamKey).Scan(&value)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("get %s: %w", vercelTeamKey, err)
	}
	return value, nil
}

func (s *SettingsStore) SetVercelTeam(ctx context.Context, teamID string) error {
	return s.setPlain(ctx, vercelTeamKey, teamID)
}
