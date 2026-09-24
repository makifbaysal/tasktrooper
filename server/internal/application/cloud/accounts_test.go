package cloud_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type AccountsSuite struct {
	suite.Suite
	ctx      context.Context
	accounts *fakeAccounts
	envs     *fakeEnvironments
	provider *fakeProvider
	svc      *cloud.Service
}

func TestAccountsSuite(t *testing.T) {
	suite.Run(t, new(AccountsSuite))
}

func (s *AccountsSuite) SetupTest() {
	s.ctx = context.Background()
	s.accounts = newFakeAccounts()
	s.envs = newFakeEnvironments()
	s.provider = &fakeProvider{kind: domain.CloudVercel, verifyMeta: map[string]string{"team_slug": "acme"}}
	s.svc = cloud.NewService(cloud.Deps{
		Accounts:     s.accounts,
		Environments: s.envs,
		Providers:    []port.CloudProvider{s.provider},
		Components:   newFakeComponents(),
		Scans:        newFakeScans(),
		Repos:        newFakeRepos(),
	})
	s.svc.SetBackgroundContext(s.ctx)
}

func (s *AccountsSuite) TestCreateAccountVerifiesFirst() {
	_, err := s.svc.CreateAccount(s.ctx, domain.SaveCloudAccountRequest{
		Provider: domain.CloudVercel,
		Fields:   map[string]string{"token": "tkn"},
	})
	s.Require().NoError(err)
	s.Equal(1, s.provider.getVerifyCalls())
	s.Equal(1, s.accounts.createCount())
}

func (s *AccountsSuite) TestCreateAccountNeverStoresOnAuthFailure() {
	s.provider.verifyErr = port.ErrCloudAuth

	_, err := s.svc.CreateAccount(s.ctx, domain.SaveCloudAccountRequest{
		Provider: domain.CloudVercel,
		Fields:   map[string]string{"token": "bad"},
	})
	s.Require().Error(err)
	s.ErrorIs(err, port.ErrCloudAuth)
	s.ErrorIs(err, cloud.ErrInvalidInput)
	s.Equal(0, s.accounts.createCount())

	accounts, err := s.svc.ListAccounts(s.ctx)
	s.Require().NoError(err)
	s.Empty(accounts)
}

func (s *AccountsSuite) TestCreateAccountRequiresFieldsPerProvider() {
	tests := []struct {
		name     string
		provider domain.CloudProviderKind
		fields   map[string]string
	}{
		{"vercel missing token", domain.CloudVercel, map[string]string{}},
		{"gcp missing service_account_json", domain.CloudGCP, map[string]string{}},
		{"aws missing secret", domain.CloudAWS, map[string]string{"access_key_id": "AKIA"}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, err := s.svc.CreateAccount(s.ctx, domain.SaveCloudAccountRequest{Provider: tt.provider, Fields: tt.fields})
			s.Require().Error(err)
			s.ErrorIs(err, cloud.ErrInvalidInput)
		})
	}
}

func (s *AccountsSuite) TestCreateAccountRejectsUnknownProvider() {
	_, err := s.svc.CreateAccount(s.ctx, domain.SaveCloudAccountRequest{Provider: "azure", Fields: map[string]string{"token": "x"}})
	s.Require().Error(err)
	s.ErrorIs(err, cloud.ErrInvalidInput)
}

func (s *AccountsSuite) TestCreateAccountDefaultsLabelFromMeta() {
	tests := []struct {
		name     string
		provider domain.CloudProviderKind
		meta     map[string]string
		fields   map[string]string
		want     string
	}{
		{"vercel team slug", domain.CloudVercel, map[string]string{"team_slug": "acme-team"}, map[string]string{"token": "t"}, "acme-team"},
		{"vercel username fallback", domain.CloudVercel, map[string]string{"username": "akif"}, map[string]string{"token": "t"}, "akif"},
		{"gcp project id", domain.CloudGCP, map[string]string{"project_id": "proj-1"}, map[string]string{"service_account_json": "{}"}, "proj-1"},
		{"aws account and region", domain.CloudAWS, map[string]string{"account_id": "111", "region": "us-east-1"}, map[string]string{"access_key_id": "a", "secret_access_key": "b", "region": "us-east-1"}, "111 (us-east-1)"},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			provider := &fakeProvider{kind: tt.provider, verifyMeta: tt.meta}
			svc := cloud.NewService(cloud.Deps{
				Accounts:     newFakeAccounts(),
				Environments: newFakeEnvironments(),
				Providers:    []port.CloudProvider{provider},
				Components:   newFakeComponents(),
				Scans:        newFakeScans(),
				Repos:        newFakeRepos(),
			})
			acct, err := svc.CreateAccount(s.ctx, domain.SaveCloudAccountRequest{Provider: tt.provider, Fields: tt.fields})
			s.Require().NoError(err)
			s.Equal(tt.want, acct.Label)
			s.Equal(domain.CloudAccountOK, acct.Status)
			s.Require().NotNil(acct.VerifiedAt)
		})
	}
}

func (s *AccountsSuite) TestCreateAccountRespectsExplicitLabel() {
	acct, err := s.svc.CreateAccount(s.ctx, domain.SaveCloudAccountRequest{
		Provider: domain.CloudVercel, Label: "my label", Fields: map[string]string{"token": "t"},
	})
	s.Require().NoError(err)
	s.Equal("my label", acct.Label)
}

func (s *AccountsSuite) TestVerifyAccountRecordsFailureWithoutError() {
	acct, err := s.svc.CreateAccount(s.ctx, domain.SaveCloudAccountRequest{Provider: domain.CloudVercel, Fields: map[string]string{"token": "t"}})
	s.Require().NoError(err)

	s.provider.verifyErr = errors.New("token expired")
	updated, err := s.svc.VerifyAccount(s.ctx, acct.ID)
	s.Require().NoError(err)
	s.Equal(domain.CloudAccountError, updated.Status)
	s.Equal("token expired", updated.StatusDetail)
}

func (s *AccountsSuite) TestVerifyAccountSuccessTriggersRematch() {
	acct, err := s.svc.CreateAccount(s.ctx, domain.SaveCloudAccountRequest{Provider: domain.CloudVercel, Fields: map[string]string{"token": "t"}})
	s.Require().NoError(err)

	updated, err := s.svc.VerifyAccount(s.ctx, acct.ID)
	s.Require().NoError(err)
	s.Equal(domain.CloudAccountOK, updated.Status)
}

func (s *AccountsSuite) TestUpdateAccountWithoutFieldsSkipsVerify() {
	acct, err := s.svc.CreateAccount(s.ctx, domain.SaveCloudAccountRequest{Provider: domain.CloudVercel, Fields: map[string]string{"token": "t"}})
	s.Require().NoError(err)
	before := s.provider.getVerifyCalls()

	updated, err := s.svc.UpdateAccount(s.ctx, acct.ID, ptr("new label"), nil)
	s.Require().NoError(err)
	s.Equal("new label", updated.Label)
	s.Equal(before, s.provider.getVerifyCalls())
}

func (s *AccountsSuite) TestUpdateAccountWithFieldsReverifies() {
	acct, err := s.svc.CreateAccount(s.ctx, domain.SaveCloudAccountRequest{Provider: domain.CloudVercel, Fields: map[string]string{"token": "t"}})
	s.Require().NoError(err)

	_, err = s.svc.UpdateAccount(s.ctx, acct.ID, nil, map[string]string{"token": "new-token"})
	s.Require().NoError(err)
	s.Equal(2, s.provider.getVerifyCalls())
}

func (s *AccountsSuite) TestUpdateAccountWithFieldsFailureLeavesAccountUnchanged() {
	acct, err := s.svc.CreateAccount(s.ctx, domain.SaveCloudAccountRequest{Provider: domain.CloudVercel, Label: "original", Fields: map[string]string{"token": "t"}})
	s.Require().NoError(err)

	s.provider.verifyErr = errors.New("nope")
	_, err = s.svc.UpdateAccount(s.ctx, acct.ID, nil, map[string]string{"token": "bad"})
	s.Require().Error(err)

	got, err := s.svc.GetAccount(s.ctx, acct.ID)
	s.Require().NoError(err)
	s.Equal("original", got.Label)
	s.Equal(domain.CloudAccountOK, got.Status)
}

func (s *AccountsSuite) TestDeleteAccount() {
	acct, err := s.svc.CreateAccount(s.ctx, domain.SaveCloudAccountRequest{Provider: domain.CloudVercel, Fields: map[string]string{"token": "t"}})
	s.Require().NoError(err)

	s.Require().NoError(s.svc.DeleteAccount(s.ctx, acct.ID))

	_, err = s.svc.GetAccount(s.ctx, acct.ID)
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *AccountsSuite) TestListAccountsNeverReturnsNil() {
	accounts, err := s.svc.ListAccounts(s.ctx)
	s.Require().NoError(err)
	s.NotNil(accounts)
}
