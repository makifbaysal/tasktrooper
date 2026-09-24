package cloud

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *Service) ListAccounts(ctx context.Context) ([]domain.CloudAccount, error) {
	accounts, err := s.accounts.ListCloudAccounts(ctx)
	if err != nil {
		return nil, err
	}
	if accounts == nil {
		accounts = []domain.CloudAccount{}
	}
	return accounts, nil
}

func (s *Service) GetAccount(ctx context.Context, id uuid.UUID) (domain.CloudAccount, error) {
	return s.accounts.GetCloudAccount(ctx, id)
}

// requiredCredentialFields lists what each provider's credential must carry;
// CreateAccount refuses to even attempt Verify without them.
func requiredCredentialFields(provider domain.CloudProviderKind) []string {
	switch provider {
	case domain.CloudVercel:
		return []string{"token"}
	case domain.CloudGCP:
		return []string{"service_account_json"}
	case domain.CloudAWS:
		return []string{"access_key_id", "secret_access_key", "region"}
	}
	return nil
}

func validateCredentialFields(provider domain.CloudProviderKind, fields map[string]string) error {
	for _, key := range requiredCredentialFields(provider) {
		if strings.TrimSpace(fields[key]) == "" {
			return fmt.Errorf("%s is required: %w", key, ErrInvalidInput)
		}
	}
	return nil
}

// defaultAccountLabel is what an account is called when the caller did not
// name one, read off the identity Verify reported.
func defaultAccountLabel(provider domain.CloudProviderKind, meta map[string]string) string {
	switch provider {
	case domain.CloudVercel:
		if v := meta["team_slug"]; v != "" {
			return v
		}
		return meta["username"]
	case domain.CloudGCP:
		return meta["project_id"]
	case domain.CloudAWS:
		accountID, region := meta["account_id"], meta["region"]
		switch {
		case accountID != "" && region != "":
			return accountID + " (" + region + ")"
		case accountID != "":
			return accountID
		default:
			return region
		}
	}
	return ""
}

// CreateAccount verifies the credential against the live provider BEFORE
// anything is stored: a bad credential must never reach cloud_accounts, since
// its presence there is what the rest of this package reads as "this
// provider is connected".
func (s *Service) CreateAccount(ctx context.Context, req domain.SaveCloudAccountRequest) (domain.CloudAccount, error) {
	if !domain.ValidCloudProvider(req.Provider) {
		return domain.CloudAccount{}, fmt.Errorf("unknown provider %q: %w", req.Provider, ErrInvalidInput)
	}
	if err := validateCredentialFields(req.Provider, req.Fields); err != nil {
		return domain.CloudAccount{}, err
	}
	provider, ok := s.providerFor(req.Provider)
	if !ok {
		return domain.CloudAccount{}, fmt.Errorf("no adapter configured for provider %q: %w", req.Provider, ErrInvalidInput)
	}

	meta, err := provider.Verify(ctx, domain.CloudCredential{Provider: req.Provider, Fields: req.Fields})
	if err != nil {
		return domain.CloudAccount{}, fmt.Errorf("verify %s credential: %w: %w", req.Provider, err, ErrInvalidInput)
	}

	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = defaultAccountLabel(req.Provider, meta)
	}
	now := s.now()
	acct, err := s.accounts.CreateCloudAccount(ctx, domain.CloudAccount{
		Provider:   req.Provider,
		Label:      label,
		Meta:       meta,
		Status:     domain.CloudAccountOK,
		VerifiedAt: &now,
	}, req.Fields)
	if err != nil {
		return domain.CloudAccount{}, err
	}

	go s.rematchAll(context.WithoutCancel(s.bgCtx))
	return acct, nil
}

// UpdateAccount re-verifies against the provider when fields are given,
// applying the same never-store-on-failure rule as CreateAccount: the update
// is rejected wholesale rather than leaving the account half-changed.
func (s *Service) UpdateAccount(ctx context.Context, id uuid.UUID, label *string, fields map[string]string) (domain.CloudAccount, error) {
	acct, err := s.accounts.GetCloudAccount(ctx, id)
	if err != nil {
		return domain.CloudAccount{}, err
	}
	if label != nil {
		acct.Label = strings.TrimSpace(*label)
	}

	var storeFields map[string]string
	reverified := false
	if fields != nil {
		if err := validateCredentialFields(acct.Provider, fields); err != nil {
			return domain.CloudAccount{}, err
		}
		provider, ok := s.providerFor(acct.Provider)
		if !ok {
			return domain.CloudAccount{}, fmt.Errorf("no adapter configured for provider %q: %w", acct.Provider, ErrInvalidInput)
		}
		meta, verr := provider.Verify(ctx, domain.CloudCredential{AccountID: id, Provider: acct.Provider, Fields: fields})
		if verr != nil {
			return domain.CloudAccount{}, fmt.Errorf("verify %s credential: %w: %w", acct.Provider, verr, ErrInvalidInput)
		}
		now := s.now()
		acct.Meta = meta
		acct.Status = domain.CloudAccountOK
		acct.StatusDetail = ""
		acct.VerifiedAt = &now
		storeFields = fields
		reverified = true
	}

	saved, err := s.accounts.UpdateCloudAccount(ctx, acct, storeFields)
	if err != nil {
		return domain.CloudAccount{}, err
	}
	s.forgetResourceCache(id)
	if reverified {
		go s.rematchAll(context.WithoutCancel(s.bgCtx))
	}
	return saved, nil
}

// VerifyAccount re-checks the stored credential against the provider and
// records the outcome; a verify failure is reported through Status/StatusDetail,
// not as an error, so a UI can poll it the same way it reads any other account.
func (s *Service) VerifyAccount(ctx context.Context, id uuid.UUID) (domain.CloudAccount, error) {
	acct, err := s.accounts.GetCloudAccount(ctx, id)
	if err != nil {
		return domain.CloudAccount{}, err
	}
	provider, ok := s.providerFor(acct.Provider)
	if !ok {
		return domain.CloudAccount{}, fmt.Errorf("no adapter configured for provider %q: %w", acct.Provider, ErrInvalidInput)
	}
	cred, err := s.accounts.CloudCredential(ctx, id)
	if err != nil {
		return domain.CloudAccount{}, err
	}

	meta, verr := provider.Verify(ctx, cred)
	if verr != nil {
		acct.Status = domain.CloudAccountError
		acct.StatusDetail = verr.Error()
		saved, err := s.accounts.UpdateCloudAccount(ctx, acct, nil)
		if err != nil {
			return domain.CloudAccount{}, err
		}
		return saved, nil
	}

	now := s.now()
	acct.Meta = meta
	acct.Status = domain.CloudAccountOK
	acct.StatusDetail = ""
	acct.VerifiedAt = &now
	saved, err := s.accounts.UpdateCloudAccount(ctx, acct, nil)
	if err != nil {
		return domain.CloudAccount{}, err
	}
	s.forgetResourceCache(id)
	go s.rematchAll(context.WithoutCancel(s.bgCtx))
	return saved, nil
}

func (s *Service) DeleteAccount(ctx context.Context, id uuid.UUID) error {
	if err := s.accounts.DeleteCloudAccount(ctx, id); err != nil {
		return err
	}
	s.forgetResourceCache(id)
	return nil
}
