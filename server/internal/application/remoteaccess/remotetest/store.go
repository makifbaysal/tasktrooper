// Package remotetest holds an in-memory remoteaccess.Store for tests in this
// package and in the HTTP adapter.
package remotetest

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/remoteaccess"
)

type Store struct {
	mu       sync.Mutex
	Pairings map[uuid.UUID]*remoteaccess.Pairing
	used     map[uuid.UUID]bool
	Devices  map[uuid.UUID]*remoteaccess.Device
	Audit    []remoteaccess.AuditEntry
	settings map[string]string
}

var _ remoteaccess.Store = (*Store)(nil)

func New() *Store {
	return &Store{
		Pairings: map[uuid.UUID]*remoteaccess.Pairing{},
		used:     map[uuid.UUID]bool{},
		Devices:  map[uuid.UUID]*remoteaccess.Device{},
		settings: map[string]string{},
	}
}

func (s *Store) CreatePairing(_ context.Context, p remoteaccess.Pairing) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Pairings[p.ID] = &p
	return nil
}

func (s *Store) CancelPairing(_ context.Context, id uuid.UUID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Pairings[id]; !ok || s.used[id] {
		return false, nil
	}
	s.used[id] = true
	return true, nil
}

func (s *Store) CancelPendingPairings(_ context.Context, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id, p := range s.Pairings {
		if !s.used[id] && p.ExpiresAt.After(now) {
			s.used[id] = true
			n++
		}
	}
	return n, nil
}

func (s *Store) CountPendingPairings(_ context.Context, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id, p := range s.Pairings {
		if !s.used[id] && p.ExpiresAt.After(now) {
			n++
		}
	}
	return n, nil
}

func (s *Store) ConsumePairing(_ context.Context, codeHash string, now time.Time) (remoteaccess.Pairing, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, p := range s.Pairings {
		if p.CodeHash == codeHash && !s.used[id] && p.ExpiresAt.After(now) {
			s.used[id] = true
			return *p, true, nil
		}
	}
	return remoteaccess.Pairing{}, false, nil
}

func (s *Store) CreateDevice(_ context.Context, d remoteaccess.Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Devices[d.ID] = &d
	return nil
}

func (s *Store) DeviceByTokenHash(_ context.Context, hash string) (remoteaccess.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.Devices {
		if d.TokenHash == hash {
			return *d, nil
		}
	}
	return remoteaccess.Device{}, remoteaccess.ErrNotFound
}

func (s *Store) GetDevice(_ context.Context, id uuid.UUID) (remoteaccess.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.Devices[id]; ok {
		return *d, nil
	}
	return remoteaccess.Device{}, remoteaccess.ErrNotFound
}

func (s *Store) ListDevices(_ context.Context, revokedSince time.Time) ([]remoteaccess.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []remoteaccess.Device{}
	for _, d := range s.Devices {
		if d.RevokedAt == nil || d.RevokedAt.After(revokedSince) {
			out = append(out, *d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) CountActiveDevices(_ context.Context, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, d := range s.Devices {
		if d.RevokedAt == nil && (d.ExpiresAt == nil || d.ExpiresAt.After(now)) {
			n++
		}
	}
	return n, nil
}

func (s *Store) UpdateDevice(_ context.Context, id uuid.UUID, name *string, role *remoteaccess.Role) (remoteaccess.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.Devices[id]
	if !ok {
		return remoteaccess.Device{}, remoteaccess.ErrNotFound
	}
	if name != nil {
		d.Name = *name
	}
	if role != nil {
		d.Role = *role
	}
	return *d, nil
}

func (s *Store) RevokeDevice(_ context.Context, id uuid.UUID, at time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.Devices[id]
	if !ok || d.RevokedAt != nil {
		return false, nil
	}
	d.RevokedAt = &at
	return true, nil
}

func (s *Store) TouchDevice(_ context.Context, id uuid.UUID, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.Devices[id]; ok {
		d.LastSeenAt = &at
	}
	return nil
}

func (s *Store) AppendAudit(_ context.Context, e remoteaccess.AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e.ID = int64(len(s.Audit) + 1)
	s.Audit = append(s.Audit, e)
	return nil
}

func (s *Store) ListAudit(_ context.Context, deviceID *uuid.UUID, limit int) ([]remoteaccess.AuditEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []remoteaccess.AuditEntry{}
	for i := len(s.Audit) - 1; i >= 0 && len(out) < limit; i-- {
		e := s.Audit[i]
		if deviceID == nil || (e.DeviceID != nil && *e.DeviceID == *deviceID) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *Store) Setting(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.settings[key]
	return v, ok, nil
}

func (s *Store) SetSetting(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings[key] = value
	return nil
}

func (s *Store) AuditKinds() []remoteaccess.AuditKind {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]remoteaccess.AuditKind, len(s.Audit))
	for i, e := range s.Audit {
		out[i] = e.Kind
	}
	return out
}
