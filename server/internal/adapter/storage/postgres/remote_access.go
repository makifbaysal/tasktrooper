package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/application/remoteaccess"
)

type RemoteAccessStore struct{ pool *DB }

func NewRemoteAccessStore(pool *DB) *RemoteAccessStore { return &RemoteAccessStore{pool: pool} }

var _ remoteaccess.Store = (*RemoteAccessStore)(nil)

const remoteDeviceColumns = `id, name, platform, role, token_hash, created_at, last_seen_at, expires_at, revoked_at`

func scanRemoteDevice(row pgx.Row) (remoteaccess.Device, error) {
	var d remoteaccess.Device
	var role string
	err := row.Scan(&d.ID, &d.Name, &d.Platform, &role, &d.TokenHash, &d.CreatedAt, &d.LastSeenAt, &d.ExpiresAt, &d.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, remoteaccess.ErrNotFound
	}
	d.Role = remoteaccess.Role(role)
	return d, err
}

func (s *RemoteAccessStore) CreatePairing(ctx context.Context, p remoteaccess.Pairing) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO remote_pairings
		(id, code_hash, role, expires_in_days, device_label, created_at, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		p.ID, p.CodeHash, string(p.Role), p.ExpiresInDays, p.DeviceLabel, p.CreatedAt, p.ExpiresAt)
	return err
}

func (s *RemoteAccessStore) CancelPairing(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE remote_pairings SET cancelled_at = now()
		WHERE id = $1 AND used_at IS NULL AND cancelled_at IS NULL`, id)
	return tag.RowsAffected() > 0, err
}

func (s *RemoteAccessStore) CountPendingPairings(ctx context.Context, now time.Time) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM remote_pairings
		WHERE used_at IS NULL AND cancelled_at IS NULL AND expires_at > $1`, now).Scan(&n)
	return n, err
}

func (s *RemoteAccessStore) ConsumePairing(ctx context.Context, codeHash string, now time.Time) (remoteaccess.Pairing, bool, error) {
	var p remoteaccess.Pairing
	var role string
	err := s.pool.QueryRow(ctx, `UPDATE remote_pairings SET used_at = $2
		WHERE code_hash = $1 AND used_at IS NULL AND cancelled_at IS NULL AND expires_at > $2
		RETURNING id, code_hash, role, expires_in_days, device_label, created_at, expires_at`,
		codeHash, now).Scan(&p.ID, &p.CodeHash, &role, &p.ExpiresInDays, &p.DeviceLabel, &p.CreatedAt, &p.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, false, nil
	}
	p.Role = remoteaccess.Role(role)
	return p, err == nil, err
}

func (s *RemoteAccessStore) CreateDevice(ctx context.Context, d remoteaccess.Device) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO remote_devices
		(id, name, platform, role, token_hash, created_at, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		d.ID, d.Name, d.Platform, string(d.Role), d.TokenHash, d.CreatedAt, d.ExpiresAt)
	return err
}

func (s *RemoteAccessStore) DeviceByTokenHash(ctx context.Context, hash string) (remoteaccess.Device, error) {
	return scanRemoteDevice(s.pool.QueryRow(ctx, `SELECT `+remoteDeviceColumns+` FROM remote_devices WHERE token_hash = $1`, hash))
}

func (s *RemoteAccessStore) GetDevice(ctx context.Context, id uuid.UUID) (remoteaccess.Device, error) {
	return scanRemoteDevice(s.pool.QueryRow(ctx, `SELECT `+remoteDeviceColumns+` FROM remote_devices WHERE id = $1`, id))
}

func (s *RemoteAccessStore) ListDevices(ctx context.Context, revokedSince time.Time) ([]remoteaccess.Device, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+remoteDeviceColumns+` FROM remote_devices
		WHERE revoked_at IS NULL OR revoked_at > $1 ORDER BY created_at DESC`, revokedSince)
	if err != nil {
		return nil, fmt.Errorf("list remote devices: %w", err)
	}
	defer rows.Close()
	out := []remoteaccess.Device{}
	for rows.Next() {
		d, err := scanRemoteDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *RemoteAccessStore) CountActiveDevices(ctx context.Context, now time.Time) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM remote_devices
		WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > $1)`, now).Scan(&n)
	return n, err
}

func (s *RemoteAccessStore) UpdateDevice(ctx context.Context, id uuid.UUID, name *string, role *remoteaccess.Role) (remoteaccess.Device, error) {
	var roleArg *string
	if role != nil {
		r := string(*role)
		roleArg = &r
	}
	return scanRemoteDevice(s.pool.QueryRow(ctx, `UPDATE remote_devices
		SET name = COALESCE($2, name), role = COALESCE($3, role)
		WHERE id = $1 RETURNING `+remoteDeviceColumns, id, name, roleArg))
}

func (s *RemoteAccessStore) RevokeDevice(ctx context.Context, id uuid.UUID, at time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE remote_devices SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL`, id, at)
	return tag.RowsAffected() > 0, err
}

func (s *RemoteAccessStore) TouchDevice(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE remote_devices SET last_seen_at = $2 WHERE id = $1`, id, at)
	return err
}

func (s *RemoteAccessStore) AppendAudit(ctx context.Context, e remoteaccess.AuditEntry) error {
	if _, err := s.pool.Exec(ctx, `INSERT INTO remote_audit (at, device_id, device_name, method, route, status, kind)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, e.At, e.DeviceID, e.DeviceName, e.Method, e.Route, e.Status, string(e.Kind)); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM remote_audit WHERE id < (SELECT max(id) FROM remote_audit) - 5000`)
	return err
}

func (s *RemoteAccessStore) ListAudit(ctx context.Context, deviceID *uuid.UUID, limit int) ([]remoteaccess.AuditEntry, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, at, device_id, device_name, method, route, status, kind
		FROM remote_audit WHERE ($1::uuid IS NULL OR device_id = $1) ORDER BY id DESC LIMIT $2`, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []remoteaccess.AuditEntry{}
	for rows.Next() {
		var e remoteaccess.AuditEntry
		var kind string
		if err := rows.Scan(&e.ID, &e.At, &e.DeviceID, &e.DeviceName, &e.Method, &e.Route, &e.Status, &kind); err != nil {
			return nil, err
		}
		e.Kind = remoteaccess.AuditKind(kind)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *RemoteAccessStore) Setting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.pool.QueryRow(ctx, `SELECT value FROM remote_settings WHERE key = $1`, key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// SetSetting avoids ON CONFLICT (key): the company edition re-keys every table
// by tenant, and a single-column conflict target would stop applying there.
func (s *RemoteAccessStore) SetSetting(ctx context.Context, key, value string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE remote_settings SET value = $2 WHERE key = $1`, key, value)
	if err != nil || tag.RowsAffected() > 0 {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO remote_settings (key, value) VALUES ($1,$2)`, key, value)
	return err
}
