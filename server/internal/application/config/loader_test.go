package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/config"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ConfigLoaderSuite struct {
	suite.Suite
}

func (s *ConfigLoaderSuite) TestLoadValidConfig() {
	content := `
llm:
  base_url: "http://localhost:1234/v1"
  model: "test-model"
  api_key: "secret"
  max_iterations: 5
  timeout: "30s"

server:
  port: 9090
  api_key: "bridge-key"

tools:
  terminal:
    enabled: true
    working_dir: "/tmp"
    timeout: "15s"
  web:
    enabled: false
    max_response_bytes: 512000
`
	f := s.writeTempConfig(content)
	defer os.Remove(f)

	cfg, err := config.Load(f)
	s.NoError(err)
	s.Equal("http://localhost:1234/v1", cfg.LLM.BaseURL)
	s.Equal("test-model", cfg.LLM.Model)
	s.Equal("secret", cfg.LLM.APIKey)
	s.Equal(5, cfg.LLM.MaxIterations)
	s.Equal(30*time.Second, cfg.LLM.Timeout)
	s.Equal(9090, cfg.Server.Port)
	s.Equal("bridge-key", cfg.Server.APIKey)
	s.True(cfg.Tools.Terminal.Enabled)
	s.Equal("/tmp", cfg.Tools.Terminal.WorkingDir)
	s.Equal(15*time.Second, cfg.Tools.Terminal.Timeout)
	s.False(cfg.Tools.Web.Enabled)
	s.Equal(int64(512000), cfg.Tools.Web.MaxResponseBytes)
}

func (s *ConfigLoaderSuite) TestDefaultMaxIterations() {
	content := `
llm:
  base_url: "http://localhost:1234/v1"
server:
  port: 8080
`
	f := s.writeTempConfig(content)
	defer os.Remove(f)

	cfg, err := config.Load(f)
	s.NoError(err)
	s.Equal(30, cfg.LLM.MaxIterations)

	s.Equal(80, cfg.LLM.TaskMaxIterations)
}

func (s *ConfigLoaderSuite) TestDefaultMaxResponseBytes() {
	content := `
llm:
  base_url: "http://localhost:1234/v1"
server:
  port: 8080
tools:
  web:
    enabled: true
`
	f := s.writeTempConfig(content)
	defer os.Remove(f)

	cfg, err := config.Load(f)
	s.NoError(err)
	s.Equal(int64(1048576), cfg.Tools.Web.MaxResponseBytes)
}

func (s *ConfigLoaderSuite) TestDefaultBrowserEnabled() {
	content := `
llm:
  base_url: "http://localhost:1234/v1"
server:
  port: 8080
`
	f := s.writeTempConfig(content)
	defer os.Remove(f)

	cfg, err := config.Load(f)
	s.NoError(err)
	s.True(cfg.Tools.Browser.Enabled)
}

func (s *ConfigLoaderSuite) TestBrowserExplicitlyDisabled() {
	content := `
llm:
  base_url: "http://localhost:1234/v1"
server:
  port: 8080
tools:
  browser:
    enabled: false
`
	f := s.writeTempConfig(content)
	defer os.Remove(f)

	cfg, err := config.Load(f)
	s.NoError(err)
	s.False(cfg.Tools.Browser.Enabled)
}

func (s *ConfigLoaderSuite) TestFileNotFoundFallsBackToTheEmbeddedConfig() {
	cfg, err := config.Load("/nonexistent/path/config.yml")
	s.Require().NoError(err)
	s.Equal(8085, cfg.Server.Port)
}

func (s *ConfigLoaderSuite) TestTheEmbeddedConfigBoundsAlertIngestTokens() {
	cfg, err := config.Load("/nonexistent/path/config.yml")
	s.Require().NoError(err)
	s.Equal(domain.AlertIngestLimits{RequestsPerMinute: 60, Burst: 10, NewIncidentsPerHour: 20}, cfg.ProdOps.AlertIngest)
}

func (s *ConfigLoaderSuite) TestToolsConfigWithoutMCPServers() {
	content := `
llm:
  base_url: "http://localhost:1234/v1"
server:
  port: 8080
tools:
  default_policy:
    allow_tools: ["web_search", "fetch_url"]
`
	f := s.writeTempConfig(content)
	defer os.Remove(f)

	cfg, err := config.Load(f)
	s.NoError(err)
	s.Equal("web_search", cfg.Tools.DefaultPolicy.AllowTools[0])
}

func (s *ConfigLoaderSuite) TestEnvExpansion() {
	os.Setenv("TEST_BRIDGE_KEY", "from-env")
	os.Setenv("TEST_LM_URL", "http://env-host:1234/v1")
	defer os.Unsetenv("TEST_BRIDGE_KEY")
	defer os.Unsetenv("TEST_LM_URL")

	content := `
llm:
  base_url: "${TEST_LM_URL}"
server:
  port: 8080
  api_key: "${TEST_BRIDGE_KEY}"
`
	f := s.writeTempConfig(content)
	defer os.Remove(f)

	cfg, err := config.Load(f)
	s.NoError(err)
	s.Equal("http://env-host:1234/v1", cfg.LLM.BaseURL)
	s.Equal("from-env", cfg.Server.APIKey)
}

func (s *ConfigLoaderSuite) writeTempConfig(content string) string {
	f, err := os.CreateTemp("", "config-*.yml")
	s.Require().NoError(err)
	_, err = f.WriteString(content)
	s.Require().NoError(err)
	s.Require().NoError(f.Close())
	return f.Name()
}

func TestConfigLoaderSuite(t *testing.T) {
	suite.Run(t, new(ConfigLoaderSuite))
}

func (s *ConfigLoaderSuite) TestDefaultStoreopsPollInterval() {
	cfg, err := config.Parse([]byte(`
server:
  port: 8080
`))
	s.Require().NoError(err)
	s.Equal(5*time.Minute, cfg.Storeops.PollInterval)
}

func (s *ConfigLoaderSuite) TestStoreopsPollIntervalOverride() {
	cfg, err := config.Parse([]byte(`
server:
  port: 8080
storeops:
  poll_interval: "10m"
`))
	s.Require().NoError(err)
	s.Equal(10*time.Minute, cfg.Storeops.PollInterval)
}

func (s *ConfigLoaderSuite) TestDefaultEmbeddingQueryCacheEntries() {
	cfg, err := config.Parse([]byte(`
server:
  port: 8080
`))
	s.Require().NoError(err)
	s.Equal(2048, cfg.Embedding.QueryCacheEntries)
}

func (s *ConfigLoaderSuite) TestEmbeddingQueryCacheEntriesOverride() {
	cfg, err := config.Parse([]byte(`
server:
  port: 8080
embedding:
  query_cache_entries: 64
`))
	s.Require().NoError(err)
	s.Equal(64, cfg.Embedding.QueryCacheEntries)
}

func (s *ConfigLoaderSuite) TestEmbeddingQueryCacheEntriesNegativeDisables() {
	cfg, err := config.Parse([]byte(`
server:
  port: 8080
embedding:
  query_cache_entries: -1
`))
	s.Require().NoError(err)
	s.Equal(-1, cfg.Embedding.QueryCacheEntries)
}
