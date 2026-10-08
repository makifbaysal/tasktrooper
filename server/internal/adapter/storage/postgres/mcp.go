package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type MCPStore struct {
	pool *DB
}

func NewMCPStore(pool *DB) *MCPStore {
	return &MCPStore{pool: pool}
}

func (s *MCPStore) List(ctx context.Context) ([]domain.MCPServer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, enabled, transport, command, args, env, url, headers, allowed_tools, access, created_at
		FROM mcp_servers ORDER BY id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list mcp servers: %w", err)
	}
	defer rows.Close()
	return scanMCPServers(rows)
}

func (s *MCPStore) Get(ctx context.Context, id string) (domain.MCPServer, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, enabled, transport, command, args, env, url, headers, allowed_tools, access, created_at
		FROM mcp_servers WHERE id = $1
	`, id)
	server, err := scanMCPServer(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.MCPServer{}, domain.ErrMCPServerNotFound
		}
		return domain.MCPServer{}, err
	}
	return server, nil
}

func (s *MCPStore) Create(ctx context.Context, server domain.MCPServer) (domain.MCPServer, error) {
	argsJSON, envJSON, headersJSON, toolsJSON, err := marshalMCPFields(server)
	if err != nil {
		return domain.MCPServer{}, err
	}
	var out domain.MCPServer
	err = s.pool.QueryRow(ctx, `
		INSERT INTO mcp_servers (id, enabled, transport, command, args, env, url, headers, allowed_tools, access)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, enabled, transport, command, args, env, url, headers, allowed_tools, access, created_at
	`, server.ID, server.Enabled, server.Transport, server.Command, argsJSON, envJSON, server.URL, headersJSON, toolsJSON, storedMCPAccess(server.Access)).Scan(
		&out.ID, &out.Enabled, &out.Transport, &out.Command, &argsJSON, &envJSON, &out.URL, &headersJSON, &toolsJSON, &out.Access, &out.CreatedAt,
	)
	if err != nil {
		return domain.MCPServer{}, mapMCPStoreError(fmt.Errorf("create mcp server: %w", err))
	}
	return unmarshalMCPFields(out, argsJSON, envJSON, headersJSON, toolsJSON)
}

func (s *MCPStore) Update(ctx context.Context, server domain.MCPServer) (domain.MCPServer, error) {
	argsJSON, envJSON, headersJSON, toolsJSON, err := marshalMCPFields(server)
	if err != nil {
		return domain.MCPServer{}, err
	}
	var out domain.MCPServer
	err = s.pool.QueryRow(ctx, `
		UPDATE mcp_servers
		SET enabled=$2, transport=$3, command=$4, args=$5, env=$6, url=$7, headers=$8, allowed_tools=$9, access=$10
		WHERE id=$1
		RETURNING id, enabled, transport, command, args, env, url, headers, allowed_tools, access, created_at
	`, server.ID, server.Enabled, server.Transport, server.Command, argsJSON, envJSON, server.URL, headersJSON, toolsJSON, storedMCPAccess(server.Access)).Scan(
		&out.ID, &out.Enabled, &out.Transport, &out.Command, &argsJSON, &envJSON, &out.URL, &headersJSON, &toolsJSON, &out.Access, &out.CreatedAt,
	)
	if err != nil {
		return domain.MCPServer{}, fmt.Errorf("update mcp server: %w", err)
	}
	return unmarshalMCPFields(out, argsJSON, envJSON, headersJSON, toolsJSON)
}

func (s *MCPStore) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM mcp_servers WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete mcp server: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrMCPServerNotFound
	}
	return nil
}

func (s *MCPStore) SeededTemplateIDs(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT template_id FROM mcp_template_seeds ORDER BY template_id`)
	if err != nil {
		return nil, fmt.Errorf("list seeded mcp templates: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("scan seeded mcp templates: %w", err)
	}
	return ids, nil
}

func (s *MCPStore) MarkTemplatesSeeded(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO mcp_template_seeds (template_id)
		SELECT unnest($1::text[])
		ON CONFLICT DO NOTHING
	`, ids)
	if err != nil {
		return fmt.Errorf("mark mcp templates seeded: %w", err)
	}
	return nil
}

// storedMCPAccess keeps a caller that never set a mode on the safe side of
// the column's check: unset is "listed", the default for a new server.
func storedMCPAccess(access domain.MCPAccess) string {
	if access.Valid() {
		return string(access)
	}
	return string(domain.DefaultNewMCPAccess)
}

func marshalMCPFields(server domain.MCPServer) ([]byte, []byte, []byte, []byte, error) {
	argsJSON, err := json.Marshal(server.Args)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	envJSON, err := json.Marshal(server.Env)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	headersJSON, err := json.Marshal(server.Headers)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	toolsJSON, err := json.Marshal(server.AllowedTools)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return argsJSON, envJSON, headersJSON, toolsJSON, nil
}

func unmarshalMCPFields(out domain.MCPServer, argsJSON, envJSON, headersJSON, toolsJSON []byte) (domain.MCPServer, error) {
	_ = json.Unmarshal(argsJSON, &out.Args)
	_ = json.Unmarshal(envJSON, &out.Env)
	_ = json.Unmarshal(headersJSON, &out.Headers)
	_ = json.Unmarshal(toolsJSON, &out.AllowedTools)
	return out, nil
}

func scanMCPServer(row pgx.Row) (domain.MCPServer, error) {
	var out domain.MCPServer
	var argsJSON, envJSON, headersJSON, toolsJSON []byte
	err := row.Scan(
		&out.ID, &out.Enabled, &out.Transport, &out.Command, &argsJSON, &envJSON, &out.URL, &headersJSON, &toolsJSON, &out.Access, &out.CreatedAt,
	)
	if err != nil {
		return domain.MCPServer{}, fmt.Errorf("scan mcp server: %w", err)
	}
	return unmarshalMCPFields(out, argsJSON, envJSON, headersJSON, toolsJSON)
}

func scanMCPServers(rows pgx.Rows) ([]domain.MCPServer, error) {
	var servers []domain.MCPServer
	for rows.Next() {
		var out domain.MCPServer
		var argsJSON, envJSON, headersJSON, toolsJSON []byte
		if err := rows.Scan(
			&out.ID, &out.Enabled, &out.Transport, &out.Command, &argsJSON, &envJSON, &out.URL, &headersJSON, &toolsJSON, &out.Access, &out.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan mcp server row: %w", err)
		}
		item, err := unmarshalMCPFields(out, argsJSON, envJSON, headersJSON, toolsJSON)
		if err != nil {
			return nil, err
		}
		servers = append(servers, item)
	}
	return servers, rows.Err()
}
