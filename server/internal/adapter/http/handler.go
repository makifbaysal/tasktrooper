package http

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"time"

	goccyjson "github.com/goccy/go-json"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"

	llmadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/llm"
	mcpadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/mcp"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/mcpserver"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	agentcliapp "github.com/makifbaysal/tasktrooper/server/internal/application/agentcli"
	"github.com/makifbaysal/tasktrooper/server/internal/application/apikey"
	"github.com/makifbaysal/tasktrooper/server/internal/application/attachment"
	"github.com/makifbaysal/tasktrooper/server/internal/application/billing"
	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/application/deploy"
	"github.com/makifbaysal/tasktrooper/server/internal/application/deployops"
	"github.com/makifbaysal/tasktrooper/server/internal/application/embedmap"
	"github.com/makifbaysal/tasktrooper/server/internal/application/evolution"
	"github.com/makifbaysal/tasktrooper/server/internal/application/gcloudops"
	"github.com/makifbaysal/tasktrooper/server/internal/application/hosting"
	"github.com/makifbaysal/tasktrooper/server/internal/application/indexer"
	"github.com/makifbaysal/tasktrooper/server/internal/application/initiative"
	"github.com/makifbaysal/tasktrooper/server/internal/application/job"
	"github.com/makifbaysal/tasktrooper/server/internal/application/kpi"
	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mcp"
	"github.com/makifbaysal/tasktrooper/server/internal/application/memory"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mobiledevice"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/application/rag"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/repodependency"
	"github.com/makifbaysal/tasktrooper/server/internal/application/repodocs"
	"github.com/makifbaysal/tasktrooper/server/internal/application/repository"
	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/application/settings"
	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/application/vercelops"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/resources"
)

// BoardRunControl stops a live board run and re-queues a finished one.
// Declared here rather than imported so this package stays clear of the board
// package (which imports the runner, the agent loop and everything under them).
type BoardRunControl interface {
	CancelRun(ctx context.Context, repositoryID, taskID, runID uuid.UUID, reason string) (domain.TaskAgentRun, error)
	RerunRun(ctx context.Context, repositoryID, taskID, runID uuid.UUID) (domain.TaskAgentRun, error)
}

// TaskChatControl opens (or reuses) the chat thread about one board task and
// reports the session to talk in and the agent answering there. Declared here for
// the same reason as BoardRunControl: it keeps this package clear of the board
// package and everything the runner drags in.
type TaskChatControl interface {
	Open(ctx context.Context, repositoryID, taskID uuid.UUID) (sessionID uuid.UUID, agentID uuid.UUID, err error)
}

type Handler struct {
	agentLoop     *agent.Loop
	llmClient     port.LLMClient
	registry      port.ToolRegistry
	defaultPolicy domain.ToolPolicy
	legacyAPIKey  string
	apiKeys       []domain.APIKeyConfig
	apiKeySvc     *apikey.Service
	// bootSeed retries a board seed that failed at boot and reports whether the
	// boot steps are still running. Nil on a build with no database.
	bootSeed          BootSeed
	sessionSvc        *session.Service
	settingsSvc       *settings.Service
	mobileDeviceSvc   *mobiledevice.Service
	githubTokens      port.GitHubTokenStore
	llmProviderSvc    *llmprovider.Service
	agentCLISvc       *agentcliapp.Service
	jobSvc            *job.Service
	ragSvc            *rag.Service
	attachmentSvc     *attachment.Service
	catalogSvc        *catalog.Service
	mcpSvc            *mcp.Service
	auditStore        port.AuditLogger
	mcpManager        *mcpadapter.Manager
	reloadFn          func() error
	metrics           *Metrics
	indexSvc          *indexer.Service
	indexAllowedRoots []string
	embedMapSvc       *embedmap.Service
	repositorySvc     *repository.Service
	initiativeSvc     *initiative.Service
	workspaceSvc      *workspace.Service
	boardEvents       port.BoardEventStore
	taskRuns          port.TaskAgentRunStore
	runControl        BoardRunControl
	taskChat          TaskChatControl
	evolutionSvc      *evolution.Service
	memorySvc         *memory.Service
	kpiSvc            *kpi.Service
	perfStore         port.AgentPerformanceStore
	goldenStore       port.GoldenTaskStore
	usageStore        port.LLMUsageStore
	billingSvc        *billing.Service
	uiRoot            string
	uiFS              fs.FS
	deploySvc         *deploy.Service
	repoDocsSvc       *repodocs.Service
	prodOpsSvc        *prodops.Service
	storeOpsSvc       *storeops.Service
	deployOpsSvc      *deployops.Service
	hostingSvc        *hosting.Service
	repoDependencySvc *repodependency.Service
	vercelOpsSvc      *vercelops.Service
	gcloudOpsSvc      *gcloudops.Service
	// mcpToolServer serves TaskTrooper's tools to a local Claude Code session.
	// Nil on every host without the CLI, in which case no route is mounted.
	mcpToolServer *mcpserver.Server
}

type Config struct {
	AgentLoop         *agent.Loop
	LLMClient         port.LLMClient
	Registry          port.ToolRegistry
	DefaultPolicy     domain.ToolPolicy
	LegacyAPIKey      string
	APIKeys           []domain.APIKeyConfig
	APIKeySvc         *apikey.Service
	SessionSvc        *session.Service
	SettingsSvc       *settings.Service
	MobileDeviceSvc   *mobiledevice.Service
	GitHubTokens      port.GitHubTokenStore
	LLMProviderSvc    *llmprovider.Service
	AgentCLISvc       *agentcliapp.Service
	JobSvc            *job.Service
	RAGSvc            *rag.Service
	AttachmentSvc     *attachment.Service
	CatalogSvc        *catalog.Service
	MCPSvc            *mcp.Service
	AuditStore        port.AuditLogger
	MCPManager        *mcpadapter.Manager
	ReloadFn          func() error
	IndexSvc          *indexer.Service
	IndexAllowedRoots []string
	EmbedMapSvc       *embedmap.Service
	RepositorySvc     *repository.Service
	InitiativeSvc     *initiative.Service
	WorkspaceSvc      *workspace.Service
	BoardEvents       port.BoardEventStore
	TaskRuns          port.TaskAgentRunStore
	RunControl        BoardRunControl
	TaskChat          TaskChatControl
	EvolutionSvc      *evolution.Service
	MemorySvc         *memory.Service
	KPISvc            *kpi.Service
	PerfStore         port.AgentPerformanceStore
	GoldenStore       port.GoldenTaskStore
	UsageStore        port.LLMUsageStore
	BillingSvc        *billing.Service
	UIRoot            string
	UIFS              fs.FS
	DeploySvc         *deploy.Service
	RepoDocsSvc       *repodocs.Service
	ProdOpsSvc        *prodops.Service
	StoreOpsSvc       *storeops.Service
	DeployOpsSvc      *deployops.Service
	HostingSvc        *hosting.Service
	RepoDependencySvc *repodependency.Service
	VercelOpsSvc      *vercelops.Service
	GCloudOpsSvc      *gcloudops.Service
	MCPToolServer     *mcpserver.Server
	BootSeed          BootSeed
}

func NewHandler(cfg Config) *Handler {
	return &Handler{
		agentLoop:         cfg.AgentLoop,
		llmClient:         cfg.LLMClient,
		registry:          cfg.Registry,
		defaultPolicy:     cfg.DefaultPolicy,
		legacyAPIKey:      cfg.LegacyAPIKey,
		apiKeys:           cfg.APIKeys,
		apiKeySvc:         cfg.APIKeySvc,
		sessionSvc:        cfg.SessionSvc,
		settingsSvc:       cfg.SettingsSvc,
		mobileDeviceSvc:   cfg.MobileDeviceSvc,
		githubTokens:      cfg.GitHubTokens,
		llmProviderSvc:    cfg.LLMProviderSvc,
		agentCLISvc:       cfg.AgentCLISvc,
		jobSvc:            cfg.JobSvc,
		ragSvc:            cfg.RAGSvc,
		attachmentSvc:     cfg.AttachmentSvc,
		catalogSvc:        cfg.CatalogSvc,
		mcpSvc:            cfg.MCPSvc,
		auditStore:        cfg.AuditStore,
		mcpManager:        cfg.MCPManager,
		reloadFn:          cfg.ReloadFn,
		metrics:           NewMetrics(),
		indexSvc:          cfg.IndexSvc,
		indexAllowedRoots: cfg.IndexAllowedRoots,
		embedMapSvc:       cfg.EmbedMapSvc,
		repositorySvc:     cfg.RepositorySvc,
		initiativeSvc:     cfg.InitiativeSvc,
		workspaceSvc:      cfg.WorkspaceSvc,
		boardEvents:       cfg.BoardEvents,
		taskRuns:          cfg.TaskRuns,
		runControl:        cfg.RunControl,
		taskChat:          cfg.TaskChat,
		evolutionSvc:      cfg.EvolutionSvc,
		memorySvc:         cfg.MemorySvc,
		kpiSvc:            cfg.KPISvc,
		perfStore:         cfg.PerfStore,
		goldenStore:       cfg.GoldenStore,
		usageStore:        cfg.UsageStore,
		billingSvc:        cfg.BillingSvc,
		uiRoot:            cfg.UIRoot,
		uiFS:              cfg.UIFS,
		deploySvc:         cfg.DeploySvc,
		repoDocsSvc:       cfg.RepoDocsSvc,
		prodOpsSvc:        cfg.ProdOpsSvc,
		storeOpsSvc:       cfg.StoreOpsSvc,
		deployOpsSvc:      cfg.DeployOpsSvc,
		hostingSvc:        cfg.HostingSvc,
		repoDependencySvc: cfg.RepoDependencySvc,
		vercelOpsSvc:      cfg.VercelOpsSvc,
		gcloudOpsSvc:      cfg.GCloudOpsSvc,
		mcpToolServer:     cfg.MCPToolServer,
		bootSeed:          cfg.BootSeed,
	}
}

// RegisterRoutes mounts every route. CORS is NOT set here: the origin list is
// configuration (CORS_ORIGINS) and belongs with the process that read it, so
// platform/runtime installs it before this runs.
func (h *Handler) RegisterRoutes(app *fiber.App) {
	app.Use(h.requestIDMiddleware)
	app.Use(h.authMiddleware)
	app.Use(h.bootSeedMiddleware)
	app.Use(h.metricsMiddleware)

	app.Post("/v1/chat/completions", h.ChatCompletions)
	app.Get("/v1/models", h.Models)
	app.Get("/v1/tools", h.Tools)
	app.Get("/v1/tools/health", h.ToolsHealth)
	app.Get("/health", h.Health)

	app.Get("/v1/usage", h.UsageSummary)
	app.Get("/v1/billing", h.BillingStatus)
	app.Get("/admin/billing/plan", h.GetBillingPlan)
	app.Put("/admin/billing/plan", h.UpdateBillingPlan)
	app.Get("/admin/billing/model-prices", h.ListModelPrices)
	app.Put("/admin/billing/model-prices", h.UpsertModelPrice)
	app.Delete("/admin/billing/model-prices/:model", h.DeleteModelPrice)

	app.Post("/v1/sessions", h.CreateSession)
	app.Get("/v1/sessions/:id", h.GetSession)
	app.Delete("/v1/sessions/:id", h.DeleteSession)
	app.Post("/v1/sessions/:id/messages", h.SessionMessage)
	app.Post("/v1/sessions/:id/cancel", h.CancelSession)
	h.registerIndexRoutes(app)
	h.registerEmbeddingMapRoutes(app)
	h.registerRepositoryRoutes(app)
	h.registerInitiativeRoutes(app)
	h.registerDeployRoutes(app)
	h.registerRepoDocsRoutes(app)
	h.registerProdOpsRoutes(app)
	h.registerRepositoryOpsRoutes(app)
	h.registerStoreOpsRoutes(app)
	h.registerGCloudOpsRoutes(app)
	h.registerWorkspaceRoutes(app)
	h.registerEvolutionRoutes(app)

	h.registerSettingsRoutes(app)
	h.registerHostingRoutes(app)
	h.registerRepoDependencyRoutes(app)
	h.registerVercelOpsRoutes(app)
	h.registerMobileDeviceRoutes(app)
	h.registerLLMProviderRoutes(app)
	h.registerAgentCLIRoutes(app)
	h.registerLLMEndpointRoutes(app)

	app.Post("/v1/jobs", h.CreateJob)
	app.Get("/v1/jobs/:id", h.GetJob)
	app.Get("/v1/jobs/:id/result", h.GetJobResult)
	app.Delete("/v1/jobs/:id", h.DeleteJob)

	app.Get("/v1/audit", h.AuditLog)

	app.Post("/v1/files", h.UploadFile)
	app.Get("/v1/files", h.ListFiles)
	app.Delete("/v1/files/:id", h.DeleteFile)

	h.registerAttachmentRoutes(app)

	app.Post("/admin/reload", h.AdminReload)

	app.Get("/docs", h.OpenAPIDocs)
	app.Get("/docs/*", h.OpenAPIDocs)

	metricsHandler := fasthttpadaptor.NewFastHTTPHandler(promhttp.Handler())
	app.Get("/metrics", func(c *fiber.Ctx) error {
		metricsHandler(c.Context())
		return nil
	})

	h.registerOrchestrationRoutes(app)
	h.registerMCPRoutes(app)

	// The per-run MCP tool endpoint a Claude Code session calls back on —
	// a child process on this host, or a session on a member's Mac reaching in
	// through the control plane at /api/mcp.
	//
	// Mounted WITHOUT either auth middleware, and it stays that way on purpose:
	// the caller holds none of this server's credentials — not the API
	// key, not INTERNAL_AUTH_KEY, not a Firebase token — which for a local
	// session is the whole point of the environment scrub (platform/childenv,
	// claudecode.claudeEnvPassthrough) and for a remote one is simply true of
	// somebody's laptop. It presents the per-run bearer token instead, which
	// the endpoint verifies for itself, which names the run this process
	// bound it to, and which is worthless the moment the run ends. isPublicPath
	// already lets /mcp past both middlewares because it is neither /v1 nor
	// /admin; that is spelled out there rather than left to the default.
	//
	// Registered before the UI routes so the SPA's catch-all cannot answer a
	// JSON-RPC request with index.html.
	if h.mcpToolServer != nil {
		h.mcpToolServer.Register(app)
	}

	h.registerUIRoutes(app)
}

func (h *Handler) requestIDMiddleware(c *fiber.Ctx) error {
	requestID := c.Get("X-Request-ID")
	if requestID == "" {
		requestID = uuid.New().String()
	}
	c.Set("X-Request-ID", requestID)
	c.Locals("request_id", requestID)
	return c.Next()
}

func (h *Handler) authMiddleware(c *fiber.Ctx) error {
	if h.isPublicPath(c.Path()) {
		return c.Next()
	}

	if len(h.apiKeys) == 0 && h.legacyAPIKey == "" && h.apiKeySvc == nil {
		return c.Next()
	}

	auth := c.Get("Authorization")
	if auth == "" {
		return unauthorized(c)
	}

	const prefix = "Bearer "
	if len(auth) <= len(prefix) || auth[:len(prefix)] != prefix {
		return unauthorized(c)
	}
	token := auth[len(prefix):]

	if h.authenticateToken(c, token) {
		return c.Next()
	}

	return unauthorized(c)
}

func unauthorized(c *fiber.Ctx) error {
	return c.Status(fiber.StatusUnauthorized).JSON(errorResponse{
		Error: errorDetail{Message: "invalid or missing api key", Type: "authentication_error"},
	})
}

func (h *Handler) metricsMiddleware(c *fiber.Ctx) error {
	start := time.Now()
	err := c.Next()
	status := strconv.Itoa(c.Response().StatusCode())
	h.metrics.RequestsTotal.WithLabelValues(c.Method(), c.Path(), status).Inc()
	h.metrics.RequestDuration.WithLabelValues(c.Method(), c.Path()).Observe(time.Since(start).Seconds())
	return err
}

// bootSeedMiddleware retries a board seed that failed at boot; once the seed has
// succeeded it costs a couple of atomic reads. Public paths are skipped: their
// callers (GitHub, a Claude Code session) hold no API key and must not be able
// to trigger a seed.
func (h *Handler) bootSeedMiddleware(c *fiber.Ctx) error {
	if h.bootSeed != nil && !h.isPublicPath(c.Path()) {
		if err := h.bootSeed.Ensure(c.UserContext()); err != nil {
			log.Warn().Err(err).Msg("board seed failed; the next request retries it")
		}
	}
	return c.Next()
}

// BootSeed is the half of bootseed.Service this layer needs, declared here so
// the handler stays testable without a database.
type BootSeed interface {
	Ensure(ctx context.Context) error
	Booting() bool
}

func (h *Handler) enrichContext(c *fiber.Ctx) context.Context {
	ctx := c.UserContext()
	if rid, ok := c.Locals("request_id").(string); ok {
		ctx = registry.ContextWithRequestID(ctx, rid)
	}
	if name, ok := c.Locals("client_name").(string); ok {
		ctx = registry.ContextWithAPIKeyName(ctx, name)
	}
	return ctx
}

// resolvePolicy derives the tool allowlist for one request. The server-side
// policies (config default, then the API key's own policy) set the ceiling; the
// policy in the request body may only narrow it. Merging the request policy in
// used to REPLACE the allowlist, so a key restricted to read-only tools could
// grant itself run_terminal just by asking for it.
func (h *Handler) resolvePolicy(c *fiber.Ctx, reqPolicy domain.ToolPolicy) domain.ToolPolicy {
	base := h.defaultPolicy
	if clientPolicy, ok := c.Locals("client_policy").(domain.ToolPolicy); ok && !clientPolicy.IsZero() {
		base = domain.MergeToolPolicy(base, clientPolicy)
	}
	if len(reqPolicy.AllowMCPServers) > 0 {
		base = domain.IntersectMCPServers(base, reqPolicy.AllowMCPServers)
	}
	return domain.IntersectToolPolicy(base, reqPolicy.AllowTools)
}

// budgetGate refuses a request that would start an agent run once the
// USD budget for the period is spent. The board runner has enforced this since
// the budget shipped, but the documented API entry points (chat completions,
// session messages, jobs) ran the same agent loop with no check at all — an API
// key holder could loop them and bill past the plan without ever touching a
// board. Nil billing service = self-hosted/desktop, which owns its own spend:
// the gate is a no-op there.
//
// 402 rather than 429: the run is not being paced, it is refused until the
// period renews or the plan changes, and 429 is already what an upstream LLM
// provider's rate limit looks like to clients that retry on it.
func (h *Handler) budgetGate(c *fiber.Ctx) error {
	if h.billingSvc == nil {
		return nil
	}
	allowed, reason := h.billingSvc.Allow(h.enrichContext(c))
	if allowed {
		return nil
	}
	return fiber.NewError(fiber.StatusPaymentRequired, "quota exhausted: "+reason)
}

func (h *Handler) ChatCompletions(c *fiber.Ctx) error {
	var req chatCompletionRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, fmt.Sprintf("invalid request body: %v", err))
	}
	if len(req.Messages) == 0 {
		return badRequest(c, "messages array is required and must not be empty")
	}
	if err := h.budgetGate(c); err != nil {
		return err
	}

	messages := parseMessages(req.Messages)
	model := req.Model
	if model == "" {
		model = "local"
	}

	policy := h.resolvePolicy(c, req.ToolPolicy)
	ctx := h.enrichContext(c)

	if req.SessionID != "" {
		sessionID, err := uuid.Parse(req.SessionID)
		if err != nil {
			return badRequest(c, "invalid session_id")
		}
		if h.sessionSvc == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
				Error: errorDetail{Message: "sessions not enabled", Type: "service_unavailable"},
			})
		}
		history, err := h.sessionSvc.LoadHistory(ctx, sessionID)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(errorResponse{
				Error: errorDetail{Message: err.Error(), Type: "not_found"},
			})
		}
		messages = append(history, messages...)
	}

	if h.ragSvc != nil && len(req.FileIDs) > 0 {
		var err error
		messages, err = h.ragSvc.InjectContext(ctx, messages, req.FileIDs)
		if err != nil {
			return internalError(c, err)
		}
	}

	log.Info().Str("model", model).Int("messages", len(messages)).Bool("stream", req.Stream).Msg("chat completion request")

	if req.Stream {
		return h.chatCompletionsStream(c, messages, model, policy)
	}

	start := time.Now()
	resp, err := h.agentLoop.Run(ctx, messages, model, "", policy)
	h.metrics.LLMLatency.Observe(time.Since(start).Seconds())
	if err != nil {
		log.Error().Err(err).Msg("agent loop error")
		return internalError(c, err)
	}

	return c.JSON(chatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []choice{{
			Index:        0,
			Message:      responseMessage{Role: string(resp.Message.Role), Content: resp.Message.Content},
			FinishReason: "stop",
		}},
		Usage: usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	})
}

func (h *Handler) chatCompletionsStream(c *fiber.Ctx, messages []domain.Message, model string, policy domain.ToolPolicy) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("Transfer-Encoding", "chunked")

	chatID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	ctx := h.enrichContext(c)

	// The body stream writer runs after this handler returns, so the request
	// context is already cancelled by then — carry its values (request id,
	// workspace scope) without its cancellation. Passing a bare Background() here
	// also meant the loop ran unscoped, losing tool/attribution context.
	streamCtx, streamCancel := context.WithCancel(context.WithoutCancel(ctx))

	c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
		defer streamCancel()
		_, err := h.agentLoop.RunStream(streamCtx, messages, model, "", policy, func(token string) {
			chunk := streamChunkEvent{
				ID: chatID, Object: "chat.completion.chunk", Created: created, Model: model,
				Choices: []streamChoiceEvent{{Index: 0, Delta: streamDeltaEvent{Content: token}}},
			}
			data, _ := json.Marshal(chunk)
			if _, writeErr := fmt.Fprintf(w, "data: %s\n\n", data); writeErr != nil {
				// The client is gone. Without this the agent loop kept running —
				// and kept spending the user's budget — on a stream nobody reads.
				streamCancel()
				return
			}
			if flushErr := w.Flush(); flushErr != nil {
				streamCancel()
			}
		})

		if err != nil {
			log.Error().Err(err).Msg("stream agent loop error")
		} else {
			doneChunk := streamChunkEvent{
				ID: chatID, Object: "chat.completion.chunk", Created: created, Model: model,
				Choices: []streamChoiceEvent{{Index: 0, FinishReason: "stop"}},
			}
			data, _ := json.Marshal(doneChunk)
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
			w.Flush()
		}
	}))

	return nil
}

// resolveMultiClient walks the LLM client decorator chain (usage recorder,
// hot-swapper, ...) via Unwrap() to reach the underlying MultiProviderClient,
// which exposes per-provider model listing and health checks the port.LLMClient
// interface does not. Without this, those capabilities are invisible whenever
// the client is wrapped (always in cloud mode, where usage recording is on).
func resolveMultiClient(c port.LLMClient) (*llmadapter.MultiProviderClient, bool) {
	for i := 0; i < 8 && c != nil; i++ {
		if m, ok := c.(*llmadapter.MultiProviderClient); ok {
			return m, true
		}
		u, ok := c.(interface{ Unwrap() port.LLMClient })
		if !ok {
			return nil, false
		}
		c = u.Unwrap()
	}
	return nil, false
}

func (h *Handler) Models(c *fiber.Ctx) error {
	provider := c.Query("provider")
	var models []string
	var err error
	if provider != "" {
		// A host-executed provider (claude_code) is a binary on this host, not an
		// endpoint, so there is nothing to ask — and asking is exactly what used
		// to happen: it fell through to the multi client, which has no entry for
		// it, and the picker got "provider is not configured". Its models are a
		// curated constant instead, served from here so the web uses the SAME
		// endpoint it already uses for every other provider.
		if opts, ok := domain.ModelsForHostExecutedProvider(domain.LLMProviderType(provider)); ok {
			// claude_code's list is never empty (it is the curated constant); an
			// empty list here means one of the OTHER host-executed CLIs, which
			// have no curated table because they can answer for themselves — ask
			// the CLI on this host (or, on a remote deployment, get back nothing
			// and keep the curated empty list, same as before this existed).
			if len(opts) == 0 && h.agentCLISvc != nil {
				if live, handled, err := h.agentCLISvc.ModelsFor(h.enrichContext(c), domain.LLMProviderType(provider)); handled {
					if err != nil {
						log.Warn().Err(err).Str("provider", provider).Msg("live model catalog fetch failed; picker falls back to free text")
					} else {
						opts = live
					}
				}
			}
			data := make([]modelData, 0, len(opts))
			for _, opt := range opts {
				data = append(data, modelData{ID: opt.ID, Object: "model", Label: opt.Label})
			}
			return c.JSON(modelsResponse{Object: "list", Data: data})
		}
		multi, ok := resolveMultiClient(h.llmClient)
		if !ok {
			return c.Status(fiber.StatusBadRequest).JSON(errorResponse{
				Error: errorDetail{Message: "provider-specific models not supported", Type: "invalid_request"},
			})
		}
		// A provider is either a built-in type or a named-endpoint uuid, which is
		// registered in the client map under that uuid. Reject only if it is
		// neither — otherwise endpoint models could never be listed.
		if !domain.ValidLLMProviderType(provider) {
			if _, ok := multi.ClientFor(c.UserContext(), domain.LLMProviderType(provider)); !ok {
				return c.Status(fiber.StatusBadRequest).JSON(errorResponse{
					Error: errorDetail{Message: "invalid provider type", Type: "invalid_request"},
				})
			}
		}
		models, err = multi.ModelsFor(c.UserContext(), domain.LLMProviderType(provider))
	} else {
		models, err = h.llmClient.Models(c.UserContext())
	}
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(errorResponse{
			Error: errorDetail{Message: fmt.Sprintf("failed to fetch models: %v", err), Type: "upstream_error"},
		})
	}
	data := make([]modelData, 0, len(models))
	for _, m := range models {
		data = append(data, modelData{ID: m, Object: "model"})
	}
	return c.JSON(modelsResponse{Object: "list", Data: data})
}

func (h *Handler) Tools(c *fiber.Ctx) error {
	// ?all=true returns every registered tool, unfiltered by the default policy —
	// used by the admin agent tool picker so any tool can be granted (and an
	// existing grant is never silently stripped because the picker didn't know
	// the tool existed).
	if c.Query("all") == "true" {
		defs := h.registry.DefinitionsForPolicy(domain.ToolPolicy{})
		return c.JSON(toolsResponse{Tools: defs, Count: len(defs)})
	}
	policy := h.resolvePolicy(c, domain.ToolPolicy{})
	defs := h.registry.DefinitionsForPolicy(policy)
	return c.JSON(toolsResponse{Tools: defs, Count: len(defs)})
}

func (h *Handler) ToolsHealth(c *fiber.Ctx) error {
	if h.mcpManager == nil {
		return c.JSON(fiber.Map{"servers": []interface{}{}})
	}
	return c.JSON(fiber.Map{"servers": h.mcpManager.Health()})
}

func (h *Handler) Health(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.UserContext(), 5*time.Second)
	defer cancel()

	var providers []llmProviderHealthItem
	llmStatus := "ok"
	status := "ok"

	if h.llmProviderSvc != nil {
		list, err := h.llmProviderSvc.List(ctx)
		if err == nil {
			if multi, ok := resolveMultiClient(h.llmClient); ok {
				checks := append(multi.HealthCheck(ctx, list.Providers),
					multi.HealthCheckEndpoints(ctx, list.Endpoints)...)
				providers = make([]llmProviderHealthItem, 0, len(checks))
				configuredOK := 0
				configuredTotal := 0
				for _, check := range checks {
					providers = append(providers, llmProviderHealthItem{
						ProviderType: string(check.ProviderType),
						Label:        check.Label,
						Configured:   check.Configured,
						Active:       check.Active,
						Status:       check.Status,
						Message:      check.Message,
					})
					if check.Configured {
						configuredTotal++
						if check.Status == "ok" {
							configuredOK++
						}
					}
				}
				if configuredTotal == 0 {
					llmStatus = "no providers configured"
					status = "degraded"
				} else if configuredOK == 0 {
					llmStatus = "all configured providers unreachable"
					status = "degraded"
				} else if configuredOK < configuredTotal {
					llmStatus = fmt.Sprintf("%d/%d providers ok", configuredOK, configuredTotal)
					status = "degraded"
				}
			}
		}
	}

	if len(providers) == 0 {
		_, err := h.llmClient.Models(ctx)
		if err != nil {
			llmStatus = fmt.Sprintf("unreachable: %v", err)
			status = "degraded"
		}
	}

	return c.JSON(healthResponse{Status: status, LLM: llmStatus, Providers: providers})
}

func (h *Handler) CreateSession(c *fiber.Ctx) error {
	if h.sessionSvc == nil {
		return sessionsDisabled(c)
	}
	var req domain.CreateSessionRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, fmt.Sprintf("invalid request body: %v", err))
	}
	sess, err := h.sessionSvc.Create(h.enrichContext(c), req)
	if err != nil {
		return internalError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(sess)
}

func (h *Handler) GetSession(c *fiber.Ctx) error {
	if h.sessionSvc == nil {
		return sessionsDisabled(c)
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid session id")
	}
	sess, msgs, err := h.sessionSvc.Get(h.enrichContext(c), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	// Actions are best-effort: a chat must still render if the ledger read fails.
	actions, actionsErr := h.sessionSvc.ListActions(h.enrichContext(c), id)
	if actionsErr != nil {
		actions = []domain.SessionAction{}
	}
	return c.JSON(fiber.Map{"session": sess, "messages": msgs, "actions": actions})
}

func (h *Handler) DeleteSession(c *fiber.Ctx) error {
	if h.sessionSvc == nil {
		return sessionsDisabled(c)
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid session id")
	}
	if err := h.sessionSvc.Delete(h.enrichContext(c), id); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) SessionMessage(c *fiber.Ctx) error {
	if h.sessionSvc == nil {
		return sessionsDisabled(c)
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid session id")
	}
	var req domain.SessionMessageRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, fmt.Sprintf("invalid request body: %v", err))
	}
	if req.Content == "" {
		return badRequest(c, "content is required")
	}
	if req.Role == "" {
		req.Role = domain.RoleUser
	}
	if err := h.budgetGate(c); err != nil {
		return err
	}
	policy := h.resolvePolicy(c, req.ToolPolicy)
	if req.Stream {
		return h.sessionMessageStream(c, id, req, policy)
	}
	resp, err := h.sessionSvc.SendMessage(h.enrichContext(c), id, req, policy)
	if err != nil {
		// The turn was stopped from another request while this one was waiting for
		// it. Nothing failed, so this is not a 500 — and there is no reply to
		// return either, which is exactly what the status says.
		if errors.Is(err, domain.ErrRunCancelled) {
			return c.Status(fiber.StatusConflict).JSON(errorResponse{
				Error: errorDetail{Message: err.Error(), Type: "cancelled"},
			})
		}
		return internalError(c, err)
	}
	return c.JSON(resp)
}

// CancelSession — POST /v1/sessions/:id/cancel
//
// Stops the chat turn this session has in flight. `cancelled: false` means there
// was nothing to stop (the turn finished a moment before the button was pressed)
// — an ordinary answer, not an error, so the client must not treat it as one.
func (h *Handler) CancelSession(c *fiber.Ctx) error {
	if h.sessionSvc == nil {
		return sessionsDisabled(c)
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid session id")
	}
	// An empty body is the normal case (the composer's stop button sends none), so
	// a parse failure only means "no reason given".
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.BodyParser(&req)

	cancelled, err := h.sessionSvc.CancelSession(h.enrichContext(c), id, req.Reason)
	if err != nil {
		if errors.Is(err, domain.ErrSessionNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(errorResponse{
				Error: errorDetail{Message: err.Error(), Type: "not_found"},
			})
		}
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"cancelled": cancelled})
}

func (h *Handler) sessionMessageStream(c *fiber.Ctx, sessionID uuid.UUID, req domain.SessionMessageRequest, policy domain.ToolPolicy) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("Transfer-Encoding", "chunked")

	chatID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	ctx := h.enrichContext(c)

	newChunk := func(choice streamChoiceEvent) streamChunkEvent {
		return streamChunkEvent{
			ID: chatID, Object: "chat.completion.chunk", Created: created, Model: req.Model,
			Choices: []streamChoiceEvent{choice},
		}
	}
	// One `data:` frame, flushed. A write or flush failure here means the client
	// is gone — every caller has to act on that, not ignore it.
	writeFrame := func(w *bufio.Writer, chunk streamChunkEvent) error {
		data, _ := json.Marshal(chunk)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return err
		}
		return w.Flush()
	}

	// The body stream writer runs after this handler returns, so the request
	// context is already cancelled by then — carry its values (request id,
	// workspace scope) without its cancellation. Passing a bare Background() here
	// also meant the loop ran unscoped, losing tool/attribution context.
	streamCtx, streamCancel := context.WithCancel(context.WithoutCancel(ctx))

	// The multi-stage orchestration pipeline cannot stream, and session.Service
	// only says so *after* it has persisted the user's message — so the refusal
	// could not be recovered from either here (a retry without `stream` would
	// duplicate the message) or in the client, and every orchestrated send that
	// asked for a stream failed outright. Run the non-streaming path for those
	// and deliver its reply over the same frames: the caller asked for
	// text/event-stream and gets it, the answer simply arrives in one piece.
	// Live progress for those runs is what the activity/steps endpoints are for.
	orchestrated := req.Orchestrate != nil && *req.Orchestrate

	c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
		defer streamCancel()

		var err error
		if orchestrated {
			var resp domain.AgentResponse
			resp, err = h.sessionSvc.SendMessage(streamCtx, sessionID, req, policy)
			if err == nil && resp.Message.Content != "" {
				if writeErr := writeFrame(w, newChunk(streamChoiceEvent{
					Index: 0, Delta: streamDeltaEvent{Content: resp.Message.Content},
				})); writeErr != nil {
					return
				}
			}
		} else {
			// The loop calls this whenever the text it has just streamed turned out
			// to be reasoning ahead of a tool call. Forwarding it as its own frame
			// is what lets the client keep that stretch apart — without it the
			// reasoning and the answer arrive as one undivided body of text, so the
			// client can only show them as one message and then drop the first half
			// when the real reply is committed.
			segmented := agent.WithSegmentBreak(streamCtx, func() {
				if writeErr := writeFrame(w, newChunk(streamChoiceEvent{
					Index: 0, Delta: streamDeltaEvent{Phase: "reasoning_end"},
				})); writeErr != nil {
					streamCancel()
				}
			})
			_, err = h.sessionSvc.SendMessageStream(segmented, sessionID, req, policy, func(token string) {
				if writeErr := writeFrame(w, newChunk(streamChoiceEvent{
					Index: 0, Delta: streamDeltaEvent{Content: token},
				})); writeErr != nil {
					// The client is gone. Without this the agent loop kept running —
					// and kept spending the user's budget — on a stream nobody reads.
					streamCancel()
				}
			})
		}

		// A turn the user stopped is not a failure, so it must not go out as an
		// error frame: the service has already persisted whatever the agent had
		// said, and the stream ends the way a finished one ends. That leaves the
		// partial answer on screen (matching what was persisted) instead of the
		// client replacing it with an error, and every decoder — web, iOS — already
		// handles "stop" + [DONE].
		if errors.Is(err, domain.ErrRunCancelled) {
			log.Info().Str("session_id", sessionID.String()).Msg("session message stream cancelled by user")
			err = nil
		}

		if err != nil {
			log.Error().Err(err).Msg("session message stream error")
			// A provider rate limit is not a crash — it is a condition of the
			// user's own account, with an action attached — so it goes out under
			// its own type and its own sentence. Clients render that as a warning;
			// everything else keeps the generic error path unchanged.
			message, errType := err.Error(), "agent_error"
			if rl, ok := domain.RateLimitOf(err); ok {
				message, errType = rl.UserMessage(), "rate_limited"
			}
			// A spent Claude Code subscription rides the SAME frame type as a
			// provider rate limit. It is the same class of thing — the account
			// has nothing left to spend until a known time — and reusing the
			// type means every client that already renders a rate limit as a
			// calm, actionable warning renders this one that way too, with no
			// change on their side. A new type would have been shown as an
			// unrecognised error by every one of them.
			//
			// err.Error() is already the localised sentence naming the reset
			// time: the session service built it while it still had the
			// settings loaded, so nothing here has to read the database on an
			// error path to find out what language to say it in.
			if _, ok := domain.QuotaBlockOf(err); ok {
				message, errType = err.Error(), "rate_limited"
			}
			// The message is repeated in the delta for clients that predate the
			// `error` field (iOS decodes a fixed struct and drops unknown keys);
			// clients that read it show a real error instead of pasting the text
			// into the transcript as the agent's own reply.
			errChunk := newChunk(streamChoiceEvent{
				Index: 0, Delta: streamDeltaEvent{Content: message},
			})
			errChunk.Error = &errorDetail{Message: message, Type: errType}
			if writeErr := writeFrame(w, errChunk); writeErr != nil {
				return
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			w.Flush()
			return
		}

		if writeErr := writeFrame(w, newChunk(streamChoiceEvent{Index: 0, FinishReason: "stop"})); writeErr != nil {
			return
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		w.Flush()
	}))

	return nil
}

func (h *Handler) CreateJob(c *fiber.Ctx) error {
	if h.jobSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "jobs not enabled", Type: "service_unavailable"},
		})
	}
	var req domain.JobRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, fmt.Sprintf("invalid request body: %v", err))
	}
	if len(req.Messages) == 0 {
		return badRequest(c, "messages array is required")
	}
	if err := h.budgetGate(c); err != nil {
		return err
	}
	policy := h.resolvePolicy(c, req.ToolPolicy)
	job, err := h.jobSvc.Create(h.enrichContext(c), req, policy)
	if err != nil {
		return internalError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(job)
}

func (h *Handler) GetJob(c *fiber.Ctx) error {
	if h.jobSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "jobs not enabled", Type: "service_unavailable"},
		})
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid job id")
	}
	job, err := h.jobSvc.Get(h.enrichContext(c), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	return c.JSON(job)
}

func (h *Handler) GetJobResult(c *fiber.Ctx) error {
	if h.jobSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "jobs not enabled", Type: "service_unavailable"},
		})
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid job id")
	}
	job, err := h.jobSvc.Get(h.enrichContext(c), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	if job.Status != domain.JobStatusCompleted {
		return c.Status(fiber.StatusConflict).JSON(errorResponse{
			Error: errorDetail{Message: fmt.Sprintf("job status is %s", job.Status), Type: "conflict"},
		})
	}
	return c.JSON(goccyjson.RawMessage(job.Result))
}

func (h *Handler) DeleteJob(c *fiber.Ctx) error {
	if h.jobSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "jobs not enabled", Type: "service_unavailable"},
		})
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid job id")
	}
	if err := h.jobSvc.Delete(h.enrichContext(c), id); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) AuditLog(c *fiber.Ctx) error {
	if h.auditStore == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "audit not enabled", Type: "service_unavailable"},
		})
	}
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	entries, err := h.auditStore.List(h.enrichContext(c), limit)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"entries": entries, "count": len(entries)})
}

func (h *Handler) UploadFile(c *fiber.Ctx) error {
	if h.ragSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "file upload not enabled", Type: "service_unavailable"},
		})
	}
	file, err := c.FormFile("file")
	if err != nil {
		return badRequest(c, "file field is required")
	}
	f, err := file.Open()
	if err != nil {
		return internalError(c, err)
	}
	defer f.Close()

	contentType := file.Header.Get("Content-Type")
	rec, err := h.ragSvc.Upload(h.enrichContext(c), file.Filename, contentType, f)
	if err != nil {
		return internalError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(rec)
}

func (h *Handler) ListFiles(c *fiber.Ctx) error {
	if h.ragSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "file upload not enabled", Type: "service_unavailable"},
		})
	}
	files, err := h.ragSvc.List(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"files": files, "count": len(files)})
}

func (h *Handler) DeleteFile(c *fiber.Ctx) error {
	if h.ragSvc == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "file upload not enabled", Type: "service_unavailable"},
		})
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return badRequest(c, "invalid file id")
	}
	if err := h.ragSvc.Delete(h.enrichContext(c), id); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(errorResponse{
			Error: errorDetail{Message: err.Error(), Type: "not_found"},
		})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) AdminReload(c *fiber.Ctx) error {
	if h.reloadFn == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
			Error: errorDetail{Message: "reload not configured", Type: "service_unavailable"},
		})
	}
	if err := h.reloadFn(); err != nil {
		return internalError(c, err)
	}
	return c.JSON(fiber.Map{"status": "reloaded"})
}

func (h *Handler) OpenAPIDocs(c *fiber.Ctx) error {
	// The file on disk wins so an edit shows up without a rebuild; the embedded
	// copy is what the packaged app has, where there is no resources directory.
	data, err := os.ReadFile("resources/openapi.yaml")
	if err != nil {
		data = resources.OpenAPIYAML
	}
	if len(data) == 0 {
		return c.Status(fiber.StatusNotFound).SendString("openapi spec not found")
	}
	c.Set("Content-Type", "text/yaml")
	return c.Send(data)
}

func parseMessages(msgs []requestMessage) []domain.Message {
	messages := make([]domain.Message, 0, len(msgs))
	for _, m := range msgs {
		dm := domain.Message{
			Role:       domain.Role(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
		for _, tc := range m.ToolCalls {
			dm.ToolCalls = append(dm.ToolCalls, domain.ToolCall{
				ID: tc.ID, Type: tc.Type,
				Function: domain.FunctionCall{Name: tc.Function.Name, Arguments: tc.Function.Arguments},
			})
		}
		messages = append(messages, dm)
	}
	return messages
}

func badRequest(c *fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusBadRequest).JSON(errorResponse{
		Error: errorDetail{Message: msg, Type: "invalid_request_error"},
	})
}

// badRequestErr is badRequest for a failure that arrived as an ERROR rather
// than as a validation message this handler wrote itself.
//
// The difference is not cosmetic. A message this handler composed can only ever
// be a bad request; an error from a service below it can be a bad request OR
// one of the states that has its own status. So a handler that has an error in
// its hand should reach for this one.
func badRequestErr(c *fiber.Ctx, err error) error {
	if handled, writeErr := permanentRefusal(c, err); handled {
		return writeErr
	}
	if handled, writeErr := typedBadRequest(c, err); handled {
		return writeErr
	}
	if handled, writeErr := criteriaGateBadRequest(c, err); handled {
		return writeErr
	}
	return badRequest(c, err.Error())
}

// internalError is where a handler sends anything it could not classify, which
// makes it the one place that sees every failure this server produces — and
// therefore the right place to catch the one failure that is not a failure.
//
// A permanent refusal that reached here would tell the client to retry a
// request that cannot ever succeed. Checked here rather than at ~90 call
// sites: the condition is raised deep and every route that lets it out needs
// the same answer. See permanent_refusal.go.
func internalError(c *fiber.Ctx, err error) error {
	if handled, writeErr := permanentRefusal(c, err); handled {
		return writeErr
	}
	return c.Status(fiber.StatusInternalServerError).JSON(errorResponse{
		Error: errorDetail{Message: err.Error(), Type: "internal_error"},
	})
}

func sessionsDisabled(c *fiber.Ctx) error {
	return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
		Error: errorDetail{Message: "sessions not enabled", Type: "service_unavailable"},
	})
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
