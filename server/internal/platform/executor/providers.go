package executor

import (
	"context"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/llm"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/mcp"
	execapp "github.com/makifbaysal/tasktrooper/server/internal/application/executor"
	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// buildProviders turns the config's providers into one client that routes a
// request by provider id. Ids rather than types key the set because a user
// may hold two providers of one type (two OpenAI-compatible endpoints), the
// way the server keys an endpoint by its uuid. The keys stay in these clients'
// memory and nowhere else.
func buildProviders(configs []ProviderConfig, defaultTimeout time.Duration) (port.LLMClient, map[string]execapp.Provider) {
	set := llm.ProviderSet{Clients: make(map[domain.LLMProviderType]port.LLMClient, len(configs))}
	providers := make(map[string]execapp.Provider, len(configs))
	for _, cfg := range configs {
		typ, _ := wireType(cfg.Type)
		def, _ := domain.LLMProviderDefinitionFor(typ)
		baseURL := strings.TrimSpace(cfg.BaseURL)
		if baseURL == "" {
			baseURL = def.DefaultBaseURL
		}
		defaultModel := def.DefaultModel
		for _, m := range cfg.Models {
			if m = strings.TrimSpace(m); m != "" {
				defaultModel = m
				break
			}
		}
		timeout := llmprovider.ResolveTimeoutDuration(cfg.TimeoutSeconds, typ, defaultTimeout)
		set.Clients[domain.LLMProviderType(cfg.ID)] = llm.NewProviderClient(typ, baseURL, defaultModel, cfg.APIKey, timeout)
		if set.Default == "" {
			set.Default = domain.LLMProviderType(cfg.ID)
		}
		providers[cfg.ID] = execapp.Provider{Type: typ, DefaultModel: defaultModel}
	}
	return llm.NewMultiProviderClient(nil, llm.StaticResolver(set)), providers
}

func providerSecrets(configs []ProviderConfig) []string {
	secrets := make([]string, 0, len(configs))
	for _, cfg := range configs {
		if key := strings.TrimSpace(cfg.APIKey); key != "" {
			secrets = append(secrets, key)
		}
	}
	return secrets
}

func memberSecrets(servers []MCPServerConfig) []string {
	var secrets []string
	for _, m := range servers {
		secrets = append(secrets, m.secrets()...)
	}
	return secrets
}

func connectMembers(servers []MCPServerConfig) *mcp.Members {
	if len(servers) == 0 {
		return nil
	}
	configs := make([]domain.MCPServerConfig, 0, len(servers))
	for _, m := range servers {
		configs = append(configs, m.domainConfig())
	}
	return mcp.ConnectMembers(context.Background(), configs)
}
