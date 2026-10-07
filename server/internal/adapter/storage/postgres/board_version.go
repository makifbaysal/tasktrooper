package postgres

import (
	"context"
	"regexp"
	"sync"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// boardVersion lets GET /v1/tasks answer a conditional request without
// re-reading the board. It moves after every committed statement that could
// change what the board list returns, so a version equal to the one read before
// a list was built proves that list is still current.
//
// It is bumped by DB itself, not by the stores that happen to write board rows
// today: a write added later anywhere in this package is covered without anyone
// remembering to call a hook. Two rules keep it correct:
//   - it is bumped after the statement or transaction commits, never before,
//     and a reader takes the version before it reads the rows; a list built
//     from older rows is therefore always tagged with an older version.
//   - every DELETE and TRUNCATE counts, whatever table it names, because
//     ON DELETE CASCADE / SET NULL rewrite board rows from statements that never
//     mention them.
//
// An in-memory counter sees this process's writes only. Another host serving
// the same database (see hostRoots) moves nothing here, so a reader must not
// trust an unchanged version indefinitely; the /v1/tasks cache re-reads after
// a short maximum age.
var boardVersion atomic.Uint64

var (
	boardWriteRe     = regexp.MustCompile(`(?i)\b(insert|update|merge)\b`)
	cascadingWriteRe = regexp.MustCompile(`(?i)\b(delete|truncate)\b`)
	// Every table GET /v1/tasks reads: the board_tasks row, the open column
	// span, the runs (quota resume time, agent_running), the task type's key
	// prefix and the latest pipeline status.
	boardTableRe = regexp.MustCompile(`(?i)\b(board_tasks|task_column_spans|task_agent_runs|task_types|task_pipelines)\b`)
)

func changesBoard(sql string) bool {
	if cascadingWriteRe.MatchString(sql) {
		return true
	}
	return boardWriteRe.MatchString(sql) && boardTableRe.MatchString(sql)
}

func batchChangesBoard(b *pgx.Batch) bool {
	for _, q := range b.QueuedQueries {
		if changesBoard(q.SQL) {
			return true
		}
	}
	return false
}

func bumpBoardVersion() { boardVersion.Add(1) }

// BoardVersion is the current board version; see boardVersion.
func (s *BoardTaskStore) BoardVersion() uint64 { return boardVersion.Load() }

type bumpingRows struct {
	pgx.Rows
	once sync.Once
}

func (r *bumpingRows) Next() bool {
	if r.Rows.Next() {
		return true
	}
	r.once.Do(bumpBoardVersion)
	return false
}

func (r *bumpingRows) Close() {
	r.Rows.Close()
	r.once.Do(bumpBoardVersion)
}

type bumpingRow struct {
	pgx.Row
}

func (r bumpingRow) Scan(dest ...any) error {
	err := r.Row.Scan(dest...)
	bumpBoardVersion()
	return err
}

type bumpingBatchResults struct {
	pgx.BatchResults
	once sync.Once
}

func (r *bumpingBatchResults) Close() error {
	err := r.BatchResults.Close()
	r.once.Do(bumpBoardVersion)
	return err
}

// boardTx defers the bump to Commit: until then no other session can see the
// transaction's writes. A pseudo-nested transaction's statements are not
// inspected, so opening one marks the outer transaction as changing the board.
type boardTx struct {
	pgx.Tx
	dirty atomic.Bool
}

func (t *boardTx) note(sql string) {
	if changesBoard(sql) {
		t.dirty.Store(true)
	}
}

func (t *boardTx) Begin(ctx context.Context) (pgx.Tx, error) {
	t.dirty.Store(true)
	return t.Tx.Begin(ctx)
}

func (t *boardTx) Commit(ctx context.Context) error {
	err := t.Tx.Commit(ctx)
	if err == nil && t.dirty.Load() {
		bumpBoardVersion()
	}
	return err
}

func (t *boardTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	t.dirty.Store(true)
	return t.Tx.CopyFrom(ctx, tableName, columnNames, rowSrc)
}

func (t *boardTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	if batchChangesBoard(b) {
		t.dirty.Store(true)
	}
	return t.Tx.SendBatch(ctx, b)
}

func (t *boardTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	t.note(sql)
	return t.Tx.Exec(ctx, sql, arguments...)
}

func (t *boardTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	t.note(sql)
	return t.Tx.Query(ctx, sql, args...)
}

func (t *boardTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	t.note(sql)
	return t.Tx.QueryRow(ctx, sql, args...)
}
