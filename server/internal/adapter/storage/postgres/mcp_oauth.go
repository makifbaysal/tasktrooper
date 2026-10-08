package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var _ port.MCPOAuthStore = (*MCPStore)(nil)

func (s *MCPStore) GetMCPOAuth(ctx context.Context, serverID string) (port.MCPOAuthRecord, bool, error) {
	var rec port.MCPOAuthRecord
	var metadata []byte
	err := s.pool.QueryRow(ctx, `
		SELECT server_id, metadata, client_id, client_secret, client_auth_method, client_redirect_uri,
		       client_dynamic, access_token, refresh_token, token_type, scope, expires_at, expired, updated_at
		FROM mcp_server_oauth WHERE server_id = $1
	`, serverID).Scan(
		&rec.ServerID, &metadata, &rec.ClientID, &rec.ClientSecret, &rec.ClientAuthMethod, &rec.ClientRedirectURI,
		&rec.ClientDynamic, &rec.AccessToken, &rec.RefreshToken, &rec.TokenType, &rec.Scope, &rec.ExpiresAt, &rec.Expired, &rec.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return port.MCPOAuthRecord{}, false, nil
	}
	if err != nil {
		return port.MCPOAuthRecord{}, false, fmt.Errorf("get mcp oauth: %w", err)
	}
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &rec.Metadata); err != nil {
			return port.MCPOAuthRecord{}, false, fmt.Errorf("decode mcp oauth metadata: %w", err)
		}
	}
	return rec, true, nil
}

func (s *MCPStore) SaveMCPOAuth(ctx context.Context, rec port.MCPOAuthRecord) error {
	metadata, err := json.Marshal(rec.Metadata)
	if err != nil {
		return fmt.Errorf("encode mcp oauth metadata: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO mcp_server_oauth (server_id, metadata, client_id, client_secret, client_auth_method, client_redirect_uri,
		                              client_dynamic, access_token, refresh_token, token_type, scope, expires_at, expired, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, now())
		ON CONFLICT (server_id) DO UPDATE SET
			metadata = EXCLUDED.metadata,
			client_id = EXCLUDED.client_id,
			client_secret = EXCLUDED.client_secret,
			client_auth_method = EXCLUDED.client_auth_method,
			client_redirect_uri = EXCLUDED.client_redirect_uri,
			client_dynamic = EXCLUDED.client_dynamic,
			access_token = EXCLUDED.access_token,
			refresh_token = EXCLUDED.refresh_token,
			token_type = EXCLUDED.token_type,
			scope = EXCLUDED.scope,
			expires_at = EXCLUDED.expires_at,
			expired = EXCLUDED.expired,
			updated_at = now()
	`, rec.ServerID, metadata, rec.ClientID, rec.ClientSecret, rec.ClientAuthMethod, rec.ClientRedirectURI,
		rec.ClientDynamic, rec.AccessToken, rec.RefreshToken, rec.TokenType, rec.Scope, rec.ExpiresAt, rec.Expired)
	if err != nil {
		return fmt.Errorf("save mcp oauth: %w", err)
	}
	return nil
}

func (s *MCPStore) DeleteMCPOAuth(ctx context.Context, serverID string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM mcp_server_oauth WHERE server_id = $1`, serverID); err != nil {
		return fmt.Errorf("delete mcp oauth: %w", err)
	}
	return nil
}
