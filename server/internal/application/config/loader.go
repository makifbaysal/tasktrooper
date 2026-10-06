package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/resources"
)

func Load(path string) (*domain.Config, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Parse(resources.ConfigYAML)
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(raw)
}

func Parse(raw []byte) (*domain.Config, error) {
	expanded := expandEnv(string(raw))

	k := koanf.New(".")
	if err := k.Load(rawbytes.Provider([]byte(expanded)), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	var cfg domain.Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg.ExpandEnv()
	applyDefaults(&cfg)

	if !k.Exists("tools.browser.enabled") {
		cfg.Tools.Browser.Enabled = true
	}
	return &cfg, nil
}

func applyDefaults(cfg *domain.Config) {
	if cfg.LLM.MaxIterations <= 0 {
		cfg.LLM.MaxIterations = 30
	}
	if cfg.LLM.TaskMaxIterations <= 0 {
		cfg.LLM.TaskMaxIterations = 80
	}

	if cfg.Tools.Web.MaxResponseBytes <= 0 {
		cfg.Tools.Web.MaxResponseBytes = 1048576
	}
	if cfg.Storage.Postgres.MaxConns <= 0 {
		cfg.Storage.Postgres.MaxConns = 10
	}
	if cfg.Storage.Sessions.TTL <= 0 {
		cfg.Storage.Sessions.TTL = 24 * time.Hour
	}
	if cfg.Jobs.MaxConcurrent <= 0 {
		cfg.Jobs.MaxConcurrent = 3
	}
	if cfg.Jobs.Timeout <= 0 {
		cfg.Jobs.Timeout = 10 * time.Minute
	}
	if cfg.Board.VerifyMaxFixAttempts <= 0 {
		cfg.Board.VerifyMaxFixAttempts = 2
	}
	if cfg.Board.ReconcileStaleAfter <= 0 {
		cfg.Board.ReconcileStaleAfter = 30 * time.Minute
	}

	if cfg.Board.ReconcileInterval <= 0 {
		cfg.Board.ReconcileInterval = time.Minute
	}

	if cfg.Board.PipelineGateTimeout <= 0 {
		cfg.Board.PipelineGateTimeout = 45 * time.Minute
	}
	if cfg.Board.PipelineGateInterval <= 0 {
		cfg.Board.PipelineGateInterval = 2 * time.Minute
	}
	if cfg.RAG.ChunkSize <= 0 {
		cfg.RAG.ChunkSize = 1000
	}
	if cfg.RAG.ChunkOverlap <= 0 {
		cfg.RAG.ChunkOverlap = 200
	}
	if cfg.RAG.TopK <= 0 {
		cfg.RAG.TopK = 5
	}
	if cfg.RAG.StorageDir == "" {
		cfg.RAG.StorageDir = "./data/files"
	}

	if cfg.Embedding.MaxRetries == 0 {
		cfg.Embedding.MaxRetries = 5
	}
	if cfg.Embedding.RetryBackoff <= 0 {
		cfg.Embedding.RetryBackoff = 2 * time.Second
	}
	if cfg.Embedding.MaxRetryWait <= 0 {
		cfg.Embedding.MaxRetryWait = 60 * time.Second
	}
	if cfg.Embedding.RequestTimeout <= 0 {
		cfg.Embedding.RequestTimeout = 90 * time.Second
	}

	if cfg.Embedding.QueryCacheEntries == 0 {
		cfg.Embedding.QueryCacheEntries = 2048
	}
	if cfg.Evolution.TickInterval <= 0 {
		cfg.Evolution.TickInterval = 10 * time.Minute
	}
	if cfg.Evolution.ReflectInterval <= 0 {
		cfg.Evolution.ReflectInterval = 24 * time.Hour
	}
	if cfg.Evolution.RevisionDebounce <= 0 {
		cfg.Evolution.RevisionDebounce = 30 * time.Minute
	}
	if cfg.Evolution.ImpactWindow <= 0 {
		cfg.Evolution.ImpactWindow = 7 * 24 * time.Hour
	}
	if cfg.Evolution.MinEventsForImpact <= 0 {
		cfg.Evolution.MinEventsForImpact = 3
	}
	if cfg.Evolution.MaxSkillChanges <= 0 {
		cfg.Evolution.MaxSkillChanges = 3
	}
	if cfg.Evolution.MaxRuleChanges <= 0 {
		cfg.Evolution.MaxRuleChanges = 3
	}
	if cfg.Evolution.MaxSkillsPerAgent <= 0 {
		cfg.Evolution.MaxSkillsPerAgent = 100
	}
	if cfg.Evolution.MaxRulesPerAgent <= 0 {
		cfg.Evolution.MaxRulesPerAgent = 15
	}
	if cfg.Evolution.MaxMemoryChanges <= 0 {
		cfg.Evolution.MaxMemoryChanges = 5
	}
	if cfg.Evolution.MemoryMaxCount <= 0 {
		cfg.Evolution.MemoryMaxCount = 200
	}
	if cfg.Evolution.EvidenceMaxChars <= 0 {
		cfg.Evolution.EvidenceMaxChars = 24000
	}
	if cfg.AgentCatalog.CacheDir == "" {
		cfg.AgentCatalog.CacheDir = "./data/catalog"
	}
	if cfg.AgentCatalog.Interval <= 0 {
		cfg.AgentCatalog.Interval = 15 * time.Minute
	}
	if cfg.Storeops.PollInterval <= 0 {
		cfg.Storeops.PollInterval = 5 * time.Minute
	}
	if cfg.DeployOps.PollInterval <= 0 {
		cfg.DeployOps.PollInterval = 2 * time.Minute
	}

	if strings.TrimSpace(cfg.ClaudeCode.Binary) == "" {
		cfg.ClaudeCode.Binary = "claude"
	}

}
