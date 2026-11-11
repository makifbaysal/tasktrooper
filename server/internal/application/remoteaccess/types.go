package remoteaccess

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

type Role string

const (
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

func (r Role) Valid() bool { return r == RoleOperator || r == RoleViewer }

type Device struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Platform   string     `json:"platform"`
	Role       Role       `json:"role"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	TokenHash  string     `json:"-"`
}

type Pairing struct {
	ID            uuid.UUID
	CodeHash      string
	Role          Role
	ExpiresInDays int
	DeviceLabel   string
	CreatedAt     time.Time
	ExpiresAt     time.Time
}

type AuditKind string

const (
	AuditRequest     AuditKind = "request"
	AuditAuthFailed  AuditKind = "auth_failed"
	AuditPair        AuditKind = "pair"
	AuditPairFailed  AuditKind = "pair_failed"
	AuditRevoke      AuditKind = "revoke"
	AuditRateLimited AuditKind = "rate_limited"
)

type AuditEntry struct {
	ID         int64      `json:"id"`
	At         time.Time  `json:"at"`
	DeviceID   *uuid.UUID `json:"device_id"`
	DeviceName string     `json:"device_name"`
	Method     string     `json:"method"`
	Route      string     `json:"route"`
	Status     int        `json:"status"`
	Kind       AuditKind  `json:"kind"`
}

var (
	ErrNotFound        = errors.New("remote access: not found")
	ErrUnauthorized    = errors.New("remote access: unauthorized")
	ErrTokenExpired    = errors.New("remote access: token expired")
	ErrTokenRevoked    = errors.New("remote access: token revoked")
	ErrInvalidPairing  = errors.New("remote access: invalid pairing code")
	ErrRateLimited     = errors.New("remote access: rate limited")
	ErrInvalidArgument = errors.New("remote access: invalid argument")
)

type Store interface {
	CreatePairing(ctx context.Context, p Pairing) error
	CancelPairing(ctx context.Context, id uuid.UUID) (bool, error)
	// CancelPendingPairings cancels every live, unused pairing and says how many.
	CancelPendingPairings(ctx context.Context, now time.Time) (int, error)
	CountPendingPairings(ctx context.Context, now time.Time) (int, error)
	// ConsumePairing marks the live pairing with this code hash used and
	// returns it; ok is false when none is live. Atomic, so one code can never
	// be exchanged twice.
	ConsumePairing(ctx context.Context, codeHash string, now time.Time) (p Pairing, ok bool, err error)

	CreateDevice(ctx context.Context, d Device) error
	DeviceByTokenHash(ctx context.Context, hash string) (Device, error)
	GetDevice(ctx context.Context, id uuid.UUID) (Device, error)
	ListDevices(ctx context.Context, revokedSince time.Time) ([]Device, error)
	CountActiveDevices(ctx context.Context, now time.Time) (int, error)
	UpdateDevice(ctx context.Context, id uuid.UUID, name *string, role *Role) (Device, error)
	RevokeDevice(ctx context.Context, id uuid.UUID, at time.Time) (bool, error)
	TouchDevice(ctx context.Context, id uuid.UUID, at time.Time) error

	AppendAudit(ctx context.Context, e AuditEntry) error
	ListAudit(ctx context.Context, deviceID *uuid.UUID, limit int) ([]AuditEntry, error)

	Setting(ctx context.Context, key string) (string, bool, error)
	SetSetting(ctx context.Context, key, value string) error
}
