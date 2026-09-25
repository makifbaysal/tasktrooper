package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// ReleaseStore persists releases and the tasks each one carries (migration
// 159). Every read fills Release.Tasks off release_tasks JOIN board_tasks so a
// caller never has to issue a second query per release just to render its
// task list.
type ReleaseStore struct {
	pool *DB
}

func NewReleaseStore(db *DB) *ReleaseStore {
	return &ReleaseStore{pool: db}
}

var _ port.ReleaseStore = (*ReleaseStore)(nil)

const releaseCols = `id, repository_id, component_id, version, mode, executor, status, commit_sha, tag, notes,
	profile, deploy, checks, verdict, rollback, failure_reason, card_task_id, local_run, store_builds, cut_at,
	created_at, updated_at, deploy_started_at, deployed_at, verify_until, finished_at`

const releaseColsPrefixed = `r.id, r.repository_id, r.component_id, r.version, r.mode, r.executor, r.status, r.commit_sha, r.tag, r.notes,
	r.profile, r.deploy, r.checks, r.verdict, r.rollback, r.failure_reason, r.card_task_id, r.local_run, r.store_builds, r.cut_at,
	r.created_at, r.updated_at, r.deploy_started_at, r.deployed_at, r.verify_until, r.finished_at`

func scanRelease(row pgx.Row) (domain.Release, error) {
	var r domain.Release
	var mode, executor, status string
	var profileJSON, checksJSON, deployJSON, rollbackJSON, localRunJSON, storeBuildsJSON []byte
	if err := row.Scan(
		&r.ID, &r.RepositoryID, &r.ComponentID, &r.Version, &mode, &executor, &status, &r.CommitSHA, &r.Tag, &r.Notes,
		&profileJSON, &deployJSON, &checksJSON, &r.Verdict, &rollbackJSON, &r.FailureReason, &r.CardTaskID,
		&localRunJSON, &storeBuildsJSON, &r.CutAt,
		&r.CreatedAt, &r.UpdatedAt, &r.DeployStartedAt, &r.DeployedAt, &r.VerifyUntil, &r.FinishedAt,
	); err != nil {
		return domain.Release{}, err
	}
	r.Mode = domain.DeliveryMode(mode)
	r.Executor = domain.DeliveryExecutor(executor)
	r.Status = domain.ReleaseStatus(status)
	if err := json.Unmarshal(profileJSON, &r.Profile); err != nil {
		return domain.Release{}, fmt.Errorf("unmarshal release profile: %w", err)
	}
	if err := json.Unmarshal(checksJSON, &r.Checks); err != nil {
		return domain.Release{}, fmt.Errorf("unmarshal release checks: %w", err)
	}
	if deployJSON != nil {
		var d domain.DeployWatchStatus
		if err := json.Unmarshal(deployJSON, &d); err != nil {
			return domain.Release{}, fmt.Errorf("unmarshal release deploy: %w", err)
		}
		r.Deploy = &d
	}
	if rollbackJSON != nil {
		var rb domain.ReleaseRollback
		if err := json.Unmarshal(rollbackJSON, &rb); err != nil {
			return domain.Release{}, fmt.Errorf("unmarshal release rollback: %w", err)
		}
		r.Rollback = &rb
	}
	if localRunJSON != nil {
		var lr domain.ReleaseLocalRun
		if err := json.Unmarshal(localRunJSON, &lr); err != nil {
			return domain.Release{}, fmt.Errorf("unmarshal release local_run: %w", err)
		}
		r.LocalRun = &lr
	}
	if storeBuildsJSON != nil {
		if err := json.Unmarshal(storeBuildsJSON, &r.StoreBuilds); err != nil {
			return domain.Release{}, fmt.Errorf("unmarshal release store_builds: %w", err)
		}
	}
	return r, nil
}

// loadTasks fills Release.Tasks for a batch of releases in one query instead
// of one-per-release, ordered by added_at like the port doc promises (the
// order a task joined the release, not board or key order).
func (s *ReleaseStore) loadTasks(ctx context.Context, releaseIDs []uuid.UUID) (map[uuid.UUID][]domain.ReleaseTaskRef, error) {
	if len(releaseIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT rt.release_id, bt.id, `+taskKeySQL+`, bt.title, bt.task_type, bt.board_column, bt.merge_commit_sha
		FROM release_tasks rt
		JOIN board_tasks bt ON bt.id = rt.task_id
		WHERE rt.release_id = ANY($1)
		ORDER BY rt.added_at
	`, releaseIDs)
	if err != nil {
		return nil, fmt.Errorf("load release tasks: %w", err)
	}
	defer rows.Close()
	out := make(map[uuid.UUID][]domain.ReleaseTaskRef, len(releaseIDs))
	for rows.Next() {
		var releaseID uuid.UUID
		var t domain.ReleaseTaskRef
		var taskType, col string
		var mergeSHA *string
		if err := rows.Scan(&releaseID, &t.ID, &t.Key, &t.Title, &taskType, &col, &mergeSHA); err != nil {
			return nil, fmt.Errorf("scan release task: %w", err)
		}
		t.TaskType = domain.TaskType(taskType)
		t.Column = domain.TaskColumn(col)
		if mergeSHA != nil {
			t.MergeCommitSHA = *mergeSHA
		}
		out[releaseID] = append(out[releaseID], t)
	}
	return out, rows.Err()
}

func (s *ReleaseStore) fillOne(ctx context.Context, r domain.Release) (domain.Release, error) {
	tasks, err := s.loadTasks(ctx, []uuid.UUID{r.ID})
	if err != nil {
		return domain.Release{}, err
	}
	r.Tasks = tasks[r.ID]
	return r, nil
}

const insertReleaseSQL = `
INSERT INTO releases
	(id, repository_id, component_id, version, mode, executor, status, commit_sha, tag, notes,
	 profile, deploy, checks, verdict, rollback, failure_reason, card_task_id, local_run, store_builds, cut_at,
	 deploy_started_at, deployed_at, verify_until, finished_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
RETURNING ` + releaseCols

// Create inserts the release and its starting tasks in one transaction, so a
// release can never exist with zero tasks because the process died between
// the two writes.
func (s *ReleaseStore) Create(ctx context.Context, r domain.Release, taskIDs []uuid.UUID) (domain.Release, error) {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	if r.Status == "" {
		r.Status = domain.ReleasePending
	}
	profileJSON, err := json.Marshal(r.Profile)
	if err != nil {
		return domain.Release{}, fmt.Errorf("marshal release profile: %w", err)
	}
	checksJSON, err := json.Marshal(r.Checks)
	if err != nil {
		return domain.Release{}, fmt.Errorf("marshal release checks: %w", err)
	}
	var deployJSON []byte
	if r.Deploy != nil {
		if deployJSON, err = json.Marshal(r.Deploy); err != nil {
			return domain.Release{}, fmt.Errorf("marshal release deploy: %w", err)
		}
	}
	var rollbackJSON []byte
	if r.Rollback != nil {
		if rollbackJSON, err = json.Marshal(r.Rollback); err != nil {
			return domain.Release{}, fmt.Errorf("marshal release rollback: %w", err)
		}
	}
	var localRunJSON []byte
	if r.LocalRun != nil {
		if localRunJSON, err = json.Marshal(r.LocalRun); err != nil {
			return domain.Release{}, fmt.Errorf("marshal release local_run: %w", err)
		}
	}
	storeBuilds := r.StoreBuilds
	if storeBuilds == nil {
		storeBuilds = []domain.ReleaseStoreBuild{}
	}
	storeBuildsJSON, err := json.Marshal(storeBuilds)
	if err != nil {
		return domain.Release{}, fmt.Errorf("marshal release store_builds: %w", err)
	}

	var out domain.Release
	err = s.pool.InTx(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, insertReleaseSQL,
			r.ID, r.RepositoryID, r.ComponentID, r.Version, string(r.Mode), string(r.Executor), string(r.Status),
			r.CommitSHA, r.Tag, r.Notes, profileJSON, deployJSON, checksJSON, r.Verdict, rollbackJSON,
			r.FailureReason, r.CardTaskID, localRunJSON, storeBuildsJSON, r.CutAt,
			r.DeployStartedAt, r.DeployedAt, r.VerifyUntil, r.FinishedAt)
		created, err := scanRelease(row)
		if err != nil {
			return fmt.Errorf("insert release: %w", err)
		}
		out = created
		seen := make(map[uuid.UUID]bool, len(taskIDs))
		for _, taskID := range taskIDs {
			if taskID == uuid.Nil || seen[taskID] {
				continue
			}
			seen[taskID] = true
			if _, err := tx.Exec(ctx, `
				INSERT INTO release_tasks (release_id, task_id) VALUES ($1, $2)
				ON CONFLICT DO NOTHING
			`, out.ID, taskID); err != nil {
				return fmt.Errorf("insert release task: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return domain.Release{}, fmt.Errorf("create release: %w", err)
	}
	return s.fillOne(ctx, out)
}

func (s *ReleaseStore) Get(ctx context.Context, id uuid.UUID) (domain.Release, error) {
	r, err := scanRelease(s.pool.QueryRow(ctx, `SELECT `+releaseCols+` FROM releases WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Release{}, fmt.Errorf("get release: %w", domain.ErrReleaseNotFound)
	}
	if err != nil {
		return domain.Release{}, fmt.Errorf("get release: %w", err)
	}
	return s.fillOne(ctx, r)
}

// ForTask is the newest release carrying the task — release_tasks rows accrue
// (a superseding release carries the old one's tasks forward too), so this is
// ORDER BY created_at DESC LIMIT 1, not a uniqueness assumption.
func (s *ReleaseStore) ForTask(ctx context.Context, taskID uuid.UUID) (domain.Release, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+releaseColsPrefixed+`
		FROM releases r
		JOIN release_tasks rt ON rt.release_id = r.id
		WHERE rt.task_id = $1
		ORDER BY r.created_at DESC
		LIMIT 1
	`, taskID)
	r, err := scanRelease(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Release{}, fmt.Errorf("release for task: %w", domain.ErrReleaseNotFound)
	}
	if err != nil {
		return domain.Release{}, fmt.Errorf("release for task: %w", err)
	}
	return s.fillOne(ctx, r)
}

// List filters newest first; an empty filter lists every release capped at
// the default page size.
func (s *ReleaseStore) List(ctx context.Context, f domain.ReleaseListFilter) ([]domain.Release, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	conds := make([]string, 0, 4)
	args := make([]any, 0, 5)
	if f.RepositoryID != nil {
		args = append(args, *f.RepositoryID)
		conds = append(conds, fmt.Sprintf("repository_id = $%d", len(args)))
	}
	if f.ComponentID != nil {
		args = append(args, *f.ComponentID)
		conds = append(conds, fmt.Sprintf("component_id = $%d", len(args)))
	}
	if f.TaskID != nil {
		args = append(args, *f.TaskID)
		conds = append(conds, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM release_tasks rt WHERE rt.release_id = releases.id AND rt.task_id = $%d)", len(args)))
	}
	if len(f.Statuses) > 0 {
		statuses := make([]string, len(f.Statuses))
		for i, st := range f.Statuses {
			statuses[i] = string(st)
		}
		args = append(args, statuses)
		conds = append(conds, fmt.Sprintf("status = ANY($%d)", len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, `
		SELECT `+releaseCols+`
		FROM releases
		`+where+`
		ORDER BY created_at DESC
		LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("list releases: %w", err)
	}
	defer rows.Close()
	var out []domain.Release
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		r, err := scanRelease(rows)
		if err != nil {
			return nil, fmt.Errorf("scan release: %w", err)
		}
		out = append(out, r)
		ids = append(ids, r.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list releases: %w", err)
	}
	tasks, err := s.loadTasks(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list releases: %w", err)
	}
	for i := range out {
		out[i].Tasks = tasks[out[i].ID]
	}
	return out, nil
}

const updateReleaseSQL = `
UPDATE releases SET
	status = $3,
	version = $4,
	commit_sha = $5,
	tag = $6,
	notes = $7,
	deploy = $8,
	checks = $9,
	verdict = $10,
	rollback = $11,
	failure_reason = $12,
	card_task_id = $13,
	local_run = $14,
	store_builds = $15,
	cut_at = $16,
	deploy_started_at = $17,
	deployed_at = $18,
	verify_until = $19,
	finished_at = $20,
	updated_at = now()
WHERE id = $1 AND status = $2
RETURNING ` + releaseCols

// Update is conditional on the stored status still being expect (see the port
// doc comment): two sweeps, or a sweep racing an agent's finish/rollback,
// cannot both advance the same release. Zero rows means either the release
// does not exist or its status moved out from under the caller; a second read
// tells the two apart so the error names what actually happened.
func (s *ReleaseStore) Update(ctx context.Context, r domain.Release, expect domain.ReleaseStatus) (domain.Release, error) {
	deployJSON, checksJSON, rollbackJSON, localRunJSON, storeBuildsJSON, err := marshalReleaseMutable(r)
	if err != nil {
		return domain.Release{}, fmt.Errorf("update release: %w", err)
	}
	row := s.pool.QueryRow(ctx, updateReleaseSQL,
		r.ID, string(expect), string(r.Status), r.Version, r.CommitSHA, r.Tag, r.Notes,
		deployJSON, checksJSON, r.Verdict, rollbackJSON, r.FailureReason, r.CardTaskID,
		localRunJSON, storeBuildsJSON, r.CutAt,
		r.DeployStartedAt, r.DeployedAt, r.VerifyUntil, r.FinishedAt)
	updated, err := scanRelease(row)
	if errors.Is(err, pgx.ErrNoRows) {
		var actual string
		lookupErr := s.pool.QueryRow(ctx, `SELECT status FROM releases WHERE id = $1`, r.ID).Scan(&actual)
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			return domain.Release{}, fmt.Errorf("update release: %w", domain.ErrReleaseNotFound)
		}
		if lookupErr != nil {
			return domain.Release{}, fmt.Errorf("update release: %w", lookupErr)
		}
		return domain.Release{}, fmt.Errorf("update release: %w: is %s, expected %s", domain.ErrReleaseWrongStatus, actual, expect)
	}
	if err != nil {
		return domain.Release{}, fmt.Errorf("update release: %w", err)
	}
	return s.fillOne(ctx, updated)
}

func marshalReleaseMutable(r domain.Release) (deployJSON, checksJSON, rollbackJSON, localRunJSON, storeBuildsJSON []byte, err error) {
	if r.Deploy != nil {
		if deployJSON, err = json.Marshal(r.Deploy); err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("marshal release deploy: %w", err)
		}
	}
	if checksJSON, err = json.Marshal(r.Checks); err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("marshal release checks: %w", err)
	}
	if r.Rollback != nil {
		if rollbackJSON, err = json.Marshal(r.Rollback); err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("marshal release rollback: %w", err)
		}
	}
	if r.LocalRun != nil {
		if localRunJSON, err = json.Marshal(r.LocalRun); err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("marshal release local_run: %w", err)
		}
	}
	storeBuilds := r.StoreBuilds
	if storeBuilds == nil {
		storeBuilds = []domain.ReleaseStoreBuild{}
	}
	if storeBuildsJSON, err = json.Marshal(storeBuilds); err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("marshal release store_builds: %w", err)
	}
	return deployJSON, checksJSON, rollbackJSON, localRunJSON, storeBuildsJSON, nil
}

// AddTasks is idempotent: a release the sweeper is superseding and one that is
// being superseded can both try to carry the same task forward, and the
// second attempt must be a no-op rather than a primary-key error.
func (s *ReleaseStore) AddTasks(ctx context.Context, releaseID uuid.UUID, taskIDs []uuid.UUID) error {
	for _, taskID := range taskIDs {
		if taskID == uuid.Nil {
			continue
		}
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO release_tasks (release_id, task_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, releaseID, taskID); err != nil {
			return fmt.Errorf("add release task: %w", err)
		}
	}
	return nil
}

func (s *ReleaseStore) RemoveTask(ctx context.Context, releaseID, taskID uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `
		DELETE FROM release_tasks WHERE release_id = $1 AND task_id = $2
	`, releaseID, taskID); err != nil {
		return fmt.Errorf("remove release task: %w", err)
	}
	return nil
}

// LastReleased is the newest `released` release of the component finished
// before the given time. component_id IS NOT DISTINCT FROM treats NULL as a
// value that matches NULL — a component-less release only competes with other
// component-less releases of the same repository.
func (s *ReleaseStore) LastReleased(ctx context.Context, repositoryID uuid.UUID, componentID *uuid.UUID, before time.Time) (domain.Release, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+releaseCols+`
		FROM releases
		WHERE repository_id = $1
		  AND status = $2
		  AND finished_at < $3
		  AND component_id IS NOT DISTINCT FROM $4
		ORDER BY finished_at DESC
		LIMIT 1
	`, repositoryID, string(domain.ReleaseReleased), before, componentID)
	r, err := scanRelease(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Release{}, fmt.Errorf("last released: %w", domain.ErrReleaseNotFound)
	}
	if err != nil {
		return domain.Release{}, fmt.Errorf("last released: %w", err)
	}
	return s.fillOne(ctx, r)
}
