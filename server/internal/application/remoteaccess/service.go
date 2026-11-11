// Package remoteaccess pairs phones with this computer and authenticates them.
//
// A phone reaches the local server through a tunnel; everything that decides
// whether it may is here: the short-lived pairing code, the long-lived device
// token (only its SHA-256 is stored), roles, expiry, revocation, rate limits
// and the audit trail. Which routes a device may call is the HTTP adapter's.
package remoteaccess

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

const (
	TokenPrefix = "ttd_"

	CodeAlphabet  = "ABCDEFGHJKMNPQRSTVWXYZ23456789"
	CodeLength    = 12
	PairingTTL    = 5 * time.Minute
	MaxExpiryDays = 365

	DeviceRequestsPerMinute = 120
	PairFailuresPerMinute   = 10
	AuthFailuresPerMinute   = 60

	touchInterval   = 30 * time.Second
	revokedKeepFor  = 30 * 24 * time.Hour
	auditKeep       = 5000
	auditPruneEvery = 200
	serverNameKey   = "server_name"
	maxNameLength   = 80
)

type Service struct {
	store   Store
	now     func() time.Time
	version string

	mu          sync.Mutex
	pairFails   window
	authFails   window
	perDevice   map[uuid.UUID]*window
	lastTouch   map[uuid.UUID]time.Time
	auditWrites int
	lastAuthLog time.Time
	reqLog      []time.Time
}

func New(store Store, version string) *Service {
	return &Service{
		store:     store,
		now:       time.Now,
		version:   version,
		perDevice: map[uuid.UUID]*window{},
		lastTouch: map[uuid.UUID]time.Time{},
	}
}

func (s *Service) SetClock(now func() time.Time) { s.now = now }

func (s *Service) Version() string { return s.version }

func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func NormalizeCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.NewReplacer("-", "", " ", "").Replace(code)
	return code
}

func DisplayCode(code string) string {
	var parts []string
	for i := 0; i < len(code); i += 4 {
		end := i + 4
		if end > len(code) {
			end = len(code)
		}
		parts = append(parts, code[i:end])
	}
	return strings.Join(parts, "-")
}

func newCode() (string, error) {
	max := big.NewInt(int64(len(CodeAlphabet)))
	out := make([]byte, CodeLength)
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = CodeAlphabet[n.Int64()]
	}
	return string(out), nil
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

type CreatedPairing struct {
	ID        uuid.UUID
	Code      string
	ExpiresAt time.Time
}

func (s *Service) CreatePairing(ctx context.Context, role Role, expiresInDays int, label string) (CreatedPairing, error) {
	if !role.Valid() {
		return CreatedPairing{}, fmt.Errorf("%w: role must be operator or viewer", ErrInvalidArgument)
	}
	if expiresInDays < 0 || expiresInDays > MaxExpiryDays {
		return CreatedPairing{}, fmt.Errorf("%w: expires_in_days must be 0..%d", ErrInvalidArgument, MaxExpiryDays)
	}
	code, err := newCode()
	if err != nil {
		return CreatedPairing{}, err
	}
	now := s.now()
	p := Pairing{
		ID:            uuid.New(),
		CodeHash:      HashSecret(code),
		Role:          role,
		ExpiresInDays: expiresInDays,
		DeviceLabel:   clip(label),
		CreatedAt:     now,
		ExpiresAt:     now.Add(PairingTTL),
	}
	if err := s.store.CreatePairing(ctx, p); err != nil {
		return CreatedPairing{}, err
	}
	return CreatedPairing{ID: p.ID, Code: code, ExpiresAt: p.ExpiresAt}, nil
}

func (s *Service) CancelPairing(ctx context.Context, id uuid.UUID) error {
	ok, err := s.store.CancelPairing(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// Exchange trades a pairing code for a device token. The token is returned
// once; only its hash is kept.
func (s *Service) Exchange(ctx context.Context, code, deviceName, platform string) (string, Device, error) {
	now := s.now()
	if retry := s.pairBlocked(now); retry > 0 {
		s.Audit(ctx, AuditEntry{Kind: AuditRateLimited, Method: "POST", Route: "/remote/v1/pair", Status: 429})
		return "", Device{}, &RateLimitError{RetryAfter: retry}
	}
	code = NormalizeCode(code)
	deviceName = clip(deviceName)
	if code == "" || deviceName == "" {
		return "", Device{}, fmt.Errorf("%w: code and device_name are required", ErrInvalidArgument)
	}
	p, ok, err := s.store.ConsumePairing(ctx, HashSecret(code), now)
	if err != nil {
		return "", Device{}, err
	}
	if !ok {
		s.notePairFailure(now)
		s.Audit(ctx, AuditEntry{Kind: AuditPairFailed, Method: "POST", Route: "/remote/v1/pair", Status: 401})
		return "", Device{}, ErrInvalidPairing
	}
	token, err := newToken()
	if err != nil {
		return "", Device{}, err
	}
	d := Device{
		ID:        uuid.New(),
		Name:      deviceName,
		Platform:  clip(platform),
		Role:      p.Role,
		CreatedAt: now,
		TokenHash: HashSecret(token),
	}
	if p.ExpiresInDays > 0 {
		exp := now.Add(time.Duration(p.ExpiresInDays) * 24 * time.Hour)
		d.ExpiresAt = &exp
	}
	if err := s.store.CreateDevice(ctx, d); err != nil {
		return "", Device{}, err
	}
	s.Audit(ctx, AuditEntry{Kind: AuditPair, DeviceID: &d.ID, DeviceName: d.Name, Method: "POST", Route: "/remote/v1/pair", Status: 200})
	return token, d, nil
}

// Authenticate resolves a bearer token to a live device.
func (s *Service) Authenticate(ctx context.Context, token string) (Device, error) {
	if !strings.HasPrefix(token, TokenPrefix) || len(token) > 128 {
		return Device{}, ErrUnauthorized
	}
	d, err := s.store.DeviceByTokenHash(ctx, HashSecret(token))
	if err != nil {
		if err == ErrNotFound {
			return Device{}, ErrUnauthorized
		}
		return Device{}, err
	}
	now := s.now()
	if d.RevokedAt != nil {
		return d, ErrTokenRevoked
	}
	if d.ExpiresAt != nil && !now.Before(*d.ExpiresAt) {
		return d, ErrTokenExpired
	}
	s.touch(ctx, d.ID, now)
	return d, nil
}

func (s *Service) touch(ctx context.Context, id uuid.UUID, now time.Time) {
	s.mu.Lock()
	last := s.lastTouch[id]
	due := now.Sub(last) >= touchInterval
	if due {
		s.lastTouch[id] = now
	}
	s.mu.Unlock()
	if due {
		if err := s.store.TouchDevice(ctx, id, now); err != nil {
			log.Debug().Err(err).Msg("remote access: touch device")
		}
	}
}

func (s *Service) Devices(ctx context.Context) ([]Device, error) {
	return s.store.ListDevices(ctx, s.now().Add(-revokedKeepFor))
}

func (s *Service) Device(ctx context.Context, id uuid.UUID) (Device, error) {
	return s.store.GetDevice(ctx, id)
}

func (s *Service) UpdateDevice(ctx context.Context, id uuid.UUID, name *string, role *Role) (Device, error) {
	if name != nil {
		n := clip(*name)
		if n == "" {
			return Device{}, fmt.Errorf("%w: name is empty", ErrInvalidArgument)
		}
		name = &n
	}
	if role != nil && !role.Valid() {
		return Device{}, fmt.Errorf("%w: role must be operator or viewer", ErrInvalidArgument)
	}
	return s.store.UpdateDevice(ctx, id, name, role)
}

// Revoke makes the device's token worthless immediately: Authenticate looks
// the row up on every request, nothing is cached.
func (s *Service) Revoke(ctx context.Context, id uuid.UUID) error {
	d, err := s.store.GetDevice(ctx, id)
	if err != nil {
		return err
	}
	ok, err := s.store.RevokeDevice(ctx, id, s.now())
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	s.Audit(ctx, AuditEntry{Kind: AuditRevoke, DeviceID: &d.ID, DeviceName: d.Name, Method: "DELETE", Route: "/v1/remote-access/devices/:id", Status: 204})
	return nil
}

func (s *Service) Counts(ctx context.Context) (devices, pending int, err error) {
	now := s.now()
	if devices, err = s.store.CountActiveDevices(ctx, now); err != nil {
		return
	}
	pending, err = s.store.CountPendingPairings(ctx, now)
	return
}

func (s *Service) ServerName(ctx context.Context) string {
	if v, ok, err := s.store.Setting(ctx, serverNameKey); err == nil && ok && v != "" {
		return v
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "TaskTrooper"
}

func (s *Service) SetServerName(ctx context.Context, name string) error {
	name = clip(name)
	if name == "" {
		return fmt.Errorf("%w: server_name is empty", ErrInvalidArgument)
	}
	return s.store.SetSetting(ctx, serverNameKey, name)
}

// Audit is best effort: a failing audit write must not fail the request it
// describes, and must not be silent either.
func (s *Service) Audit(ctx context.Context, e AuditEntry) {
	if e.At.IsZero() {
		e.At = s.now()
	}
	if e.Kind == AuditAuthFailed {
		s.mu.Lock()
		skip := e.At.Sub(s.lastAuthLog) < 10*time.Second
		if !skip {
			s.lastAuthLog = e.At
		}
		s.mu.Unlock()
		if skip {
			return
		}
	}
	if err := s.store.AppendAudit(ctx, e); err != nil {
		log.Warn().Err(err).Msg("remote access: audit write failed")
	}
}

func (s *Service) AuditEntries(ctx context.Context, deviceID *uuid.UUID, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return s.store.ListAudit(ctx, deviceID, limit)
}

// RequestsLastMinute backs the tunnel health hint in the desktop.
func (s *Service) NoteRequest() {
	now := s.now()
	s.mu.Lock()
	s.reqLog = append(s.reqLog, now)
	s.trimReqLog(now)
	s.mu.Unlock()
}

func (s *Service) trimReqLog(now time.Time) {
	cut := now.Add(-time.Minute)
	i := 0
	for i < len(s.reqLog) && s.reqLog[i].Before(cut) {
		i++
	}
	s.reqLog = s.reqLog[i:]
}

func (s *Service) Live() (perMinute int, last *time.Time) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trimReqLog(now)
	if n := len(s.reqLog); n > 0 {
		t := s.reqLog[n-1]
		last = &t
	}
	return len(s.reqLog), last
}

type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string { return ErrRateLimited.Error() }
func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

// Allow charges one request to the device's per-minute budget.
func (s *Service) Allow(deviceID uuid.UUID) (bool, time.Duration) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.perDevice[deviceID]
	if w == nil {
		w = &window{}
		s.perDevice[deviceID] = w
	}
	if w.count(now) >= DeviceRequestsPerMinute {
		return false, w.retryAfter(now)
	}
	w.add(now)
	return true, 0
}

// AuthFailureBlocked reports whether the listener as a whole has seen too many
// bad tokens in the last minute, which turns further guessing into 429s.
func (s *Service) AuthFailureBlocked() (bool, time.Duration) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.authFails.count(now) >= AuthFailuresPerMinute {
		return true, s.authFails.retryAfter(now)
	}
	return false, 0
}

func (s *Service) NoteAuthFailure() {
	now := s.now()
	s.mu.Lock()
	s.authFails.add(now)
	s.mu.Unlock()
}

func (s *Service) pairBlocked(now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pairFails.count(now) >= PairFailuresPerMinute {
		return s.pairFails.retryAfter(now)
	}
	return 0
}

func (s *Service) notePairFailure(now time.Time) {
	s.mu.Lock()
	s.pairFails.add(now)
	s.mu.Unlock()
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > maxNameLength {
		s = string(r[:maxNameLength])
	}
	return s
}

type window struct{ at []time.Time }

func (w *window) trim(now time.Time) {
	cut := now.Add(-time.Minute)
	i := 0
	for i < len(w.at) && !w.at[i].After(cut) {
		i++
	}
	w.at = w.at[i:]
}

func (w *window) count(now time.Time) int { w.trim(now); return len(w.at) }
func (w *window) add(now time.Time)       { w.trim(now); w.at = append(w.at, now) }
func (w *window) retryAfter(now time.Time) time.Duration {
	if len(w.at) == 0 {
		return time.Second
	}
	d := w.at[0].Add(time.Minute).Sub(now)
	if d < time.Second {
		d = time.Second
	}
	return d
}
