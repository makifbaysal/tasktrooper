package domain

import "time"

type Config struct {
	LLM           LLMConfig           `koanf:"llm"`
	Server        ServerConfig        `koanf:"server"`
	Tools         ToolsConfig         `koanf:"tools"`
	Storage       StorageConfig       `koanf:"storage"`
	Jobs          JobsConfig          `koanf:"jobs"`
	RAG           RAGConfig           `koanf:"rag"`
	Orchestration OrchestrationConfig `koanf:"orchestration"`
	Board         BoardConfig         `koanf:"board"`
	Context       ContextConfig       `koanf:"context"`
	Mapping       MappingConfig       `koanf:"mapping"`
	Indexer       IndexerConfig       `koanf:"indexer"`
	Embedding     EmbeddingConfig     `koanf:"embedding"`
	Graph         GraphConfig         `koanf:"graph"`
	Evolution     EvolutionConfig     `koanf:"evolution"`
	AgentCatalog  AgentCatalogConfig  `koanf:"agent_catalog"`
	ProdOps       ProdOpsConfig       `koanf:"prod_ops"`
	Storeops      StoreopsConfig      `koanf:"storeops"`
	DeployOps     DeployOpsConfig     `koanf:"deploy_ops"`
	ClaudeCode    ClaudeCodeConfig    `koanf:"claude_code"`
	Antigravity   AntigravityConfig   `koanf:"antigravity"`
	CursorAgent   CursorAgentConfig   `koanf:"cursor_agent"`
	Opencode      OpencodeConfig      `koanf:"opencode"`
}

// CursorAgentConfig drives the local Cursor CLI executor
// (internal/adapter/cli/cursor). No Enabled flag: the switch is whether the
// binary exists on this host.
type CursorAgentConfig struct {
	// Binary is the CLI to run, resolved on PATH; empty means "cursor-agent",
	// normally set from CURSOR_AGENT_BIN.
	Binary string `koanf:"binary"`
	// RunTimeout bounds one session end to end; 0 means the executor default
	// (1h).
	RunTimeout time.Duration `koanf:"run_timeout"`
}

// OpencodeConfig drives the local OpenCode CLI executor
// (internal/adapter/cli/opencode). No Enabled flag: the switch is whether the
// binary exists on this host.
type OpencodeConfig struct {
	// Binary is the CLI to run, resolved on PATH; empty means "opencode",
	// normally set from OPENCODE_BIN.
	Binary string `koanf:"binary"`
	// RunTimeout bounds one session end to end; 0 means the executor default
	// (1h).
	RunTimeout time.Duration `koanf:"run_timeout"`
}

// AntigravityConfig drives the local Antigravity (AGY) CLI executor
// (internal/adapter/cli/antigravity). No Enabled flag: the switch is whether
// the binary exists on this host.
type AntigravityConfig struct {
	// Binary is the CLI to run, resolved on PATH; empty means "agy", normally
	// set from ANTIGRAVITY_BIN.
	Binary string `koanf:"binary"`
	// MaxTurns bounds one CLI session; 0 means the executor default (100).
	MaxTurns int `koanf:"max_turns"`
	// RunTimeout bounds one session end to end; 0 means the executor default
	// (1h).
	RunTimeout time.Duration `koanf:"run_timeout"`
}

// ClaudeCodeConfig drives the local Claude Code CLI executor
// (internal/adapter/cli/claudecode). No Enabled flag: the switch is whether
// the binary exists on this host.
type ClaudeCodeConfig struct {
	// Binary is the CLI to run, resolved on PATH; empty means "claude", set
	// from CLAUDE_CODE_BIN.
	Binary string `koanf:"binary"`
	// MaxTurns bounds one CLI session; 0 means the executor default (100).
	MaxTurns int `koanf:"max_turns"`
	// RunTimeout bounds one session end to end — the only safeguard on a
	// wedged subprocess, whose heartbeat keeps the stale-run reconciler away.
	// 0 means the executor default (1h).
	RunTimeout time.Duration `koanf:"run_timeout"`
	// SettingSources is a subset of user, project, local; empty means
	// "project,local" — the operator's own ~/.claude settings are excluded.
	SettingSources string `koanf:"setting_sources"`
	// MaxConcurrentSessions bounds parallel CLI sessions; 0 means default (3),
	// negative unlimited. The cap keeps the shared subscription limit from
	// cutting every session off at once.
	MaxConcurrentSessions int `koanf:"max_concurrent_sessions"`
}

// ProdOpsConfig drives production monitoring: the health probe that watches
// deploy targets and turns a dead environment into an incident.
type ProdOpsConfig struct {
	MonitorEnabled bool          `koanf:"monitor_enabled"`
	ProbeInterval  time.Duration `koanf:"probe_interval"`
}

// StoreopsConfig drives the mobile store monitor.
type StoreopsConfig struct {
	PollInterval time.Duration `koanf:"poll_interval"`
}

// DeployOpsConfig drives the deploy monitor: it mirrors GitHub Actions runs
// locally and turns a failed deploy into an incident.
type DeployOpsConfig struct {
	MonitorEnabled bool          `koanf:"monitor_enabled"`
	PollInterval   time.Duration `koanf:"poll_interval"`
	// HealthWindow is how long after a successful deploy an incident is
	// attributed to the task that just released (the post-release watch and
	// auto_rollback window); 0 means the package default (15m). Short on
	// purpose: it authorises a rollback of a specific card.
	HealthWindow time.Duration `koanf:"health_window"`
}

// EvolutionConfig drives agent self-evolution.
type EvolutionConfig struct {
	Enabled            bool          `koanf:"enabled"`
	AllowWebResearch   bool          `koanf:"allow_web_research"`
	Model              string        `koanf:"model"`
	ProviderType       string        `koanf:"provider_type"`
	TickInterval       time.Duration `koanf:"tick_interval"`
	ReflectInterval    time.Duration `koanf:"reflect_interval"`
	RevisionDebounce   time.Duration `koanf:"revision_debounce"`
	ImpactWindow       time.Duration `koanf:"impact_window"`
	MinEventsForImpact int           `koanf:"min_events_for_impact"`
	MaxSkillChanges    int           `koanf:"max_skill_changes"`
	MaxRuleChanges     int           `koanf:"max_rule_changes"`
	// MaxSkillsPerAgent / MaxRulesPerAgent are the standing budget; at budget a
	// reflection can only update, merge or delete, never create.
	MaxSkillsPerAgent int `koanf:"max_skills_per_agent"`
	MaxRulesPerAgent  int `koanf:"max_rules_per_agent"`
	// GoldenGate runs the golden suite before AND after changes and reverts
	// when the after-run is worse.
	GoldenGate        bool   `koanf:"golden_gate"`
	JudgeModel        string `koanf:"judge_model"`
	JudgeProviderType string `koanf:"judge_provider_type"`
	MaxMemoryChanges  int    `koanf:"max_memory_changes"`
	MemoryMaxCount    int    `koanf:"memory_max_count"`
	EvidenceMaxChars  int    `koanf:"evidence_max_chars"`
}

// AgentCatalogConfig drives the external agents/skills catalog: a git clone
// (or a local directory) the app watches and pulls agent definitions from.
// Disabled by an empty Source.
type AgentCatalogConfig struct {
	Source string `koanf:"source"`
	// CacheDir is where a git Source is cloned; a directory Source is used in
	// place.
	CacheDir string `koanf:"cache_dir"`
	// Interval between background syncs after the boot-time one; 0 = the 15m
	// default.
	Interval time.Duration `koanf:"interval"`
}

type BoardConfig struct {
	DispatchEnabled         bool              `koanf:"dispatch_enabled"`
	VerificationEnabled     bool              `koanf:"verification_enabled"`
	VerifyMaxFixAttempts    int               `koanf:"verify_max_fix_attempts"`
	RequireCriteriaComplete bool              `koanf:"require_criteria_complete"`
	TaskTypeModels          map[string]string `koanf:"task_type_models"`
	// ReconcileStaleAfter/ReconcileInterval drive the reconciler that recovers
	// task_agent_runs orphaned by a restart/dropped job (dispatch is otherwise
	// purely event-driven). Generous: a run has no per-iteration timeout.
	ReconcileStaleAfter time.Duration `koanf:"reconcile_stale_after"`
	ReconcileInterval   time.Duration `koanf:"reconcile_interval"`
	// PipelineGateTimeout/Interval exist because the gate's only key used to
	// be the pipeline-finalize event nobody guarantees (restart, dropped
	// delivery, CI out of minutes), which left cards in code_review forever.
	PipelineGateTimeout  time.Duration `koanf:"pipeline_gate_timeout"`
	PipelineGateInterval time.Duration `koanf:"pipeline_gate_interval"`
}

type LLMConfig struct {
	BaseURL string `koanf:"base_url"`
	Model   string `koanf:"model"`
	APIKey  string `koanf:"api_key"`
	// MaxIterations bounds a chat turn.
	MaxIterations int `koanf:"max_iterations"`
	// TaskMaxIterations bounds a board task/orchestration subtask, which needs
	// several times a chat's turns; sharing MaxIterations killed real runs
	// mid-edit.
	TaskMaxIterations int `koanf:"task_max_iterations"`
	// RunMaxTotalTokens is a mid-run circuit breaker on total prompt+completion
	// tokens, taking the same wrap-up path as an exhausted iteration budget;
	// billing only gates before a run starts. 0 disables it.
	RunMaxTotalTokens int           `koanf:"run_max_total_tokens"`
	Timeout           time.Duration `koanf:"timeout"`
}

type ServerConfig struct {
	Port    int            `koanf:"port"`
	APIKey  string         `koanf:"api_key"`
	APIKeys []APIKeyConfig `koanf:"api_keys"`
	// PublicBaseURL is the externally reachable origin, where GitHub push
	// webhooks are pointed; empty disables webhook setup.
	PublicBaseURL string `koanf:"public_base_url"`
}

type APIKeyConfig struct {
	Key        string     `koanf:"key"`
	Name       string     `koanf:"name"`
	ToolPolicy ToolPolicy `koanf:"tool_policy"`
}

type ToolsConfig struct {
	DefaultPolicy      ToolPolicy               `koanf:"default_policy"`
	Search             SearchConfig             `koanf:"search"`
	Terminal           TerminalConfig           `koanf:"terminal"`
	Web                WebConfig                `koanf:"web"`
	Browser            BrowserConfig            `koanf:"browser"`
	Mobile             MobileConfig             `koanf:"mobile"`
	BoilerplateCatalog BoilerplateCatalogConfig `koanf:"boilerplate_catalog"`
	MaxToolOutputChars int                      `koanf:"max_tool_output_chars"`
}

type ContextConfig struct {
	MaxTokens          int `koanf:"max_tokens"`
	ReserveOutput      int `koanf:"reserve_output"`
	SummarizeThreshold int `koanf:"summarize_threshold"`
	KeepRecentMessages int `koanf:"keep_recent_messages"`
}

type MappingConfig struct {
	Enabled              bool `koanf:"enabled"`
	TreeMaxDepth         int  `koanf:"tree_max_depth"`
	MaxFiles             int  `koanf:"max_files"`
	SkeletonMaxTokens    int  `koanf:"skeleton_max_tokens"`
	InjectOnSessionStart bool `koanf:"inject_on_session_start"`
}

type IndexerConfig struct {
	Enabled         bool     `koanf:"enabled"`
	TopK            int      `koanf:"top_k"`
	ReindexOnChange bool     `koanf:"reindex_on_change"`
	ChunkMaxLines   int      `koanf:"chunk_max_lines"`
	AllowedRoots    []string `koanf:"allowed_roots"`
	QueryRewrite    bool     `koanf:"query_rewrite"`
	// Concurrency is how many files are chunked/embedded/stored at once;
	// indexing mostly waits on the embedding endpoint. 0 = default.
	Concurrency int `koanf:"concurrency"`
	// EmbedConcurrency bounds embedding calls across every index job; 0 = the
	// indexer's default (2).
	EmbedConcurrency int `koanf:"embed_concurrency"`
	// IndexGenerated keeps generated code and mocks in the code index; false
	// (the default) leaves them out so they do not crowd real code out of
	// search results.
	IndexGenerated bool `koanf:"index_generated"`
}

// EmbeddingConfig paces embedding requests (one per chunk back to back used to
// 429-fail whole indexes at ~9%).
type EmbeddingConfig struct {
	// RequestsPerMinute caps embedding calls; 0 leaves them unthrottled.
	RequestsPerMinute int `koanf:"requests_per_minute"`
	// MaxRetries is how many times a rate-limited call is retried; 0 = the
	// loader default, negative turns retrying off.
	MaxRetries int `koanf:"max_retries"`
	// RetryBackoff is the first wait after a 429, doubling per attempt, used
	// only when the provider sends no Retry-After.
	RetryBackoff time.Duration `koanf:"retry_backoff"`
	// MaxRetryWait caps a single wait, including a provider's Retry-After.
	MaxRetryWait time.Duration `koanf:"max_retry_wait"`
	// RequestTimeout budgets one embedding call; the indexer's per-chunk
	// deadline derives from it plus the retry waits.
	RequestTimeout time.Duration `koanf:"request_timeout"`
	// QueryCacheEntries bounds the LRU caching query embedding vectors keyed by
	// (provider, model, exact text); 0 = unset, negative disables.
	QueryCacheEntries int `koanf:"query_cache_entries"`
}

type GraphConfig struct {
	Enabled           bool `koanf:"enabled"`
	MaxExpansionDepth int  `koanf:"max_expansion_depth"`
	MaxExpandedChunks int  `koanf:"max_expanded_chunks"`
}

type SearchConfig struct {
	Enabled    bool `koanf:"enabled"`
	MaxResults int  `koanf:"max_results"`
}

type TerminalConfig struct {
	Enabled    bool          `koanf:"enabled"`
	WorkingDir string        `koanf:"working_dir"`
	Timeout    time.Duration `koanf:"timeout"`
	// MaxTimeout is the ceiling for a slow call (dependency install, cold
	// build); Timeout is the default for an unspecified one.
	MaxTimeout time.Duration         `koanf:"max_timeout"`
	Sandbox    TerminalSandboxConfig `koanf:"sandbox"`
}

type TerminalSandboxConfig struct {
	Mode               string   `koanf:"mode"`
	AllowedCommands    []string `koanf:"allowed_commands"`
	BlockedPatterns    []string `koanf:"blocked_patterns"`
	RestrictWorkingDir bool     `koanf:"restrict_working_dir"`
}

type WebConfig struct {
	Enabled          bool  `koanf:"enabled"`
	MaxResponseBytes int64 `koanf:"max_response_bytes"`
}

type BoilerplateCatalogConfig struct {
	Enabled bool `koanf:"enabled"`
}

type BrowserConfig struct {
	Enabled bool `koanf:"enabled"`
}

// MobileConfig attaches a real Android device, reached through an Appium
// server, to the mobile_* tools. No Enabled flag: a hub URL is the only honest
// switch — an installation with no hub and no device has nothing to enable.
type MobileConfig struct {
	// HubURL is the Appium server base.
	HubURL string `koanf:"hub_url"`
	// AppiumBin, set, makes the hub at HubURL this process's to start when a
	// mobile tool needs it and to stop when idle. Unset, whoever runs it does.
	AppiumBin string `koanf:"appium_bin"`
	// DeviceUDID pins which phone; Appium would otherwise take whatever adb
	// lists first.
	DeviceUDID string `koanf:"device_udid"`
	// PlatformVersion optionally makes the capability set explicit.
	PlatformVersion string `koanf:"platform_version"`
	// DevicePIN unlocks the lock screen each session; a credential, from the
	// environment, never logged, never a tool argument.
	DevicePIN string `koanf:"device_pin"`
	// AuthToken is sent to the hub as a bearer token.
	AuthToken string `koanf:"auth_token"`
	// BridgeURL is the adb sidecar (cmd/device-agent) that lets the settings
	// UI pair and connect a phone.
	BridgeURL   string `koanf:"bridge_url"`
	BridgeToken string `koanf:"bridge_token"`
}

type MCPServerConfig struct {
	ID           string            `koanf:"id"`
	Enabled      bool              `koanf:"enabled"`
	Transport    string            `koanf:"transport"`
	Command      string            `koanf:"command"`
	Args         []string          `koanf:"args"`
	Env          map[string]string `koanf:"env"`
	URL          string            `koanf:"url"`
	Headers      map[string]string `koanf:"headers"`
	AllowedTools []string          `koanf:"allowed_tools"`
	Access       MCPAccess         `koanf:"access"`
}

type StorageConfig struct {
	Postgres PostgresConfig `koanf:"postgres"`
	Sessions SessionsConfig `koanf:"sessions"`
}

type PostgresConfig struct {
	DSN      string `koanf:"dsn"`
	MaxConns int32  `koanf:"max_conns"`
}

type SessionsConfig struct {
	TTL           time.Duration `koanf:"ttl"`
	WorkspaceRoot string        `koanf:"workspace_root"`
}

type JobsConfig struct {
	MaxConcurrent int           `koanf:"max_concurrent"`
	Timeout       time.Duration `koanf:"timeout"`
}

type RAGConfig struct {
	Enabled      bool   `koanf:"enabled"`
	StorageDir   string `koanf:"storage_dir"`
	ChunkSize    int    `koanf:"chunk_size"`
	ChunkOverlap int    `koanf:"chunk_overlap"`
	TopK         int    `koanf:"top_k"`
}

func (c *Config) ExpandEnv() {
	c.LLM.BaseURL = EnvExpandString(c.LLM.BaseURL)
	c.LLM.Model = EnvExpandString(c.LLM.Model)
	c.LLM.APIKey = EnvExpandString(c.LLM.APIKey)
	c.Server.APIKey = EnvExpandString(c.Server.APIKey)
	c.Server.PublicBaseURL = EnvExpandString(c.Server.PublicBaseURL)
	for i := range c.Server.APIKeys {
		c.Server.APIKeys[i].Key = EnvExpandString(c.Server.APIKeys[i].Key)
		c.Server.APIKeys[i].Name = EnvExpandString(c.Server.APIKeys[i].Name)
	}
	c.Tools.Terminal.WorkingDir = EnvExpandString(c.Tools.Terminal.WorkingDir)
	c.Storage.Postgres.DSN = EnvExpandString(c.Storage.Postgres.DSN)
	c.Storage.Sessions.WorkspaceRoot = EnvExpandString(c.Storage.Sessions.WorkspaceRoot)
	c.RAG.StorageDir = EnvExpandString(c.RAG.StorageDir)
	for i := range c.Indexer.AllowedRoots {
		c.Indexer.AllowedRoots[i] = EnvExpandString(c.Indexer.AllowedRoots[i])
	}
}
