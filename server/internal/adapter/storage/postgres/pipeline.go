package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type PipelineStore struct {
	pool *DB
}

func NewPipelineStore(pool *DB) *PipelineStore {
	return &PipelineStore{pool: pool}
}

const pipelineCols = `id, task_id, repository_id, trigger, status, provider, note, head_sha, gate_reason, created_at, started_at, finished_at, coverage_pct`

func scanPipeline(row pgx.Row) (domain.TaskPipeline, error) {
	var p domain.TaskPipeline
	var trigger, status string
	if err := row.Scan(
		&p.ID, &p.TaskID, &p.RepositoryID, &trigger, &status, &p.Provider, &p.Note,
		&p.HeadSHA, &p.GateReason,
		&p.CreatedAt, &p.StartedAt, &p.FinishedAt, &p.CoveragePct,
	); err != nil {
		return domain.TaskPipeline{}, err
	}
	p.Trigger = domain.PipelineTrigger(trigger)
	p.Status = domain.PipelineStatus(status)
	return p, nil
}

func (s *PipelineStore) Create(ctx context.Context, p domain.TaskPipeline) (domain.TaskPipeline, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO task_pipelines (task_id, repository_id, trigger, status, provider, note, head_sha, gate_reason, coverage_pct)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+pipelineCols,
		p.TaskID, p.RepositoryID, string(p.Trigger), string(p.Status), p.Provider, p.Note, p.HeadSHA, p.GateReason, p.CoveragePct)
	out, err := scanPipeline(row)
	if err != nil {
		return domain.TaskPipeline{}, fmt.Errorf("create task pipeline: %w", err)
	}
	return out, nil
}

func (s *PipelineStore) Update(ctx context.Context, p domain.TaskPipeline) (domain.TaskPipeline, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE task_pipelines SET status = $2, note = $3, started_at = $4, finished_at = $5, provider = $6, coverage_pct = $7,
			head_sha = $8, gate_reason = $9
		WHERE id = $1
		RETURNING `+pipelineCols,
		p.ID, string(p.Status), p.Note, p.StartedAt, p.FinishedAt, p.Provider, p.CoveragePct, p.HeadSHA, p.GateReason)
	out, err := scanPipeline(row)
	if err != nil {
		return domain.TaskPipeline{}, fmt.Errorf("update task pipeline: %w", err)
	}
	return out, nil
}

func (s *PipelineStore) Get(ctx context.Context, id uuid.UUID) (domain.TaskPipeline, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+pipelineCols+` FROM task_pipelines WHERE id = $1`, id)
	out, err := scanPipeline(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskPipeline{}, domain.ErrPipelineNotFound
	}
	if err != nil {
		return domain.TaskPipeline{}, fmt.Errorf("get task pipeline: %w", err)
	}
	jobs, err := s.ListJobs(ctx, out.ID)
	if err != nil {
		return domain.TaskPipeline{}, err
	}
	out.Jobs = jobs
	return out, nil
}

// taskPipelineHistoryLimit bounds ListByTask: both readers (the task's
// pipeline panel and the bounce guard's same-commit streak) only care about
// recent history, and a task that cycled through QA for weeks must not turn
// every read into a scan of all of it.
const taskPipelineHistoryLimit = 100

// ListByTask returns the task's newest taskPipelineHistoryLimit pipelines,
// newest first, each with its jobs in position order.
func (s *PipelineStore) ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskPipeline, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+pipelineCols+` FROM task_pipelines
		WHERE task_id = $1 ORDER BY created_at DESC
		LIMIT $2
	`, taskID, taskPipelineHistoryLimit)
	if err != nil {
		return nil, fmt.Errorf("list task pipelines: %w", err)
	}
	defer rows.Close()
	var out []domain.TaskPipeline
	for rows.Next() {
		p, err := scanPipeline(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(out))
	for i := range out {
		ids[i] = out[i].ID
	}
	jobsByPipeline, err := s.listJobsForPipelines(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Jobs = jobsByPipeline[out[i].ID]
	}
	return out, nil
}

// listJobsForPipelines leaves a pipeline with no jobs out of the map, so the
// caller assigns it a nil slice — the same value ListJobs returns for none.
func (s *PipelineStore) listJobsForPipelines(ctx context.Context, pipelineIDs []uuid.UUID) (map[uuid.UUID][]domain.TaskPipelineJob, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+jobCols+` FROM task_pipeline_jobs
		WHERE pipeline_id = ANY($1) ORDER BY pipeline_id, position ASC
	`, pipelineIDs)
	if err != nil {
		return nil, fmt.Errorf("list pipeline jobs for pipelines: %w", err)
	}
	defer rows.Close()
	out := make(map[uuid.UUID][]domain.TaskPipelineJob, len(pipelineIDs))
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out[j.PipelineID] = append(out[j.PipelineID], j)
	}
	return out, rows.Err()
}

func (s *PipelineStore) LatestByTask(ctx context.Context, taskID uuid.UUID) (domain.TaskPipeline, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+pipelineCols+` FROM task_pipelines
		WHERE task_id = $1 ORDER BY created_at DESC LIMIT 1
	`, taskID)
	out, err := scanPipeline(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskPipeline{}, domain.ErrPipelineNotFound
	}
	if err != nil {
		return domain.TaskPipeline{}, fmt.Errorf("latest task pipeline: %w", err)
	}
	jobs, err := s.ListJobs(ctx, out.ID)
	if err != nil {
		return domain.TaskPipeline{}, err
	}
	out.Jobs = jobs
	return out, nil
}

// LatestStatusByTasks bulk-resolves the most recent pipeline status for each
// of the given tasks in a single query, so callers enriching a task list
// don't issue one query per task (N+1).
//
// The gate reason rides along in the same row because it is meaningless
// without the status and useless a query later: a card showing "skipped" has
// to be able to say whether nothing was configured or whether the board gave
// up waiting for a CI run that was never going to arrive.
func (s *PipelineStore) LatestStatusByTasks(ctx context.Context, taskIDs []uuid.UUID) (map[uuid.UUID]domain.TaskPipelineDigest, error) {
	out := make(map[uuid.UUID]domain.TaskPipelineDigest)
	if len(taskIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (task_id) task_id, status, gate_reason
		FROM task_pipelines
		WHERE task_id = ANY($1)
		ORDER BY task_id, created_at DESC
	`, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("latest pipeline status by tasks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var taskID uuid.UUID
		var digest domain.TaskPipelineDigest
		if err := rows.Scan(&taskID, &digest.Status, &digest.GateReason); err != nil {
			return nil, err
		}
		out[taskID] = digest
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListUnfinished returns pipelines still pending/running, oldest first — the
// ones most likely to be stuck are the ones that have been waiting longest, and
// a bounded pass must look at those before it looks at anything else.
//
// Deploy triggers are included: the caller decides what to do with them (the
// gate sweeper skips them — a parked deploy has its own sweeper), and excluding
// them here would make this method a gate-specific query wearing a general name.
func (s *PipelineStore) ListUnfinished(ctx context.Context, limit int) ([]domain.TaskPipeline, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+pipelineCols+` FROM task_pipelines
		WHERE status IN ('pending','running')
		ORDER BY created_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list unfinished pipelines: %w", err)
	}
	return collectPipelines(rows)
}

// ListUnfinishedByHeadSHA returns the repository's unfinished pipelines for one
// commit. Scoped by repository as well as by SHA because a SHA is only unique
// within a repository in practice and never by contract, and a webhook is the
// one caller whose input comes from outside this system.
func (s *PipelineStore) ListUnfinishedByHeadSHA(ctx context.Context, repositoryID uuid.UUID, headSHA string) ([]domain.TaskPipeline, error) {
	if headSHA == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+pipelineCols+` FROM task_pipelines
		WHERE repository_id = $1 AND head_sha = $2 AND status IN ('pending','running')
		ORDER BY created_at ASC
	`, repositoryID, headSHA)
	if err != nil {
		return nil, fmt.Errorf("list unfinished pipelines by head sha: %w", err)
	}
	return collectPipelines(rows)
}

// collectPipelines drains a pipeline row set. Jobs are deliberately NOT loaded:
// both callers are resolving an UNFINISHED pipeline, which by definition has no
// persisted job rows yet (evaluate writes them once, at the terminal state).
func collectPipelines(rows pgx.Rows) ([]domain.TaskPipeline, error) {
	defer rows.Close()
	var out []domain.TaskPipeline
	for rows.Next() {
		p, err := scanPipeline(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SupersedePending marks pending pipelines of the task as failed with note='superseded'.
func (s *PipelineStore) SupersedePending(ctx context.Context, taskID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE task_pipelines SET status = 'failed', note = 'superseded', finished_at = now()
		WHERE task_id = $1 AND status = 'pending'
	`, taskID)
	if err != nil {
		return fmt.Errorf("supersede pending pipelines: %w", err)
	}
	return nil
}

// ClaimTerminal is Update with the transition guarded, and is the only way a
// pipeline reaches a terminal state through finalize.
//
// The WHERE clause is the whole mechanism: 'pending'/'running' -> terminal
// happens once, and the loser of a race between two replicas gets no row back
// and fires no side effects. Same shape as TaskAgentRunStore.CancelIfLive and
// as the four blocked-resource sweepers — the state change and the decision to
// make it are one statement, so there is no window between them.
func (s *PipelineStore) ClaimTerminal(ctx context.Context, p domain.TaskPipeline) (domain.TaskPipeline, bool, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE task_pipelines SET
			status = $2, note = $3, provider = $4, started_at = $5, finished_at = $6,
			coverage_pct = $7, head_sha = $8, gate_reason = $9
		WHERE id = $1 AND status IN ('pending','running')
		RETURNING `+pipelineCols+`
	`, p.ID, string(p.Status), p.Note, p.Provider, p.StartedAt, p.FinishedAt, p.CoveragePct, p.HeadSHA, p.GateReason)
	claimed, err := scanPipeline(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.TaskPipeline{}, false, nil
		}
		return domain.TaskPipeline{}, false, fmt.Errorf("claim terminal pipeline: %w", err)
	}
	return claimed, true, nil
}

// FailStaleRunning marks pending/running pipelines created before cutoff
// (filters on created_at, not started_at — a pending row never had
// started_at set) as failed with note='interrupted'. cutoffMinutes is
// clamped to >= 0: a negative value would make make_interval negative and
// change which side of "now" the comparison lands on.
func (s *PipelineStore) FailStaleRunning(ctx context.Context, cutoffMinutes int) error {
	if cutoffMinutes < 0 {
		cutoffMinutes = 0
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE task_pipelines SET status = 'failed', note = 'interrupted', finished_at = now()
		WHERE status IN ('pending','running') AND created_at < now() - make_interval(mins => $1)
	`, cutoffMinutes)
	if err != nil {
		return fmt.Errorf("fail stale running pipelines: %w", err)
	}
	return nil
}

const jobCols = `id, pipeline_id, name, command, status, exit_code, output, duration_ms, position, run_url, coverage_pct`

func scanJob(row pgx.Row) (domain.TaskPipelineJob, error) {
	var j domain.TaskPipelineJob
	var status string
	if err := row.Scan(
		&j.ID, &j.PipelineID, &j.Name, &j.Command, &status,
		&j.ExitCode, &j.Output, &j.DurationMS, &j.Position, &j.RunURL, &j.CoveragePct,
	); err != nil {
		return domain.TaskPipelineJob{}, err
	}
	j.Status = domain.PipelineJobStatus(status)
	return j, nil
}

func (s *PipelineStore) CreateJob(ctx context.Context, j domain.TaskPipelineJob) (domain.TaskPipelineJob, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO task_pipeline_jobs (pipeline_id, name, command, status, exit_code, output, duration_ms, position, run_url, coverage_pct)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+jobCols,
		j.PipelineID, j.Name, j.Command, string(j.Status), j.ExitCode, j.Output, j.DurationMS, j.Position, j.RunURL, j.CoveragePct)
	out, err := scanJob(row)
	if err != nil {
		return domain.TaskPipelineJob{}, fmt.Errorf("create pipeline job: %w", err)
	}
	return out, nil
}

func (s *PipelineStore) UpdateJob(ctx context.Context, j domain.TaskPipelineJob) (domain.TaskPipelineJob, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE task_pipeline_jobs SET status = $2, exit_code = $3, output = $4, duration_ms = $5, run_url = $6, coverage_pct = $7
		WHERE id = $1
		RETURNING `+jobCols,
		j.ID, string(j.Status), j.ExitCode, j.Output, j.DurationMS, j.RunURL, j.CoveragePct)
	out, err := scanJob(row)
	if err != nil {
		return domain.TaskPipelineJob{}, fmt.Errorf("update pipeline job: %w", err)
	}
	return out, nil
}

func (s *PipelineStore) ListJobs(ctx context.Context, pipelineID uuid.UUID) ([]domain.TaskPipelineJob, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+jobCols+` FROM task_pipeline_jobs
		WHERE pipeline_id = $1 ORDER BY position ASC
	`, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("list pipeline jobs: %w", err)
	}
	defer rows.Close()
	var out []domain.TaskPipelineJob
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// RecentDeploys returns the repository's most recent finished deploy pipelines
// (newest first). The incident engine correlates an outage with them: a deploy
// that landed minutes before a production alert is the first suspect.
func (s *PipelineStore) RecentDeploys(ctx context.Context, repositoryID uuid.UUID, limit int) ([]domain.TaskPipeline, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := s.pool.Query(ctx, `SELECT `+pipelineCols+` FROM task_pipelines
		WHERE repository_id = $1
		  AND trigger IN ('stage_deploy', 'preprod_deploy', 'prod_deploy')
		  AND finished_at IS NOT NULL
		ORDER BY finished_at DESC
		LIMIT $2`, repositoryID, limit)
	if err != nil {
		return nil, fmt.Errorf("recent deploys: %w", err)
	}
	defer rows.Close()
	var out []domain.TaskPipeline
	for rows.Next() {
		p, scanErr := scanPipeline(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan deploy pipeline: %w", scanErr)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
