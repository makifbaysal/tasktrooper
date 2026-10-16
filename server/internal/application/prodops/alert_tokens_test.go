package prodops_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type AlertTokenSuite struct {
	suite.Suite
	ctx    context.Context
	store  *mocks.AlertIngestTokenStore
	svc    *prodops.Service
	repoID uuid.UUID
}

func TestAlertTokenSuite(t *testing.T) { suite.Run(t, new(AlertTokenSuite)) }

func (s *AlertTokenSuite) SetupTest() {
	s.ctx = context.Background()
	s.store = mocks.NewAlertIngestTokenStore(s.T())
	s.svc = prodops.NewService(prodops.Deps{AlertTokens: s.store})
	s.repoID = uuid.New()
}

func sha256Hex(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

func (s *AlertTokenSuite) TestCreateStoresOnlyTheHashOfAHighEntropyToken() {
	var storedHash string
	s.store.On("Create", mock.Anything, s.repoID, "Alertmanager", mock.AnythingOfType("string"), domain.DeployEnvProd).
		Run(func(args mock.Arguments) { storedHash = args.String(3) }).
		Return(domain.AlertIngestToken{ID: uuid.New(), RepositoryID: s.repoID, Label: "Alertmanager", DefaultEnv: domain.DeployEnvProd}, nil).
		Once()

	tok, plaintext, err := s.svc.CreateAlertToken(s.ctx, s.repoID, "  Alertmanager ", "")
	s.Require().NoError(err)
	s.Equal(domain.DeployEnvProd, tok.DefaultEnv)
	s.True(strings.HasPrefix(plaintext, "tt_alert_"))
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(plaintext, "tt_alert_"))
	s.Require().NoError(err)
	s.GreaterOrEqual(len(raw), 32)
	s.Equal(sha256Hex(plaintext), storedHash)
	s.NotContains(storedHash, plaintext)
}

func (s *AlertTokenSuite) TestCreateRejectsABadRequest() {
	tests := []struct {
		name       string
		label      string
		defaultEnv string
	}{
		{name: "empty label", label: "  "},
		{name: "label too long", label: strings.Repeat("a", 101)},
		{name: "unknown env", label: "Grafana", defaultEnv: "production"},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, _, err := s.svc.CreateAlertToken(s.ctx, s.repoID, tt.label, tt.defaultEnv)
			s.ErrorIs(err, prodops.ErrInvalidAlertToken)
		})
	}
}

func (s *AlertTokenSuite) TestAdmitLooksTheTokenUpByItsHash() {
	plaintext := "tt_alert_" + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	want := domain.AlertIngestToken{ID: uuid.New(), RepositoryID: s.repoID, DefaultEnv: domain.DeployEnvStage}
	s.store.On("FindByHash", mock.Anything, sha256Hex(plaintext)).Return(want, nil).Once()

	got, err := s.svc.AdmitAlertToken(s.ctx, plaintext)
	s.Require().NoError(err)
	s.Equal(want, got)
}

func (s *AlertTokenSuite) TestAdmitAnUnknownTokenIsNotFound() {
	plaintext := "tt_alert_" + base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
	s.store.On("FindByHash", mock.Anything, sha256Hex(plaintext)).
		Return(domain.AlertIngestToken{}, fmt.Errorf("find alert ingest token: %w", port.ErrNotFound)).Once()

	_, err := s.svc.AdmitAlertToken(s.ctx, plaintext)
	s.ErrorIs(err, prodops.ErrAlertTokenNotFound)
}

func (s *AlertTokenSuite) TestAdmitAStringThatCannotBeATokenNeverReachesTheStore() {
	for _, token := range []string{"", "tt_alert_short", strings.Repeat("a", 52), "sk-bridge-" + strings.Repeat("a", 42)} {
		_, err := s.svc.AdmitAlertToken(s.ctx, token)
		s.ErrorIs(err, prodops.ErrAlertTokenNotFound, token)
	}
	s.store.AssertNotCalled(s.T(), "FindByHash", mock.Anything, mock.Anything)
}

func (s *AlertTokenSuite) TestDeleteATokenOfAnotherRepositoryIsNotFound() {
	id := uuid.New()
	s.store.On("Delete", mock.Anything, s.repoID, id).Return(fmt.Errorf("delete: %w", port.ErrNotFound)).Once()

	s.ErrorIs(s.svc.DeleteAlertToken(s.ctx, s.repoID, id), prodops.ErrAlertTokenNotFound)
}

func (s *AlertTokenSuite) TestMarkUsedFailureIsSwallowed() {
	id := uuid.New()
	s.store.On("MarkUsed", mock.Anything, id).Return(context.DeadlineExceeded).Once()

	s.NotPanics(func() { s.svc.MarkAlertTokenUsed(s.ctx, id) })
}

func (s *AlertTokenSuite) TestWithoutAStoreTokensAreUnavailableAndNoneResolves() {
	svc := prodops.NewService(prodops.Deps{})

	_, _, err := svc.CreateAlertToken(s.ctx, s.repoID, "Grafana", "")
	s.ErrorIs(err, prodops.ErrAlertTokensUnavailable)
	_, err = svc.AdmitAlertToken(s.ctx, "tt_alert_"+base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	s.ErrorIs(err, prodops.ErrAlertTokenNotFound)
}
