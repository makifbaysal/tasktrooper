package storeops_test

import (
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func (s *ServiceSuite) TestStoreCredentialHandsTheDataSourcesTheDecryptedCredential() {
	_, err := s.svc.StoreCredential(s.T().Context(), domain.StoreCredentialASC)
	s.ErrorIs(err, port.ErrNotFound, "none saved yet")

	s.Require().NoError(s.svc.SaveCredential(s.T().Context(), domain.StoreCredentialASC, s.ascCredential()))
	cred, err := s.svc.StoreCredential(s.T().Context(), domain.StoreCredentialASC)
	s.Require().NoError(err)
	s.Equal(domain.StoreCredentialASC, cred.Provider)
	s.Equal(s.ascCredential(), cred.Data)

	_, err = s.svc.StoreCredential(s.T().Context(), "app_store")
	s.ErrorIs(err, port.ErrNotFound)
}
