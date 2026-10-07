package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type StoreTestBuildStore struct {
	pool *DB
}

func NewStoreTestBuildStore(pool *DB) *StoreTestBuildStore {
	return &StoreTestBuildStore{pool: pool}
}

var _ port.StoreTestBuildStore = (*StoreTestBuildStore)(nil)

const storeTestBuildCols = `id, repository_id, platform, task_id, task_key, task_number, attempt, build_sequence,
	build_number, version_name, commit_sha, branch, engine, status, failure, store_build_id, install_url,
	artifact_path, test_groups, notes, run_url, log_tail, trigger_source, created_by, created_at, updated_at, finished_at`

func scanStoreTestBuild(row pgx.Row) (domain.StoreTestBuild, error) {
	var b domain.StoreTestBuild
	var groups []byte
	if err := row.Scan(
		&b.ID, &b.RepositoryID, &b.Platform, &b.TaskID, &b.TaskKey, &b.TaskNumber, &b.Attempt, &b.Sequence,
		&b.BuildNumber, &b.VersionName, &b.CommitSHA, &b.Branch, &b.Engine, &b.Status, &b.Failure, &b.StoreBuildID, &b.InstallURL,
		&b.ArtifactPath, &groups, &b.Notes, &b.RunURL, &b.LogTail, &b.Trigger, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt, &b.FinishedAt,
	); err != nil {
		return domain.StoreTestBuild{}, err
	}
	b.Groups = []string{}
	if len(groups) > 0 {
		if err := json.Unmarshal(groups, &b.Groups); err != nil {
			return domain.StoreTestBuild{}, fmt.Errorf("unmarshal test build groups: %w", err)
		}
	}
	b.HasArtifact = b.ArtifactPath != ""
	return b, nil
}

func groupsJSON(groups []string) ([]byte, error) {
	if groups == nil {
		groups = []string{}
	}
	return json.Marshal(groups)
}

func (s *StoreTestBuildStore) Create(ctx context.Context, b domain.StoreTestBuild) (domain.StoreTestBuild, error) {
	groups, err := groupsJSON(b.Groups)
	if err != nil {
		return domain.StoreTestBuild{}, err
	}
	out, err := scanStoreTestBuild(s.pool.QueryRow(ctx, `
		INSERT INTO store_test_builds (
			repository_id, platform, task_id, task_key, task_number, attempt, build_sequence, build_number,
			version_name, commit_sha, branch, engine, status, failure, store_build_id, install_url,
			artifact_path, test_groups, notes, run_url, log_tail, trigger_source, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)
		RETURNING `+storeTestBuildCols,
		b.RepositoryID, b.Platform, b.TaskID, b.TaskKey, b.TaskNumber, b.Attempt, b.Sequence, b.BuildNumber,
		b.VersionName, b.CommitSHA, b.Branch, b.Engine, b.Status, b.Failure, b.StoreBuildID, b.InstallURL,
		b.ArtifactPath, groups, b.Notes, b.RunURL, b.LogTail, b.Trigger, b.CreatedBy,
	))
	if err != nil {
		return domain.StoreTestBuild{}, fmt.Errorf("create store test build: %w", err)
	}
	return out, nil
}

func (s *StoreTestBuildStore) Update(ctx context.Context, b domain.StoreTestBuild) (domain.StoreTestBuild, error) {
	groups, err := groupsJSON(b.Groups)
	if err != nil {
		return domain.StoreTestBuild{}, err
	}
	out, err := scanStoreTestBuild(s.pool.QueryRow(ctx, `
		UPDATE store_test_builds SET
			version_name = $2, commit_sha = $3, branch = $4, engine = $5, status = $6, failure = $7,
			store_build_id = $8, install_url = $9, artifact_path = $10, test_groups = $11, notes = $12,
			run_url = $13, log_tail = $14, finished_at = $15, updated_at = now()
		WHERE id = $1
		RETURNING `+storeTestBuildCols,
		b.ID, b.VersionName, b.CommitSHA, b.Branch, b.Engine, b.Status, b.Failure,
		b.StoreBuildID, b.InstallURL, b.ArtifactPath, groups, b.Notes,
		b.RunURL, b.LogTail, b.FinishedAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.StoreTestBuild{}, fmt.Errorf("update store test build: %w", domain.ErrTestBuildNotFound)
	}
	if err != nil {
		return domain.StoreTestBuild{}, fmt.Errorf("update store test build: %w", err)
	}
	return out, nil
}

func (s *StoreTestBuildStore) Get(ctx context.Context, id uuid.UUID) (domain.StoreTestBuild, error) {
	out, err := scanStoreTestBuild(s.pool.QueryRow(ctx, `SELECT `+storeTestBuildCols+` FROM store_test_builds WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.StoreTestBuild{}, fmt.Errorf("get store test build: %w", domain.ErrTestBuildNotFound)
	}
	if err != nil {
		return domain.StoreTestBuild{}, fmt.Errorf("get store test build: %w", err)
	}
	return out, nil
}

func (s *StoreTestBuildStore) List(ctx context.Context, repositoryID uuid.UUID, platform string, taskID *uuid.UUID, limit int) ([]domain.StoreTestBuild, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT `+storeTestBuildCols+` FROM store_test_builds
		WHERE repository_id = $1
		  AND ($2 = '' OR platform = $2)
		  AND ($3::uuid IS NULL OR task_id = $3)
		ORDER BY created_at DESC
		LIMIT $4`, repositoryID, platform, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("list store test builds: %w", err)
	}
	return collectStoreTestBuilds(rows)
}

func collectStoreTestBuilds(rows pgx.Rows) ([]domain.StoreTestBuild, error) {
	defer rows.Close()
	out := []domain.StoreTestBuild{}
	for rows.Next() {
		b, err := scanStoreTestBuild(rows)
		if err != nil {
			return nil, fmt.Errorf("scan store test build: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *StoreTestBuildStore) MaxSequence(ctx context.Context, repositoryID uuid.UUID, platform string) (int64, error) {
	var max int64
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(MAX(build_sequence), 0) FROM store_test_builds
		WHERE repository_id = $1 AND platform = $2`, repositoryID, platform).Scan(&max); err != nil {
		return 0, fmt.Errorf("max store test build sequence: %w", err)
	}
	return max, nil
}

func (s *StoreTestBuildStore) MaxAttempt(ctx context.Context, taskID uuid.UUID, platform string) (int, error) {
	var max int
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(MAX(attempt), 0) FROM store_test_builds
		WHERE task_id = $1 AND platform = $2`, taskID, platform).Scan(&max); err != nil {
		return 0, fmt.Errorf("max store test build attempt: %w", err)
	}
	return max, nil
}

func (s *StoreTestBuildStore) ListUnfinished(ctx context.Context) ([]domain.StoreTestBuild, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+storeTestBuildCols+` FROM store_test_builds
		WHERE status NOT IN ('ready', 'failed', 'dispatched') ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list unfinished store test builds: %w", err)
	}
	return collectStoreTestBuilds(rows)
}

func (s *StoreTestBuildStore) AutoGroups(ctx context.Context, repositoryID uuid.UUID, platform string) ([]string, bool, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT auto_groups FROM store_test_settings WHERE repository_id = $1 AND platform = $2`,
		repositoryID, platform).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get store test settings: %w", err)
	}
	groups := []string{}
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, false, fmt.Errorf("unmarshal store test auto groups: %w", err)
	}
	return groups, true, nil
}

func (s *StoreTestBuildStore) SetAutoGroups(ctx context.Context, repositoryID uuid.UUID, platform string, groups []string) error {
	raw, err := groupsJSON(groups)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO store_test_settings (repository_id, platform, auto_groups)
		VALUES ($1, $2, $3)
		ON CONFLICT (repository_id, platform) DO UPDATE SET auto_groups = EXCLUDED.auto_groups, updated_at = now()
	`, repositoryID, platform, raw); err != nil {
		return fmt.Errorf("set store test settings: %w", err)
	}
	return nil
}
