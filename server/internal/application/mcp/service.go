package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type ReloadFunc func(configs []domain.MCPServerConfig) error

type Service struct {
	store  port.MCPStore
	cipher *secrets.Cipher
	reload ReloadFunc
	oauth  *oauthFlows
}

func NewService(store port.MCPStore, cipher *secrets.Cipher, reload ReloadFunc) *Service {
	return &Service{store: store, cipher: cipher, reload: reload}
}

func (s *Service) List(ctx context.Context) ([]domain.MCPServer, error) {
	return s.store.List(ctx)
}

func (s *Service) ListViews(ctx context.Context, health []map[string]interface{}) ([]domain.MCPServerView, error) {
	servers, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	healthByID := make(map[string]map[string]interface{}, len(health))
	for _, entry := range health {
		id, _ := entry["id"].(string)
		if id != "" {
			healthByID[id] = entry
		}
	}
	views := make([]domain.MCPServerView, 0, len(servers))
	for _, server := range servers {
		masked, err := s.maskServerView(ctx, server)
		if err != nil {
			return nil, err
		}
		view := mergeHealth(masked, healthByID[server.ID])
		view.Auth, err = s.authStatus(ctx, server, healthByID[server.ID])
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Service) Create(ctx context.Context, req domain.CreateMCPServerRequest) (domain.MCPServerView, error) {
	if err := validateCreateRequest(req); err != nil {
		return domain.MCPServerView{}, err
	}
	if req.Transport == "" {
		req.Transport = "stdio"
	}
	if req.Access == "" {
		req.Access = domain.DefaultNewMCPAccess
	}

	server := stripIncomingSecrets(req)
	created, err := s.store.Create(ctx, server)
	if err != nil {
		return domain.MCPServerView{}, err
	}
	if err := s.applySecretUpdates(ctx, created.ID, req); err != nil {
		_ = s.store.Delete(ctx, created.ID)
		return domain.MCPServerView{}, err
	}
	if err := s.reloadStored(ctx); err != nil {
		return domain.MCPServerView{}, err
	}
	return s.maskServerView(ctx, created)
}

func (s *Service) Update(ctx context.Context, id string, req domain.UpdateMCPServerRequest) (domain.MCPServerView, error) {
	if err := validateUpdateRequest(id, req); err != nil {
		return domain.MCPServerView{}, err
	}
	if req.Transport == "" {
		req.Transport = "stdio"
	}

	existing, err := s.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrMCPServerNotFound) {
			return domain.MCPServerView{}, err
		}
		return domain.MCPServerView{}, fmt.Errorf("get mcp server: %w", err)
	}
	if req.Access == "" {
		req.Access = existing.Access.Effective()
	}

	req.ID = id
	server := stripIncomingSecrets(req)
	updated, err := s.store.Update(ctx, server)
	if err != nil {
		return domain.MCPServerView{}, err
	}
	if err := s.applySecretUpdates(ctx, id, req); err != nil {
		return domain.MCPServerView{}, err
	}
	// A sign-in names the resource it was granted for; once the server points
	// somewhere else its token must never be sent there.
	if existing.Transport != updated.Transport || existing.URL != updated.URL {
		if err := s.forgetOAuth(ctx, id); err != nil {
			return domain.MCPServerView{}, err
		}
	}
	if err := s.reloadStored(ctx); err != nil {
		return domain.MCPServerView{}, err
	}
	return s.maskServerView(ctx, updated)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	s.dropPendingOAuth(id)
	if err := s.store.DeleteSecretsForServer(ctx, id); err != nil {
		return err
	}
	return s.reloadStored(ctx)
}

func (s *Service) reloadStored(ctx context.Context) error {
	if s.reload == nil {
		return nil
	}
	configs, err := s.ResolvedConfigs(ctx)
	if err != nil {
		return err
	}
	return s.reload(configs)
}

func mergeHealth(server domain.MCPServerView, health map[string]interface{}) domain.MCPServerView {
	if !server.Enabled {
		server.Status = "disabled"
		return server
	}
	if len(server.MissingConfig) > 0 {
		server.Status = "needs_config"
		return server
	}
	if health == nil {
		server.Status = "error"
		server.LastError = "not loaded"
		return server
	}
	if connected, ok := health["connected"].(bool); ok && connected {
		server.Connected = true
		server.Status = "connected"
		if count, ok := health["tool_count"].(int); ok {
			server.ToolCount = count
		} else if countF, ok := health["tool_count"].(float64); ok {
			server.ToolCount = int(countF)
		}
		server.Tools = toolsFromHealth(health)
		return server
	}
	server.Status = "error"
	if errMsg, ok := health["last_error"].(string); ok && errMsg != "" {
		server.LastError = errMsg
	} else if healthStatus, ok := health["status"].(string); ok && healthStatus == "disabled" {
		server.LastError = "Server is enabled but its connection is not loaded; save and try again"
	} else {
		server.LastError = "Could not connect to the MCP server"
	}
	return server
}

func toolsFromHealth(health map[string]interface{}) []string {
	raw, ok := health["tools"].([]interface{})
	if !ok {
		if names, ok := health["tools"].([]string); ok {
			return names
		}
		return nil
	}
	tools := make([]string, 0, len(raw))
	for _, item := range raw {
		if name, ok := item.(string); ok {
			tools = append(tools, name)
		}
	}
	return tools
}
