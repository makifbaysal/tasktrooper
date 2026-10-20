package datasource

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// SyncCloudAccounts makes the data source of every connected cloud account
// that lacks one (Vercel, Google Cloud, AWS), so a connection needs no
// separate "add data source" step. A pair already made, or deleted by hand, is
// left alone, and a source a person added themselves for the account counts as
// made. It is safe to call as often as wanted.
func (s *Service) SyncCloudAccounts(ctx context.Context) {
	if s.cloudList == nil || s.cloudLinks == nil {
		return
	}
	accounts, err := s.cloudList.ListCloudAccounts(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("data sources: listing cloud accounts failed; none were synced")
		return
	}
	for _, acct := range accounts {
		s.CloudAccountConnected(ctx, acct)
	}
}

// CloudAccountConnected makes the account's data source unless it has one or
// a person deleted it.
func (s *Service) CloudAccountConnected(ctx context.Context, acct domain.CloudAccount) {
	kind := domain.AutoDataSourceKind(acct.Provider)
	if kind == "" || s.cloudLinks == nil {
		return
	}
	if _, ok := s.kinds[kind]; !ok {
		return
	}
	s.provisionMu.Lock()
	defer s.provisionMu.Unlock()
	if err := s.provision(ctx, acct, kind); err != nil {
		log.Warn().Err(err).Str("cloud_account", acct.ID.String()).Str("kind", string(kind)).Msg("data sources: making the source of a cloud account failed")
	}
}

func (s *Service) provision(ctx context.Context, acct domain.CloudAccount, kind domain.DataSourceKind) error {
	links, err := s.cloudLinks.ListCloudSourceLinks(ctx)
	if err != nil {
		return err
	}
	for _, l := range links {
		if l.CloudAccountID == acct.ID && l.Kind == kind {
			return nil
		}
	}
	existing, err := s.store.ListDataSources(ctx)
	if err != nil {
		return err
	}
	for _, ds := range existing {
		if ds.Kind == kind && ds.Config[cloudAccountKey] == acct.ID.String() {
			_, err := s.cloudLinks.LinkCloudSource(ctx, acct.ID, kind, ds.ID)
			return err
		}
	}
	created, err := s.Create(ctx, domain.SaveDataSourceRequest{
		Kind:   kind,
		Config: map[string]string{cloudAccountKey: acct.ID.String()},
	})
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	linked, err := s.cloudLinks.LinkCloudSource(ctx, acct.ID, kind, created.ID)
	if err != nil || !linked {
		if delErr := s.Delete(ctx, created.ID); delErr != nil && !errors.Is(delErr, port.ErrNotFound) {
			log.Warn().Err(delErr).Msg("data sources: removing a source that lost the race failed")
		}
		return err
	}
	return nil
}

// CloudAccountRemoved deletes the data sources made for the account on its
// own, with the signal bindings and alert rules that read them. It runs
// before the account is deleted, while the links still exist; a source a
// person added themselves is kept and reports the account missing.
func (s *Service) CloudAccountRemoved(ctx context.Context, accountID uuid.UUID) {
	if s.cloudLinks == nil {
		return
	}
	s.provisionMu.Lock()
	defer s.provisionMu.Unlock()
	links, err := s.cloudLinks.ListCloudSourceLinks(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("data sources: listing the links of a removed cloud account failed")
		return
	}
	for _, l := range links {
		if l.CloudAccountID != accountID || l.DataSourceID == nil {
			continue
		}
		if err := s.Delete(ctx, *l.DataSourceID); err != nil && !errors.Is(err, port.ErrNotFound) {
			log.Warn().Err(err).Str("data_source", l.DataSourceID.String()).Msg("data sources: deleting the source of a removed cloud account failed")
		}
	}
}

const cloudAccountKey = "cloud_account_id"
