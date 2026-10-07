package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB is the handle every store holds: the calls this package makes on a
// *pgxpool.Pool, plus InTx.
type DB struct {
	pool *pgxpool.Pool
}

func NewDB(pool *pgxpool.Pool) *DB { return &DB{pool: pool} }

func (d *DB) Close() { d.pool.Close() }

func (d *DB) Ping(ctx context.Context) error { return d.pool.Ping(ctx) }

func (d *DB) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &boardTx{Tx: tx}, nil
}

// InTx runs fn inside one transaction, committing on nil and rolling back on
// anything else. Preferred over Begin: it cannot leak a transaction on an early
// return.
func (d *DB) InTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := d.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Exec, Query, QueryRow and SendBatch run outside a transaction, so a write
// is committed once its results are drained; that is when boardVersion moves.
func (d *DB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tag, err := d.pool.Exec(ctx, sql, args...)
	if changesBoard(sql) {
		bumpBoardVersion()
	}
	return tag, err
}

func (d *DB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := d.pool.Query(ctx, sql, args...)
	if !changesBoard(sql) {
		return rows, err
	}
	if err != nil {
		bumpBoardVersion()
		return rows, err
	}
	return &bumpingRows{Rows: rows}, nil
}

func (d *DB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	row := d.pool.QueryRow(ctx, sql, args...)
	if changesBoard(sql) {
		return bumpingRow{Row: row}
	}
	return row
}

// SendBatch keeps pgx's semantics: the queued statements run as one implicit
// transaction.
func (d *DB) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	results := d.pool.SendBatch(ctx, b)
	if batchChangesBoard(b) {
		return &bumpingBatchResults{BatchResults: results}
	}
	return results
}
