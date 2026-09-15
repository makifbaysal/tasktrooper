package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// RepoDependencyStore persists repository_dependencies (migration 134).
type RepoDependencyStore struct {
	pool *DB

	cipherOnce sync.Once
	cipher     *secrets.Cipher
	cipherErr  error
}

func NewRepoDependencyStore(pool *DB) *RepoDependencyStore {
	return &RepoDependencyStore{pool: pool}
}

// SetCipher injects the cipher derived at boot, before the process
// environment is scrubbed of MCP_SECRETS_KEY — see RepositoryStore.SetCipher.
func (s *RepoDependencyStore) SetCipher(c *secrets.Cipher, err error) {
	s.cipherOnce.Do(func() { s.cipher, s.cipherErr = c, err })
}

func (s *RepoDependencyStore) getCipher() (*secrets.Cipher, error) {
	s.cipherOnce.Do(func() {
		s.cipher, s.cipherErr = secrets.NewCipherFromEnv()
	})
	return s.cipher, s.cipherErr
}

const repoDependencyCols = `id, repository_id, target_kind, target_repository_id, target_sub_project_path,
	database_label, database_engine, database_env, database_host, database_port, database_name, database_username,
	database_secret_enc, note, created_at, updated_at`

func scanRepoDependency(row pgx.Row) (domain.RepoDependency, error) {
	var d domain.RepoDependency
	var secretEnc []byte
	err := row.Scan(
		&d.ID, &d.RepositoryID, &d.TargetKind, &d.TargetRepositoryID, &d.TargetSubProjectPath,
		&d.DatabaseLabel, &d.DatabaseEngine, &d.DatabaseEnv, &d.DatabaseHost, &d.DatabasePort, &d.DatabaseName, &d.DatabaseUsername,
		&secretEnc, &d.Note, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return d, err
	}
	if len(secretEnc) > 0 {
		d.DatabaseSecret = secrets.MaskedValue()
	}
	return d, nil
}

func (s *RepoDependencyStore) ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.RepoDependency, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+repoDependencyCols+`
		FROM repository_dependencies WHERE repository_id = $1 ORDER BY created_at`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list repo dependencies: %w", err)
	}
	defer rows.Close()
	var out []domain.RepoDependency
	for rows.Next() {
		d, err := scanRepoDependency(rows)
		if err != nil {
			return nil, fmt.Errorf("scan repo dependency: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *RepoDependencyStore) ListByProject(ctx context.Context, projectID uuid.UUID) ([]domain.RepoDependency, []domain.RepoDependency, error) {
	const projectRepos = `SELECT repository_id FROM repository_projects WHERE project_id = $1`

	outRows, err := s.pool.Query(ctx, `SELECT `+repoDependencyCols+`
		FROM repository_dependencies WHERE repository_id IN (`+projectRepos+`) ORDER BY created_at`, projectID)
	if err != nil {
		return nil, nil, fmt.Errorf("list outgoing repo dependencies: %w", err)
	}
	var outgoing []domain.RepoDependency
	for outRows.Next() {
		d, err := scanRepoDependency(outRows)
		if err != nil {
			outRows.Close()
			return nil, nil, fmt.Errorf("scan outgoing repo dependency: %w", err)
		}
		outgoing = append(outgoing, d)
	}
	outRows.Close()
	if err := outRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("list outgoing repo dependencies: %w", err)
	}

	inRows, err := s.pool.Query(ctx, `SELECT `+repoDependencyCols+`
		FROM repository_dependencies
		WHERE target_repository_id IN (`+projectRepos+`) AND repository_id NOT IN (`+projectRepos+`)
		ORDER BY created_at`, projectID)
	if err != nil {
		return nil, nil, fmt.Errorf("list incoming repo dependencies: %w", err)
	}
	defer inRows.Close()
	var incoming []domain.RepoDependency
	for inRows.Next() {
		d, err := scanRepoDependency(inRows)
		if err != nil {
			return nil, nil, fmt.Errorf("scan incoming repo dependency: %w", err)
		}
		incoming = append(incoming, d)
	}
	return outgoing, incoming, inRows.Err()
}

func (s *RepoDependencyStore) Get(ctx context.Context, id uuid.UUID) (domain.RepoDependency, error) {
	d, err := scanRepoDependency(s.pool.QueryRow(ctx, `SELECT `+repoDependencyCols+`
		FROM repository_dependencies WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RepoDependency{}, fmt.Errorf("get repo dependency: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.RepoDependency{}, fmt.Errorf("get repo dependency: %w", err)
	}
	return d, nil
}

func (s *RepoDependencyStore) Create(ctx context.Context, repositoryID uuid.UUID, req domain.SaveRepoDependencyRequest) (domain.RepoDependency, error) {
	secretEnc, err := s.encryptSecret(req.DatabaseSecret)
	if err != nil {
		return domain.RepoDependency{}, err
	}
	d, err := scanRepoDependency(s.pool.QueryRow(ctx, `
		INSERT INTO repository_dependencies
			(repository_id, target_kind, target_repository_id, target_sub_project_path,
			 database_label, database_engine, database_env, database_host, database_port, database_name, database_username,
			 database_secret_enc, note)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING `+repoDependencyCols,
		repositoryID, req.TargetKind, req.TargetRepositoryID, req.TargetSubProjectPath,
		req.DatabaseLabel, req.DatabaseEngine, req.DatabaseEnv, req.DatabaseHost, req.DatabasePort, req.DatabaseName, req.DatabaseUsername,
		secretEnc, req.Note))
	if err != nil {
		return domain.RepoDependency{}, fmt.Errorf("create repo dependency: %w", err)
	}
	return d, nil
}

// Update applies req. A "" or masked DatabaseSecret leaves the stored secret
// untouched — the same rule mcp/resolve.go applies to a masked field.
func (s *RepoDependencyStore) Update(ctx context.Context, id uuid.UUID, req domain.SaveRepoDependencyRequest) (domain.RepoDependency, error) {
	if req.DatabaseSecret == "" || secrets.IsMaskedValue(req.DatabaseSecret) {
		d, err := scanRepoDependency(s.pool.QueryRow(ctx, `
			UPDATE repository_dependencies SET
				target_kind = $2, target_repository_id = $3, target_sub_project_path = $4,
				database_label = $5, database_engine = $6, database_env = $7, database_host = $8,
				database_port = $9, database_name = $10, database_username = $11, note = $12,
				updated_at = now()
			WHERE id = $1
			RETURNING `+repoDependencyCols,
			id, req.TargetKind, req.TargetRepositoryID, req.TargetSubProjectPath,
			req.DatabaseLabel, req.DatabaseEngine, req.DatabaseEnv, req.DatabaseHost, req.DatabasePort, req.DatabaseName, req.DatabaseUsername, req.Note))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.RepoDependency{}, fmt.Errorf("update repo dependency: %w", port.ErrNotFound)
		}
		if err != nil {
			return domain.RepoDependency{}, fmt.Errorf("update repo dependency: %w", err)
		}
		return d, nil
	}

	secretEnc, err := s.encryptSecret(req.DatabaseSecret)
	if err != nil {
		return domain.RepoDependency{}, err
	}
	d, err := scanRepoDependency(s.pool.QueryRow(ctx, `
		UPDATE repository_dependencies SET
			target_kind = $2, target_repository_id = $3, target_sub_project_path = $4,
			database_label = $5, database_engine = $6, database_env = $7, database_host = $8,
			database_port = $9, database_name = $10, database_username = $11, database_secret_enc = $12, note = $13,
			updated_at = now()
		WHERE id = $1
		RETURNING `+repoDependencyCols,
		id, req.TargetKind, req.TargetRepositoryID, req.TargetSubProjectPath,
		req.DatabaseLabel, req.DatabaseEngine, req.DatabaseEnv, req.DatabaseHost, req.DatabasePort, req.DatabaseName, req.DatabaseUsername,
		secretEnc, req.Note))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RepoDependency{}, fmt.Errorf("update repo dependency: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.RepoDependency{}, fmt.Errorf("update repo dependency: %w", err)
	}
	return d, nil
}

func (s *RepoDependencyStore) encryptSecret(secret string) ([]byte, error) {
	if secret == "" {
		return nil, nil
	}
	cipher, err := s.getCipher()
	if err != nil {
		return nil, err
	}
	ct, err := cipher.Encrypt(secret)
	if err != nil {
		return nil, fmt.Errorf("encrypt database secret: %w", err)
	}
	return ct, nil
}

func (s *RepoDependencyStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM repository_dependencies WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete repo dependency: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete repo dependency: %w", port.ErrNotFound)
	}
	return nil
}
