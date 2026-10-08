package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
)

func (s *Service) ResolveRuntimeConfig(ctx context.Context, server domain.MCPServer) (domain.MCPServerConfig, error) {
	cfg := server.ToConfig()
	cfg.Command = domain.EnvExpandString(cfg.Command)
	cfg.URL = domain.EnvExpandString(cfg.URL)
	cfg.Args = domain.EnvExpandSlice(cfg.Args)
	cfg.Env = domain.EnvExpandMap(cfg.Env)
	cfg.Headers = domain.EnvExpandMap(cfg.Headers)

	secretRecords, err := s.store.ListSecrets(ctx, server.ID)
	if err != nil {
		return domain.MCPServerConfig{}, fmt.Errorf("list secrets for %s: %w", server.ID, err)
	}
	if len(secretRecords) == 0 {
		return cfg, nil
	}
	if s.cipher == nil {
		return domain.MCPServerConfig{}, fmt.Errorf("mcp secrets cipher not configured")
	}

	if cfg.Env == nil {
		cfg.Env = make(map[string]string)
	}
	if cfg.Headers == nil {
		cfg.Headers = make(map[string]string)
	}

	for _, rec := range secretRecords {
		plain, err := s.cipher.Decrypt(rec.Value)
		if err != nil {
			return domain.MCPServerConfig{}, fmt.Errorf("decrypt secret %s/%s: %w", rec.Location, rec.Key, err)
		}
		switch rec.Location {
		case "env":
			cfg.Env[rec.Key] = plain
		case "headers":
			cfg.Headers[rec.Key] = plain
		default:
			return domain.MCPServerConfig{}, fmt.Errorf("unsupported secret location %q", rec.Location)
		}
	}

	return cfg, nil
}

func (s *Service) ResolvedConfigs(ctx context.Context) ([]domain.MCPServerConfig, error) {
	servers, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	configs := make([]domain.MCPServerConfig, 0, len(servers))
	for _, server := range servers {
		cfg, err := s.ResolveRuntimeConfig(ctx, server)
		if err != nil {
			return nil, err
		}
		configs = append(configs, cfg)
	}
	return configs, nil
}

func (s *Service) maskServerView(ctx context.Context, server domain.MCPServer) (domain.MCPServerView, error) {
	view := domain.MCPServerView{
		MCPServer:    server,
		ConfigFields: domain.ConfigFieldsForServer(server.ID),
	}

	secretRecords, err := s.store.ListSecrets(ctx, server.ID)
	if err != nil {
		return domain.MCPServerView{}, err
	}
	secretIndex := make(map[string]bool, len(secretRecords))
	for _, rec := range secretRecords {
		secretIndex[rec.Location+":"+rec.Key] = true
	}

	view.MissingConfig = domain.MissingConfigFields(server, func(location, key string) bool {
		return secretIndex[location+":"+key]
	})

	for _, field := range view.ConfigFields {
		if !field.Secret {
			continue
		}
		switch field.Location {
		case "env":
			if view.Env == nil {
				view.Env = make(map[string]string)
			}
			if secretIndex["env:"+field.Key] {
				view.Env[field.Key] = secrets.MaskedValue()
			} else {
				view.Env[field.Key] = ""
			}
		case "headers":
			if view.Headers == nil {
				view.Headers = make(map[string]string)
			}
			if secretIndex["headers:"+field.Key] {
				view.Headers[field.Key] = secrets.MaskedValue()
			} else {
				view.Headers[field.Key] = ""
			}
		}
	}

	return view, nil
}

type secretUpdate struct {
	location string
	key      string
	value    string
	clear    bool
}

func (s *Service) applySecretUpdates(ctx context.Context, serverID string, req domain.CreateMCPServerRequest) error {
	if s.cipher == nil {
		return fmt.Errorf("mcp secrets cipher not configured")
	}

	updates := collectSecretUpdates(serverID, req)
	for _, upd := range updates {
		if upd.clear {
			if err := s.store.DeleteSecret(ctx, serverID, upd.location, upd.key); err != nil {
				return err
			}
			continue
		}
		if upd.value == "" || secrets.IsMaskedValue(upd.value) {
			continue
		}
		encrypted, err := s.cipher.Encrypt(upd.value)
		if err != nil {
			return fmt.Errorf("encrypt secret %s/%s: %w", upd.location, upd.key, err)
		}
		if err := s.store.SetSecret(ctx, serverID, upd.location, upd.key, encrypted); err != nil {
			return err
		}
	}
	return nil
}

func collectSecretUpdates(serverID string, req domain.CreateMCPServerRequest) []secretUpdate {
	fields := domain.ConfigFieldsForServer(serverID)
	if len(fields) == 0 {
		return inferSecretUpdates(req)
	}

	var updates []secretUpdate
	for _, field := range fields {
		if !field.Secret {
			continue
		}
		value := secretValueFromRequest(req, field.Location, field.Key)
		updates = append(updates, secretUpdate{
			location: field.Location,
			key:      field.Key,
			value:    value,
		})
	}
	return updates
}

func inferSecretUpdates(req domain.CreateMCPServerRequest) []secretUpdate {
	var updates []secretUpdate
	for key, value := range req.Env {
		if isLikelySecretValue(value) {
			updates = append(updates, secretUpdate{location: "env", key: key, value: value})
		}
	}
	for key, value := range req.Headers {
		if isLikelySecretValue(value) {
			updates = append(updates, secretUpdate{location: "headers", key: key, value: value})
		}
	}
	return updates
}

func secretValueFromRequest(req domain.CreateMCPServerRequest, location, key string) string {
	switch location {
	case "env":
		if req.Env == nil {
			return ""
		}
		return req.Env[key]
	case "headers":
		if req.Headers == nil {
			return ""
		}
		return req.Headers[key]
	default:
		return ""
	}
}

func isLikelySecretValue(value string) bool {
	if value == "" || secrets.IsMaskedValue(value) {
		return false
	}
	return !strings.Contains(value, "${")
}

func stripSecretsFromStored(server domain.MCPServer, serverID string) domain.MCPServer {
	for _, field := range domain.SecretFieldsForServer(serverID) {
		switch field.Location {
		case "env":
			if server.Env != nil {
				delete(server.Env, field.Key)
			}
		case "headers":
			if server.Headers != nil {
				delete(server.Headers, field.Key)
			}
		}
	}
	return server
}

func stripIncomingSecrets(req domain.CreateMCPServerRequest) domain.MCPServer {
	server := domain.MCPServer{
		ID:           req.ID,
		Enabled:      req.Enabled,
		Transport:    req.Transport,
		Command:      req.Command,
		Args:         req.Args,
		Env:          cloneStringMap(req.Env),
		URL:          req.URL,
		Headers:      cloneStringMap(req.Headers),
		AllowedTools: req.AllowedTools,
		Access:       req.Access,
	}
	return stripSecretsFromStored(server, req.ID)
}
