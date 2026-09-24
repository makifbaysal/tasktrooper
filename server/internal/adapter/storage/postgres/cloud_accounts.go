package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// CloudAccountStore persists one connected provider login per row; the
// credential fields (migration 155) are encrypted together as a single JSON
// blob, never split into per-field columns, so a provider can carry whatever
// shape of secret it needs without a schema change.
type CloudAccountStore struct {
	pool *DB

	cipherOnce sync.Once
	cipher     *secrets.Cipher
	cipherErr  error
}

func NewCloudAccountStore(pool *DB) *CloudAccountStore {
	return &CloudAccountStore{pool: pool}
}

var _ port.CloudAccountStore = (*CloudAccountStore)(nil)

// SetCipher injects the cipher derived at boot, before
// runtime.scrubProcessSecrets wipes MCP_SECRETS_KEY from the process
// environment. Without it, credential fields only derive a cipher on first
// use — which for this store is always a later HTTP request, after the key
// is already gone.
func (s *CloudAccountStore) SetCipher(c *secrets.Cipher, err error) {
	s.cipherOnce.Do(func() { s.cipher, s.cipherErr = c, err })
}

func (s *CloudAccountStore) getCipher() (*secrets.Cipher, error) {
	s.cipherOnce.Do(func() {
		s.cipher, s.cipherErr = secrets.NewCipherFromEnv()
	})
	return s.cipher, s.cipherErr
}

const cloudAccountCols = `id, provider, label, meta, status, status_detail, verified_at, created_at, updated_at`

func scanCloudAccount(row pgx.Row) (domain.CloudAccount, error) {
	var a domain.CloudAccount
	var metaJSON []byte
	if err := row.Scan(
		&a.ID, &a.Provider, &a.Label, &metaJSON, &a.Status, &a.StatusDetail, &a.VerifiedAt, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return domain.CloudAccount{}, err
	}
	if err := json.Unmarshal(metaJSON, &a.Meta); err != nil {
		return domain.CloudAccount{}, fmt.Errorf("unmarshal cloud account meta: %w", err)
	}
	return a, nil
}

func (s *CloudAccountStore) ListCloudAccounts(ctx context.Context) ([]domain.CloudAccount, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+cloudAccountCols+` FROM cloud_accounts ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list cloud accounts: %w", err)
	}
	defer rows.Close()
	var out []domain.CloudAccount
	for rows.Next() {
		a, err := scanCloudAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("scan cloud account: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *CloudAccountStore) GetCloudAccount(ctx context.Context, id uuid.UUID) (domain.CloudAccount, error) {
	a, err := scanCloudAccount(s.pool.QueryRow(ctx, `SELECT `+cloudAccountCols+` FROM cloud_accounts WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CloudAccount{}, fmt.Errorf("get cloud account: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.CloudAccount{}, fmt.Errorf("get cloud account: %w", err)
	}
	return a, nil
}

const insertCloudAccountSQL = `
INSERT INTO cloud_accounts (id, provider, label, meta, secret_enc, status, status_detail, verified_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING ` + cloudAccountCols

// CreateCloudAccount requires non-empty fields: an account with no credential
// fields cannot be verified or used by any CloudProvider adapter, so it is
// rejected here rather than persisted and left to fail later at call time.
func (s *CloudAccountStore) CreateCloudAccount(ctx context.Context, acct domain.CloudAccount, fields map[string]string) (domain.CloudAccount, error) {
	if len(fields) == 0 {
		return domain.CloudAccount{}, fmt.Errorf("create cloud account: fields required")
	}
	secretEnc, err := s.encryptFields(fields)
	if err != nil {
		return domain.CloudAccount{}, fmt.Errorf("create cloud account: %w", err)
	}
	if acct.ID == uuid.Nil {
		acct.ID = uuid.New()
	}
	status := acct.Status
	if status == "" {
		status = domain.CloudAccountUnverified
	}
	metaJSON, err := json.Marshal(nonNilMap(acct.Meta))
	if err != nil {
		return domain.CloudAccount{}, fmt.Errorf("marshal cloud account meta: %w", err)
	}
	row := s.pool.QueryRow(ctx, insertCloudAccountSQL,
		acct.ID, acct.Provider, acct.Label, metaJSON, secretEnc, status, acct.StatusDetail, acct.VerifiedAt)
	out, err := scanCloudAccount(row)
	if err != nil {
		return domain.CloudAccount{}, fmt.Errorf("create cloud account: %w", err)
	}
	return out, nil
}

const updateCloudAccountSQL = `
UPDATE cloud_accounts SET
	provider = $2, label = $3, meta = $4, status = $5, status_detail = $6, verified_at = $7, updated_at = now()
WHERE id = $1
RETURNING ` + cloudAccountCols

const updateCloudAccountWithSecretSQL = `
UPDATE cloud_accounts SET
	provider = $2, label = $3, meta = $4, status = $5, status_detail = $6, verified_at = $7, secret_enc = $8, updated_at = now()
WHERE id = $1
RETURNING ` + cloudAccountCols

// UpdateCloudAccount rewrites label/meta/status/verification; fields replaces
// the stored secret only when non-nil, so a caller updating just the label or
// re-verifying an existing credential never has to re-supply it.
func (s *CloudAccountStore) UpdateCloudAccount(ctx context.Context, acct domain.CloudAccount, fields map[string]string) (domain.CloudAccount, error) {
	metaJSON, err := json.Marshal(nonNilMap(acct.Meta))
	if err != nil {
		return domain.CloudAccount{}, fmt.Errorf("marshal cloud account meta: %w", err)
	}

	var row pgx.Row
	if fields != nil {
		secretEnc, err := s.encryptFields(fields)
		if err != nil {
			return domain.CloudAccount{}, fmt.Errorf("update cloud account: %w", err)
		}
		row = s.pool.QueryRow(ctx, updateCloudAccountWithSecretSQL,
			acct.ID, acct.Provider, acct.Label, metaJSON, acct.Status, acct.StatusDetail, acct.VerifiedAt, secretEnc)
	} else {
		row = s.pool.QueryRow(ctx, updateCloudAccountSQL,
			acct.ID, acct.Provider, acct.Label, metaJSON, acct.Status, acct.StatusDetail, acct.VerifiedAt)
	}
	out, err := scanCloudAccount(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CloudAccount{}, fmt.Errorf("update cloud account: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.CloudAccount{}, fmt.Errorf("update cloud account: %w", err)
	}
	return out, nil
}

func (s *CloudAccountStore) DeleteCloudAccount(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM cloud_accounts WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete cloud account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete cloud account: %w", port.ErrNotFound)
	}
	return nil
}

// CloudCredential is the only path that decrypts an account's fields; every
// other read (List/Get) stops at cloudAccountCols, which never includes
// secret_enc.
func (s *CloudAccountStore) CloudCredential(ctx context.Context, id uuid.UUID) (domain.CloudCredential, error) {
	var provider domain.CloudProviderKind
	var metaJSON, secretEnc []byte
	err := s.pool.QueryRow(ctx, `SELECT provider, meta, secret_enc FROM cloud_accounts WHERE id = $1`, id).
		Scan(&provider, &metaJSON, &secretEnc)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CloudCredential{}, fmt.Errorf("cloud credential: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.CloudCredential{}, fmt.Errorf("cloud credential: %w", err)
	}
	var meta map[string]string
	if err := json.Unmarshal(metaJSON, &meta); err != nil {
		return domain.CloudCredential{}, fmt.Errorf("unmarshal cloud credential meta: %w", err)
	}
	cipher, err := s.getCipher()
	if err != nil {
		return domain.CloudCredential{}, err
	}
	plain, err := cipher.Decrypt(secretEnc)
	if err != nil {
		return domain.CloudCredential{}, fmt.Errorf("decrypt cloud credential: %w", err)
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(plain), &fields); err != nil {
		return domain.CloudCredential{}, fmt.Errorf("unmarshal cloud credential fields: %w", err)
	}
	return domain.CloudCredential{AccountID: id, Provider: provider, Meta: meta, Fields: fields}, nil
}

func (s *CloudAccountStore) encryptFields(fields map[string]string) ([]byte, error) {
	cipher, err := s.getCipher()
	if err != nil {
		return nil, err
	}
	fieldsJSON, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("marshal fields: %w", err)
	}
	ct, err := cipher.Encrypt(string(fieldsJSON))
	if err != nil {
		return nil, fmt.Errorf("encrypt fields: %w", err)
	}
	return ct, nil
}
