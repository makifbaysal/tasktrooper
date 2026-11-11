package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type AgentCLIStore struct {
	pool *DB
}

var _ port.AgentCLIStore = (*AgentCLIStore)(nil)

func NewAgentCLIStore(pool *DB) *AgentCLIStore {
	return &AgentCLIStore{pool: pool}
}

func (s *AgentCLIStore) Get(ctx context.Context, flavor domain.AgentCLIFlavor) (domain.AgentCLIConnection, bool, error) {
	var conn domain.AgentCLIConnection
	err := s.pool.QueryRow(ctx, `
		SELECT flavor, provider_type, binary_path, binary_version, catalog_path, agent_count, skill_count, connected_at
		FROM agent_cli_connection
		WHERE flavor = $1
	`, string(flavor)).Scan(
		&conn.Flavor, &conn.ProviderType, &conn.BinaryPath, &conn.BinaryVersion,
		&conn.CatalogPath, &conn.AgentCount, &conn.SkillCount, &conn.ConnectedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {

		return domain.AgentCLIConnection{}, false, nil
	}
	if err != nil {
		return domain.AgentCLIConnection{}, false, fmt.Errorf("get agent cli connection: %w", err)
	}
	return conn, true, nil
}

// List returns every flavor currently connected.
func (s *AgentCLIStore) List(ctx context.Context) ([]domain.AgentCLIConnection, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT flavor, provider_type, binary_path, binary_version, catalog_path, agent_count, skill_count, connected_at
		FROM agent_cli_connection
	`)
	if err != nil {
		return nil, fmt.Errorf("list agent cli connections: %w", err)
	}
	defer rows.Close()

	var out []domain.AgentCLIConnection
	for rows.Next() {
		var conn domain.AgentCLIConnection
		if err := rows.Scan(
			&conn.Flavor, &conn.ProviderType, &conn.BinaryPath, &conn.BinaryVersion,
			&conn.CatalogPath, &conn.AgentCount, &conn.SkillCount, &conn.ConnectedAt,
		); err != nil {
			return nil, fmt.Errorf("scan agent cli connection: %w", err)
		}
		out = append(out, conn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list agent cli connections: %w", err)
	}
	return out, nil
}

func (s *AgentCLIStore) Set(ctx context.Context, conn domain.AgentCLIConnection) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_cli_connection
			(flavor, provider_type, binary_path, binary_version, catalog_path, agent_count, skill_count, connected_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (flavor) DO UPDATE SET
			provider_type  = EXCLUDED.provider_type,
			binary_path    = EXCLUDED.binary_path,
			binary_version = EXCLUDED.binary_version,
			catalog_path   = EXCLUDED.catalog_path,
			agent_count    = EXCLUDED.agent_count,
			skill_count    = EXCLUDED.skill_count,
			connected_at   = now()
	`,
		string(conn.Flavor), string(conn.ProviderType), conn.BinaryPath, conn.BinaryVersion,
		conn.CatalogPath, conn.AgentCount, conn.SkillCount,
	)
	if err != nil {
		return fmt.Errorf("set agent cli connection: %w", err)
	}
	return nil
}

func (s *AgentCLIStore) Clear(ctx context.Context, flavor domain.AgentCLIFlavor) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM agent_cli_connection WHERE flavor = $1`, string(flavor))
	if err != nil {
		return fmt.Errorf("clear agent cli connection: %w", err)
	}
	return nil
}

func agentCLIDefaultModelKey(flavor domain.AgentCLIFlavor) string {
	return "agent_cli_default_model:" + string(flavor)
}

func (s *AgentCLIStore) DefaultModel(ctx context.Context, flavor domain.AgentCLIFlavor) (string, error) {
	var value string
	err := s.pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, agentCLIDefaultModelKey(flavor)).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get agent cli default model: %w", err)
	}
	return value, nil
}

func (s *AgentCLIStore) SetDefaultModel(ctx context.Context, flavor domain.AgentCLIFlavor, model string) error {
	if model == "" {
		_, err := s.pool.Exec(ctx, `DELETE FROM app_settings WHERE key = $1`, agentCLIDefaultModelKey(flavor))
		if err != nil {
			return fmt.Errorf("clear agent cli default model: %w", err)
		}
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO app_settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, agentCLIDefaultModelKey(flavor), model)
	if err != nil {
		return fmt.Errorf("set agent cli default model: %w", err)
	}
	return nil
}
