package mcp_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/mcp"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeMCPStore struct {
	servers map[string]domain.MCPServer
	secrets map[string][]port.MCPSecretRecord
}

func newFakeMCPStore() *fakeMCPStore {
	return &fakeMCPStore{
		servers: make(map[string]domain.MCPServer),
		secrets: make(map[string][]port.MCPSecretRecord),
	}
}

func (f *fakeMCPStore) List(ctx context.Context) ([]domain.MCPServer, error) {
	out := make([]domain.MCPServer, 0, len(f.servers))
	for _, s := range f.servers {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeMCPStore) Get(ctx context.Context, id string) (domain.MCPServer, error) {
	s, ok := f.servers[id]
	if !ok {
		return domain.MCPServer{}, domain.ErrMCPServerNotFound
	}
	return s, nil
}

func (f *fakeMCPStore) Create(ctx context.Context, server domain.MCPServer) (domain.MCPServer, error) {
	if _, exists := f.servers[server.ID]; exists {
		return domain.MCPServer{}, domain.ErrMCPServerAlreadyExists
	}
	f.servers[server.ID] = server
	return server, nil
}

func (f *fakeMCPStore) Update(ctx context.Context, server domain.MCPServer) (domain.MCPServer, error) {
	if _, ok := f.servers[server.ID]; !ok {
		return domain.MCPServer{}, domain.ErrMCPServerNotFound
	}
	f.servers[server.ID] = server
	return server, nil
}

func (f *fakeMCPStore) Delete(ctx context.Context, id string) error {
	if _, ok := f.servers[id]; !ok {
		return domain.ErrMCPServerNotFound
	}
	delete(f.servers, id)
	return nil
}

func (f *fakeMCPStore) Count(ctx context.Context) (int, error) {
	return len(f.servers), nil
}

func (f *fakeMCPStore) ListSecrets(ctx context.Context, serverID string) ([]port.MCPSecretRecord, error) {
	return append([]port.MCPSecretRecord(nil), f.secrets[serverID]...), nil
}

func (f *fakeMCPStore) SetSecret(ctx context.Context, serverID, location, key string, encrypted []byte) error {
	records := f.secrets[serverID]
	for i, rec := range records {
		if rec.Location == location && rec.Key == key {
			records[i].Value = encrypted
			f.secrets[serverID] = records
			return nil
		}
	}
	f.secrets[serverID] = append(records, port.MCPSecretRecord{
		Location: location,
		Key:      key,
		Value:    encrypted,
	})
	return nil
}

func (f *fakeMCPStore) DeleteSecret(ctx context.Context, serverID, location, key string) error {
	records := f.secrets[serverID]
	filtered := records[:0]
	for _, rec := range records {
		if rec.Location == location && rec.Key == key {
			continue
		}
		filtered = append(filtered, rec)
	}
	f.secrets[serverID] = filtered
	return nil
}

func (f *fakeMCPStore) DeleteSecretsForServer(ctx context.Context, serverID string) error {
	delete(f.secrets, serverID)
	return nil
}

type ServiceSuite struct {
	suite.Suite
	store  *fakeMCPStore
	cipher *secrets.Cipher
	svc    *mcp.Service
}

func (s *ServiceSuite) SetupTest() {
	os.Setenv("MCP_SECRETS_KEY", "test-mcp-secrets-key")
	s.store = newFakeMCPStore()
	cipher, err := secrets.NewCipherFromEnv()
	s.Require().NoError(err)
	s.cipher = cipher
	s.svc = mcp.NewService(s.store, s.cipher, nil)
}

func (s *ServiceSuite) TestResolveRuntimeConfigMergesSecretsAndExpandsEnv() {
	ctx := context.Background()
	server := domain.MCPServer{
		ID:        "gitlab",
		Enabled:   true,
		Transport: "stdio",
		Command:   "npx",
		Args:      []string{"-y", "@zereight/mcp-gitlab"},
		Env:       map[string]string{"OTHER": "${PATH}"},
	}
	s.store.servers["gitlab"] = server

	encrypted, err := s.cipher.Encrypt("glpat_test_token")
	s.Require().NoError(err)
	s.store.secrets["gitlab"] = []port.MCPSecretRecord{{
		Location: "env",
		Key:      "GITLAB_PERSONAL_ACCESS_TOKEN",
		Value:    encrypted,
	}}

	cfg, err := s.svc.ResolveRuntimeConfig(ctx, server)
	s.Require().NoError(err)
	s.Equal("glpat_test_token", cfg.Env["GITLAB_PERSONAL_ACCESS_TOKEN"])
	s.NotEmpty(cfg.Env["OTHER"])
	s.NotEqual("${PATH}", cfg.Env["OTHER"])
}

func (s *ServiceSuite) TestCreateRejectsDuplicateID() {
	ctx := context.Background()
	s.store.servers["browser"] = domain.MCPServer{ID: "browser", Transport: "stdio", Command: "npx"}

	_, err := s.svc.Create(ctx, domain.CreateMCPServerRequest{
		ID:        "browser",
		Transport: "stdio",
		Command:   "npx",
	})
	s.ErrorIs(err, domain.ErrMCPServerAlreadyExists)
}

func (s *ServiceSuite) TestCreateValidatesTransport() {
	ctx := context.Background()
	_, err := s.svc.Create(ctx, domain.CreateMCPServerRequest{
		ID:        "bad-http",
		Transport: "http",
	})
	s.ErrorIs(err, domain.ErrMCPInvalidRequest)
}

func (s *ServiceSuite) TestUpdateKeepsExistingSecretWhenMasked() {
	ctx := context.Background()
	s.store.servers["gitlab"] = domain.MCPServer{
		ID: "gitlab", Transport: "stdio", Command: "npx",
	}
	encrypted, err := s.cipher.Encrypt("glpat_keep_me")
	s.Require().NoError(err)
	s.store.secrets["gitlab"] = []port.MCPSecretRecord{{
		Location: "env",
		Key:      "GITLAB_PERSONAL_ACCESS_TOKEN",
		Value:    encrypted,
	}}

	_, err = s.svc.Update(ctx, "gitlab", domain.UpdateMCPServerRequest{
		ID:        "gitlab",
		Transport: "stdio",
		Command:   "npx",
		Env:       map[string]string{"GITLAB_PERSONAL_ACCESS_TOKEN": secrets.MaskedValue()},
	})
	s.Require().NoError(err)

	records := s.store.secrets["gitlab"]
	s.Require().Len(records, 1)
	plain, err := s.cipher.Decrypt(records[0].Value)
	s.Require().NoError(err)
	s.Equal("glpat_keep_me", plain)
}

func (s *ServiceSuite) TestListViewsMasksSecrets() {
	ctx := context.Background()
	s.store.servers["gitlab"] = domain.MCPServer{
		ID: "gitlab", Transport: "stdio", Command: "npx",
	}
	encrypted, err := s.cipher.Encrypt("glpat_hidden")
	s.Require().NoError(err)
	s.store.secrets["gitlab"] = []port.MCPSecretRecord{{
		Location: "env",
		Key:      "GITLAB_PERSONAL_ACCESS_TOKEN",
		Value:    encrypted,
	}}

	views, err := s.svc.ListViews(ctx, nil)
	s.Require().NoError(err)
	s.Require().Len(views, 1)
	s.Equal(secrets.MaskedValue(), views[0].Env["GITLAB_PERSONAL_ACCESS_TOKEN"])
	s.NotEmpty(views[0].ConfigFields)
}

func (s *ServiceSuite) TestAccessMode() {
	ctx := context.Background()

	created, err := s.svc.Create(ctx, domain.CreateMCPServerRequest{ID: "figma", Transport: "http", URL: "https://mcp.figma.example/mcp"})
	s.Require().NoError(err)
	s.Equal(domain.MCPAccessListed, created.Access, "a new server reaches no agent until one names it")

	updated, err := s.svc.Update(ctx, "figma", domain.UpdateMCPServerRequest{Transport: "http", URL: "https://mcp.figma.example/mcp", Enabled: true})
	s.Require().NoError(err)
	s.Equal(domain.MCPAccessListed, updated.Access, "an update that does not mention access keeps it")

	updated, err = s.svc.Update(ctx, "figma", domain.UpdateMCPServerRequest{Transport: "http", URL: "https://mcp.figma.example/mcp", Access: domain.MCPAccessAll})
	s.Require().NoError(err)
	s.Equal(domain.MCPAccessAll, updated.Access)

	_, err = s.svc.Create(ctx, domain.CreateMCPServerRequest{ID: "odd", Transport: "stdio", Command: "npx", Access: "everyone"})
	s.ErrorIs(err, domain.ErrMCPInvalidRequest)

	cfgs, err := s.svc.ResolvedConfigs(ctx)
	s.Require().NoError(err)
	s.Require().Len(cfgs, 1)
	s.Equal(domain.MCPAccessAll, cfgs[0].Access, "the runtime config carries the mode to the registry")
}

func (s *ServiceSuite) TestSeededTemplatesKeepReachingEveryAgent() {
	s.Require().NoError(s.svc.SeedDefaultsIfEmpty(context.Background()))
	s.Require().NotEmpty(s.store.servers)
	for _, id := range []string{"filesystem", "git", "github", "gitlab", "postgres", "slack", "huggingface", "browser"} {
		server, ok := s.store.servers[id]
		s.Require().True(ok, id)
		s.Equal(domain.MCPAccessAll, server.Access, id)
	}
}

func (s *ServiceSuite) TestSeededSpecialistTemplatesReachOnlyTheAgentsThatNameThem() {
	s.Require().NoError(s.svc.SeedDefaultsIfEmpty(context.Background()))
	for _, id := range []string{"unity", "godot", "unreal", "blender", "jupyter", "duckdb", "dbt", "mlflow", "semgrep", "osv-scanner", "github-security", "snyk"} {
		server, ok := s.store.servers[id]
		s.Require().True(ok, id)
		s.Equal(domain.MCPAccessListed, server.Access, "%s drives an editor, a kernel or a scanner for one agent", id)
		s.False(server.Enabled, id)
	}
}

func TestServiceSuite(t *testing.T) {
	suite.Run(t, new(ServiceSuite))
}
