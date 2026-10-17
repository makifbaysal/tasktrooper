package prodops

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var (
	ErrAlertTokensUnavailable = errors.New("alert tokens are not available")
	// ErrAlertTokenNotFound covers both an unknown token and one that cannot
	// be a token at all, so the ingest route answers both the same way.
	ErrAlertTokenNotFound = errors.New("alert token not found")
	ErrInvalidAlertToken  = errors.New("invalid alert token request")
)

const (
	alertTokenPrefix     = "tt_alert_"
	alertTokenRandBytes  = 32
	alertTokenLabelLimit = 100
)

var alertTokenLength = len(alertTokenPrefix) + base64.RawURLEncoding.EncodedLen(alertTokenRandBytes)

func (s *Service) CreateAlertToken(ctx context.Context, repositoryID uuid.UUID, label, defaultEnv string) (domain.AlertIngestToken, string, error) {
	if s.alertTokens == nil {
		return domain.AlertIngestToken{}, "", ErrAlertTokensUnavailable
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return domain.AlertIngestToken{}, "", fmt.Errorf("%w: label is required", ErrInvalidAlertToken)
	}
	if utf8.RuneCountInString(label) > alertTokenLabelLimit {
		return domain.AlertIngestToken{}, "", fmt.Errorf("%w: label is longer than %d characters", ErrInvalidAlertToken, alertTokenLabelLimit)
	}
	defaultEnv = strings.TrimSpace(defaultEnv)
	if defaultEnv == "" {
		defaultEnv = domain.DeployEnvProd
	}
	if !domain.ValidDeployEnv(defaultEnv) {
		return domain.AlertIngestToken{}, "", fmt.Errorf("%w: unknown default_env %q", ErrInvalidAlertToken, defaultEnv)
	}
	if s.repos != nil {
		if _, err := s.repos.Get(ctx, repositoryID); err != nil {
			return domain.AlertIngestToken{}, "", err
		}
	}
	plaintext, err := newAlertToken()
	if err != nil {
		return domain.AlertIngestToken{}, "", err
	}
	tok, err := s.alertTokens.Create(ctx, repositoryID, label, hashAlertToken(plaintext), defaultEnv)
	if err != nil {
		return domain.AlertIngestToken{}, "", err
	}
	return tok, plaintext, nil
}

func (s *Service) ListAlertTokens(ctx context.Context, repositoryID uuid.UUID) ([]domain.AlertIngestToken, error) {
	if s.alertTokens == nil {
		return nil, ErrAlertTokensUnavailable
	}
	return s.alertTokens.List(ctx, repositoryID)
}

// DeleteAlertToken answers ErrAlertTokenNotFound for a token of another
// repository exactly as for one that does not exist.
func (s *Service) DeleteAlertToken(ctx context.Context, repositoryID, id uuid.UUID) error {
	if s.alertTokens == nil {
		return ErrAlertTokensUnavailable
	}
	err := s.alertTokens.Delete(ctx, repositoryID, id)
	if errors.Is(err, port.ErrNotFound) {
		return ErrAlertTokenNotFound
	}
	return err
}

// AdmitAlertToken finds the token's row by hash and spends one request from
// that token's budget (ErrAlertRateLimited once it is spent). A string that is
// not shaped like a token never reaches the store.
func (s *Service) AdmitAlertToken(ctx context.Context, token string) (domain.AlertIngestToken, error) {
	if s.alertTokens == nil {
		return domain.AlertIngestToken{}, ErrAlertTokenNotFound
	}
	if len(token) != alertTokenLength || !strings.HasPrefix(token, alertTokenPrefix) {
		return domain.AlertIngestToken{}, ErrAlertTokenNotFound
	}
	hash := hashAlertToken(token)
	tok, err := s.alertTokens.FindByHash(ctx, hash)
	if errors.Is(err, port.ErrNotFound) {
		return domain.AlertIngestToken{}, ErrAlertTokenNotFound
	}
	if err != nil {
		return domain.AlertIngestToken{}, err
	}
	if !s.alertGuard.allowRequest(hash, s.now()) {
		return domain.AlertIngestToken{}, ErrAlertRateLimited
	}
	return tok, nil
}

// IngestWithAlertToken is Ingest for the token's repository, except that only
// so many NEW incidents may be opened through one token per rolling hour. Past
// that, an alert that folds into an incident already open still counts its
// occurrence, and a recovery still resolves; only opening is refused.
func (s *Service) IngestWithAlertToken(ctx context.Context, tok domain.AlertIngestToken, in domain.IncidentInput) (domain.Incident, error) {
	in.RepositoryID = tok.RepositoryID
	in = withInputDefaults(in)
	if in.Resolved {
		return s.Ingest(ctx, in)
	}
	_, err := s.incidents.FindLive(ctx, in.RepositoryID, in.Env, in.Fingerprint)
	if err == nil {
		return s.Ingest(ctx, in)
	}
	if !errors.Is(err, domain.ErrIncidentNotFound) {
		return domain.Incident{}, err
	}
	if !s.alertGuard.reserveNewIncident(tok.ID, s.now()) {
		return domain.Incident{}, ErrAlertRateLimited
	}
	incident, created, err := s.ingest(ctx, in)
	if err != nil || !created {
		s.alertGuard.releaseNewIncident(tok.ID)
	}
	return incident, err
}

// MarkAlertTokenUsed is best effort: a failed stamp must not fail an alert
// that was already ingested.
func (s *Service) MarkAlertTokenUsed(ctx context.Context, id uuid.UUID) {
	if s.alertTokens == nil {
		return
	}
	if err := s.alertTokens.MarkUsed(ctx, id); err != nil {
		log.Warn().Err(err).Str("alert_token_id", id.String()).Msg("stamp alert token last use failed")
	}
}

func newAlertToken() (string, error) {
	b := make([]byte, alertTokenRandBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate alert token: %w", err)
	}
	return alertTokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func hashAlertToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
