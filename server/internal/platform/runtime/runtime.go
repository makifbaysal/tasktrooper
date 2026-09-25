package runtime

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	goccyjson "github.com/goccy/go-json"
	"github.com/gofiber/fiber/v2"
	corsmw "github.com/gofiber/fiber/v2/middleware/cors"
	recovermw "github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/catalogrepo"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/antigravity"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/claudecode"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/cursor"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/opencode"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cloud/appstore"
	awsprovider "github.com/makifbaysal/tasktrooper/server/internal/adapter/cloud/aws"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cloud/deviceagent"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cloud/gcloud"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cloud/googleplay"
	vercelapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/cloud/vercel"
	httpadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/http"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/llm"
	desktopadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/local/desktop"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/local/localdevice"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/local/localtoolchain"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/localexec"
	mcpadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/mcp"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/mcpserver"
	pgstore "github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/board"
	boilerplatetools "github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/boilerplate"
	browsertools "github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/browser"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/clarification"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/code"
	memorytools "github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/memory"
	mobiletools "github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/mobile"
	opstools "github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/ops"
	projectmodeltools "github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/projectmodel"
	runtimetools "github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/runtime"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/search"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/shell"
	skilltools "github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/skill"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/web"
	gitadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/git"
	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agentcli"
	"github.com/makifbaysal/tasktrooper/server/internal/application/apikey"
	attachmentapp "github.com/makifbaysal/tasktrooper/server/internal/application/attachment"
	"github.com/makifbaysal/tasktrooper/server/internal/application/billing"
	boardapp "github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/bootseed"
	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/application/chunker"
	cloudapp "github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	appconfig "github.com/makifbaysal/tasktrooper/server/internal/application/config"
	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/application/deploy"
	"github.com/makifbaysal/tasktrooper/server/internal/application/deployops"
	"github.com/makifbaysal/tasktrooper/server/internal/application/deploywatch"
	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery"
	"github.com/makifbaysal/tasktrooper/server/internal/application/embedmap"
	"github.com/makifbaysal/tasktrooper/server/internal/application/evolution"
	"github.com/makifbaysal/tasktrooper/server/internal/application/indexer"
	"github.com/makifbaysal/tasktrooper/server/internal/application/initiative"
	"github.com/makifbaysal/tasktrooper/server/internal/application/job"
	kpiapp "github.com/makifbaysal/tasktrooper/server/internal/application/kpi"
	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	localpreviewapp "github.com/makifbaysal/tasktrooper/server/internal/application/localpreview"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	mcpsvc "github.com/makifbaysal/tasktrooper/server/internal/application/mcp"
	memoryapp "github.com/makifbaysal/tasktrooper/server/internal/application/memory"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mobiledevice"
	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/application/projectmodel"
	"github.com/makifbaysal/tasktrooper/server/internal/application/rag"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	releaseapp "github.com/makifbaysal/tasktrooper/server/internal/application/release"
	"github.com/makifbaysal/tasktrooper/server/internal/application/repodocs"
	"github.com/makifbaysal/tasktrooper/server/internal/application/repository"
	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/application/settings"
	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops/pipeline"
	usageapp "github.com/makifbaysal/tasktrooper/server/internal/application/usage"
	workflowapp "github.com/makifbaysal/tasktrooper/server/internal/application/workflow"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// workflowDir is where the mobile release workflow is rendered; GitHub reads
// workflows nowhere else. The file's NAME carries the sub-project, so the two
// apps in one monorepo do not overwrite each other — the probes and the
// dispatch must agree on one exact file.
const workflowDir = ".github/workflows/"

// workspaceLister adapts the project + repository stores to the board toolkit's
// WorkspaceLister so agents can list projects and code repositories.
// releaseAttributor asks the release ledger first — it covers every delivery
// mode, push-to-deploy included — and falls back to the deploy-run ledger for
// releases that predate it.
type releaseAttributor struct {
	primary, fallback prodops.ReleaseAttributor
}

func (a releaseAttributor) AttributeRelease(ctx context.Context, repositoryID uuid.UUID, env string, onset time.Time) (domain.ReleaseAttribution, bool) {
	if att, ok := a.primary.AttributeRelease(ctx, repositoryID, env, onset); ok {
		return att, true
	}
	return a.fallback.AttributeRelease(ctx, repositoryID, env, onset)
}

func (a releaseAttributor) HealthWindow() time.Duration { return a.primary.HealthWindow() }

// componentPathReader scopes a monorepo task's pipeline mappings to its own
// component.
type componentPathReader struct{ model *projectmodel.Service }

func (r componentPathReader) ComponentPath(ctx context.Context, _, componentID uuid.UUID) (string, error) {
	comp, err := r.model.GetComponent(ctx, componentID)
	if err != nil {
		return "", err
	}
	return comp.Path, nil
}

type workspaceLister struct {
	projects port.InitiativeProjectStore
	repos    port.RepositoryStore
}

func (w workspaceLister) ListProjects(ctx context.Context) ([]domain.InitiativeProject, error) {
	return w.projects.List(ctx)
}

func (w workspaceLister) ListRepositories(ctx context.Context) ([]domain.Repository, error) {
	repos, err := w.repos.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(repos))
	for i := range repos {
		ids[i] = repos[i].ID
	}
	if links, err := w.repos.ListProjectIDsByRepositories(ctx, ids); err == nil {
		for i := range repos {
			repos[i].ProjectIDs = links[repos[i].ID]
		}
	}
	return repos, nil
}

func (w workspaceLister) CreateProject(ctx context.Context, name, description string) (domain.InitiativeProject, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.InitiativeProject{}, fmt.Errorf("name is required")
	}
	return w.projects.Create(ctx, name, strings.TrimSpace(description))
}

func (w workspaceLister) UpdateProject(ctx context.Context, id uuid.UUID, name, description string) (domain.InitiativeProject, error) {
	current, err := w.projects.Get(ctx, id)
	if err != nil {
		return domain.InitiativeProject{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = current.Name
	}
	description = strings.TrimSpace(description)
	if description == "" {
		description = current.Description
	}
	return w.projects.Update(ctx, id, name, description)
}

func (w workspaceLister) SetRepositoryProjects(ctx context.Context, repositoryID uuid.UUID, projectIDs []uuid.UUID) (domain.Repository, error) {
	if err := w.repos.SetProjects(ctx, repositoryID, projectIDs); err != nil {
		return domain.Repository{}, err
	}
	repo, err := w.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.Repository{}, err
	}
	repo.ProjectIDs = projectIDs
	return repo, nil
}

type Options struct {
	ConfigPath  string
	DataDir     string
	Port        int
	Debug       bool
	PostgresDSN string
	UIRoot      string
	UIFS        fs.FS
	LazyMCP     bool
	// APIKey is the bearer token every request must carry, kept out of
	// config.yml so it can be scrubbed after boot and re-applied on reload.
	APIKey string
	// CORSOrigins are the origins allowed to call this server: the UI is
	// cross-origin from it in both the desktop bundle (app://tasktrooper) and
	// a checkout (the Vite dev server).
	CORSOrigins []string
	// EmbeddingsBaseURL, set, bootstraps an embedding provider at boot so RAG
	// works without anyone opening the settings page.
	EmbeddingsBaseURL string
	// PublicBaseURL is the origin a Claude Code session calls the server's own
	// tools back on. Empty until the listener binds (PORT=0), so Run fills it
	// in and reloads re-apply the value instead of the config's empty one.
	PublicBaseURL string
	// AllowedRoots are extra roots a repository or session may be pointed at.
	// Desktop/local defaults to "*" so any local directory can be opened.
	AllowedRoots []string
}

type Server struct {
	app      *fiber.App
	cancel   context.CancelFunc
	addr     string
	engine   *engine
	listener net.Listener
}

type engine struct {
	mu         sync.RWMutex
	cfg        *domain.Config
	configPath string
	// opts is what the process was started with, kept so a reload re-applies
	// the same overrides and validation boot did.
	opts            Options
	reg             port.ToolRegistry
	mcpManager      *mcpadapter.Manager
	browserSession  *browsertools.Session
	mobilePool      *mobiletools.Pool
	mobileDeviceSvc *mobiledevice.Service
	llmClient       port.LLMClient
	multiLLM        *llm.MultiProviderClient
	agentLoop       *agent.Loop
	// agentRouter is what every agentic consumer is handed instead of
	// agentLoop: the loop plus one decision — HTTP loop or host executor — made
	// from the provider the caller already passes (see agent.Router). agentLoop
	// stays beside it because the loop's own setters are the loop's, not the
	// router's.
	agentRouter     *agent.Router
	jobSvc          *job.Service
	boardRunner     *boardapp.Runner
	billingSvc      *billing.Service
	pipelineRunner  *boardapp.PipelineRunner
	deploySvc       *deploy.Service
	repoDocsSvc     *repodocs.Service
	projectModelSvc *projectmodel.Service
	cloudSvc        *cloudapp.Service
	prodOpsSvc      *prodops.Service
	healthMonitor   *prodops.Monitor
	storeOpsSvc     *storeops.Service
	storeMonitor    *storeops.Monitor
	deployOpsSvc    *deployops.Service
	deployWatchSvc  *deploywatch.Service
	releaseSvc      *releaseapp.Service
	deployMonitor   *deployops.Monitor
	evolutionSvc    *evolution.Service
	// localRunner is release.Deps.LocalRunner's concrete type, kept here only
	// so Shutdown can Close it — a batch release's local build/publish command
	// runs detached from any request and must be killed (and its worktree
	// removed) on shutdown rather than left to outlive the process.
	localRunner *localexec.Runner
	// pgPool is kept beside pgDB for the two jobs that are not row data: pool
	// close and the pgvector bootstrap. Everything else reaches Postgres through
	// pgDB.
	pgPool *pgxpool.Pool
	pgDB   *pgstore.DB
	// bootSeed seeds the default board once and runs the boot steps.
	bootSeed   *bootseed.Service
	pendingMCP []domain.MCPServerConfig
	// mcpServer/mcpEndpoint are the two halves of the per-run tool endpoint the
	// CLI calls back on. mcpServer is nil unless the executor registered;
	// mcpEndpoint is published by Run BEFORE buildHandler because buildHandler
	// starts the board workers, which must never see an unpublished address.
	mcpServer   *mcpserver.Server
	mcpEndpoint *mcpEndpoint
	// secretsCipher derives from MCP_SECRETS_KEY (or SERVER_API_KEY) exactly
	// once at boot, because the environment stops carrying the key immediately
	// after — see scrubProcessSecrets. engine.reload, which runs long after
	// from a request goroutine, must not re-derive.
	secretsCipher    *secrets.Cipher
	secretsCipherErr error
}

// initSecretsCipher derives the at-rest encryption key while the environment
// still holds it. The error is kept: a missing key degrades only the
// credential-vault paths, it is not a boot failure.
func (e *engine) initSecretsCipher() {
	e.secretsCipher, e.secretsCipherErr = secrets.NewCipherFromEnv()
}

func ConfigureLogger(debug bool) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
	// stderr, not stdout: stdout carries exactly one machine-read line (the
	// LISTENING address the desktop supervisor parses).
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
}

// flattenLegacyWorkspaces moves a per-tenant layout install to the flat one
// (workspace.FlattenLegacyLayout). It opens a short-lived pool because the
// long-lived one appears inside buildHandler. Nothing is fatal: un-moved paths
// re-anchor on read and the next boot retries.
func flattenLegacyWorkspaces(ctx context.Context, cfg *domain.Config) {
	root := cfg.Storage.Sessions.WorkspaceRoot
	if strings.TrimSpace(root) == "" {
		return
	}
	var paths workspace.StoredPathRewriter
	if dsn := cfg.Storage.Postgres.DSN; dsn != "" {
		pool, err := pgstore.NewPool(ctx, dsn, 2)
		if err != nil {
			log.Warn().Err(err).Msg("workspace layout: database unavailable, stored paths were not re-pointed this boot")
		} else {
			defer pool.Close()
			paths = pgstore.NewWorkspacePathStore(pgstore.NewDB(pool))
		}
	}
	if _, err := workspace.FlattenLegacyLayout(ctx, root, paths); err != nil {
		log.Warn().Err(err).Msg("workspace layout: stored paths were not re-pointed; they are re-anchored on read and retried next boot")
	}
}

// agentRuntimes keeps a nil catalog service from becoming a non-nil interface
// that panics on the first connect.
func agentRuntimes(svc *catalog.Service) agentcli.AgentRuntimeReconciler {
	if svc == nil {
		return nil
	}
	return svc
}

// DefaultCORSOrigins is what the desktop shell and a `make dev` checkout call
// this server from.
var DefaultCORSOrigins = []string{"app://tasktrooper", "http://localhost:3200", "http://127.0.0.1:3200"}

func corsOrigins(opts Options) []string {
	if len(opts.CORSOrigins) > 0 {
		return opts.CORSOrigins
	}
	return DefaultCORSOrigins
}

// configuredPort is the port the HTTP listener asks for: the Options override
// when there is one, the config's otherwise. 0 means "let the kernel pick" —
// what desktop installs and tests get, and why the MCP URL needs the listener.
func configuredPort(cfg *domain.Config, opts Options) int {
	if opts.Port > 0 {
		return opts.Port
	}
	return cfg.Server.Port
}

func Run(ctx context.Context, opts Options) (*Server, error) {
	ConfigureLogger(opts.Debug)

	e := &engine{configPath: opts.ConfigPath}
	if err := e.load(opts); err != nil {
		return nil, err
	}

	// Everything that needs a pod credential from the environment must read it
	// before this line. load has the DSN and API key in cfg; initSecretsCipher
	// takes MCP_SECRETS_KEY, otherwise re-read on every reload. buildHandler
	// starts the job worker and board dispatcher, so an agent-run child can
	// exist right after — it must never exist while the secrets are reachable.
	e.initSecretsCipher()
	scrubProcessSecrets()
	if err := denyProcEnvironReads(); err != nil {
		// Not fatal: the scrubs above still hold; a /proc reader only sees what
		// execve put there, the state every previous build shipped in.
		log.Warn().Err(err).Msg("could not make the process undumpable; /proc/<pid>/environ stays readable to same-uid processes")
	}

	runCtx, cancel := context.WithCancel(ctx)

	// The listener is opened BEFORE the handler is built, and that order is
	// load-bearing: buildHandler ends by activating the board, so a claude_code
	// run can be executing before it returns — a run that needs the MCP tool
	// endpoint's address. Publishing from the CONFIGURED port was wrong because
	// the host that can run a claude_code agent at all (a desktop/runner
	// install, applyDesktopOverrides sets Server.Port to 0; the cloud image
	// ships no `claude`) is exactly where the configured port is 0. Binding
	// first publishes the kernel-chosen port before anything can dispatch.
	//
	// Nothing serves on it yet — app.Listener starts accepting further down —
	// so a request that arrives in between waits in the accept backlog.
	portNum := configuredPort(e.cfg, opts)
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", portNum))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("listen: %w", err)
	}
	addr := listener.Addr().String()
	e.mcpEndpoint = &mcpEndpoint{}
	e.mcpEndpoint.publish(addr)

	// The one line on stdout, which the desktop supervisor parses for the port
	// (PORT=0 is how it avoids colliding with the user). Printed before the
	// routes exist so the supervisor can poll /health immediately.
	fmt.Fprintf(os.Stdout, "LISTENING http://%s\n", addr)
	_ = os.Stdout.Sync()

	if e.cfg.Server.PublicBaseURL == "" {
		e.cfg.Server.PublicBaseURL = "http://" + addr
		e.opts.PublicBaseURL = e.cfg.Server.PublicBaseURL
	}

	// Before buildHandler: everything it builds reads workspace paths, and all
	// of it must see the flat layout.
	flattenLegacyWorkspaces(runCtx, e.cfg)

	handler := e.buildHandler(runCtx, opts)
	// Seed now rather than on the first request, so the board exists when the
	// desktop's first /health answers.
	if e.bootSeed != nil {
		if err := e.bootSeed.Ensure(context.Background()); err != nil {
			log.Warn().Err(err).Msg("seeding the board at boot failed; the first request retries it")
		}
	}

	log.Info().Str("addr", addr).Msg("listening")
	if e.mcpServer != nil {
		log.Info().Str("mcp_endpoint", e.mcpEndpoint.get()).Msg("tasktrooper tools reachable over mcp for agent cli sessions")
	}

	llmTimeout := e.cfg.LLM.Timeout
	if llmTimeout <= 0 {
		llmTimeout = 300 * time.Second
	}

	sessionTimeout := llmTimeout*12 + 60*time.Second
	if sessionTimeout < 10*time.Minute {
		sessionTimeout = 10 * time.Minute
	}

	app := fiber.New(fiber.Config{
		// Off, the router lowercases a path while c.Path() returns the raw one,
		// so `/V1/settings` reached the settings handler while isPublicPath
		// matched neither rule — an unauthenticated request onto a configuration
		// endpoint. On, the router refuses the spelling outright.
		CaseSensitive: true,
		// Fiber's banner would land beside the LISTENING line the desktop parses.
		DisableStartupMessage: true,
		JSONEncoder:           goccyjson.Marshal,
		JSONDecoder:           goccyjson.Unmarshal,
		ReadTimeout:           sessionTimeout,
		WriteTimeout:          sessionTimeout,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			// Honour the status a handler asked for rather than collapsing every
			// fiber.NewError and 404 to 500, which the web client retries as if
			// the workspace were waking.
			code := fiber.StatusInternalServerError
			errType := "internal_error"
			var fiberErr *fiber.Error
			if errors.As(err, &fiberErr) {
				code = fiberErr.Code
				errType = "request_error"
			}
			if code >= fiber.StatusInternalServerError {
				log.Error().Err(err).Str("path", c.Path()).Int("status", code).Msg("unhandled error")
			}
			return c.Status(code).JSON(fiber.Map{
				"error": fiber.Map{"message": err.Error(), "type": errType},
			})
		},
	})

	// A handler panic would take the whole process down, killing every in-flight
	// SSE stream and board run.
	app.Use(recovermw.New(recovermw.Config{EnableStackTrace: true}))

	// The UI is never same-origin (app://tasktrooper in the bundle, the Vite dev
	// server in a checkout). AllowCredentials stays off — the token is in a
	// header, not a cookie — which is what lets the origin list be trusted.
	app.Use(corsmw.New(corsmw.Config{
		AllowOrigins: strings.Join(corsOrigins(opts), ","),
		AllowHeaders: "Authorization,Content-Type,X-Request-ID",
		AllowMethods: "GET,POST,PUT,PATCH,DELETE,OPTIONS,HEAD",
	}))

	handler.RegisterRoutes(app)

	go func() {
		if err := app.Listener(listener); err != nil {
			log.Error().Err(err).Msg("server error")
		}
	}()

	if len(e.pendingMCP) > 0 {
		go e.loadMCPAsync(e.pendingMCP)
	}

	return &Server{
		app:      app,
		cancel:   cancel,
		addr:     addr,
		engine:   e,
		listener: listener,
	}, nil
}

func (s *Server) Addr() string {
	return s.addr
}

func (s *Server) URL() string {
	return "http://" + s.addr
}

// bootConvergeTimeout bounds the whole boot convergence pass, a bound not a
// budget: a pass that runs out is retried on the next start, nothing waits.
const bootConvergeTimeout = 15 * time.Minute

// localPushPollInterval is the fallback push check for instances GitHub
// cannot deliver webhooks to.
const localPushPollInterval = 5 * time.Minute

// httpDrainTimeout bounds step 1 of Shutdown; the drain budget belongs to
// the agent runs in step 2.
const httpDrainTimeout = 15 * time.Second

// Shutdown drains rather than severs. Order matters: a SIGTERM very likely
// lands on a workspace mid-run. Cancelling the run context and closing the
// DB pool before draining failed every in-flight request and abandoned the
// runner's queue outright.
func (s *Server) Shutdown(ctx context.Context) error {
	var err error
	// 1. Drain HTTP first: in-flight requests still have their context and
	//    the DB, and an open SSE stream would hold the whole budget.
	if s.app != nil {
		httpCtx, cancelHTTP := context.WithTimeout(ctx, httpDrainTimeout)
		err = s.app.ShutdownWithContext(httpCtx)
		cancelHTTP()
	}
	// 2. Let workers finish their job. Dropping the board runner mid-run
	//    leaves task_agent_runs rows stuck 'running' until the reconciler's
	//    stale sweep; drain waits for the agent runs, cancel only when the
	//    pod's grace period runs out.
	if s.engine.boardRunner != nil {
		s.engine.boardRunner.Drain(ctx)
	}
	if s.engine.jobSvc != nil {
		s.engine.jobSvc.Stop()
	}
	if s.engine.pipelineRunner != nil {
		s.engine.pipelineRunner.Stop()
	}
	if s.engine.healthMonitor != nil {
		s.engine.healthMonitor.Stop()
	}
	// Mid-sweep monitors get a graceful Stop — yanking their context could cut
	// a signing-asset Upsert, a secret push, or an Actions list call in half.
	if s.engine.storeMonitor != nil {
		s.engine.storeMonitor.Stop()
	}
	if s.engine.deployMonitor != nil {
		s.engine.deployMonitor.Stop()
	}
	if s.engine.evolutionSvc != nil {
		s.engine.evolutionSvc.Stop()
	}
	// 3. Only now cancel anything still holding the run context.
	s.cancel()
	// 4. External processes, then the pool everything above was using.
	if s.engine.localRunner != nil {
		if err := s.engine.localRunner.Close(); err != nil {
			log.Warn().Err(err).Msg("closing the local release runner failed")
		}
	}
	if s.engine.mcpManager != nil {
		s.engine.mcpManager.Close()
	}
	if s.engine.browserSession != nil {
		s.engine.browserSession.Close()
	}
	// Release every device lease this process holds, so the next pod (or a
	// cloud member's Mac) can take the phones immediately instead of after
	// Appium's own newCommandTimeout.
	if s.engine.mobilePool != nil {
		s.engine.mobilePool.Close()
	}
	return err
}

func (e *engine) load(opts Options) error {
	e.opts = opts
	cfg, err := appconfig.Load(e.configPath)
	if err != nil {
		return err
	}
	applyLocalOverrides(cfg, opts)

	e.mu.Lock()
	e.cfg = cfg
	if e.reg == nil {
		e.reg = registry.New()
	}
	e.mu.Unlock()

	log.Info().
		Str("openai_compat", cfg.LLM.BaseURL).
		Int("port", cfg.Server.Port).
		Msg("config loaded")

	return e.registerBuiltinTools(cfg)
}

func applyLocalOverrides(cfg *domain.Config, opts Options) {
	if opts.PostgresDSN != "" {
		cfg.Storage.Postgres.DSN = opts.PostgresDSN
	}
	if opts.DataDir != "" {
		cfg.Storage.Sessions.WorkspaceRoot = filepath.Join(opts.DataDir, "workspaces")
		cfg.RAG.StorageDir = filepath.Join(opts.DataDir, "files")
		cfg.Tools.Terminal.WorkingDir = filepath.Join(opts.DataDir, "workspaces")
		cfg.AgentCatalog.CacheDir = filepath.Join(opts.DataDir, "catalog")
	}
	if len(opts.AllowedRoots) > 0 {
		cfg.Indexer.AllowedRoots = append(cfg.Indexer.AllowedRoots, opts.AllowedRoots...)
	}
	if opts.APIKey != "" {
		cfg.Server.APIKey = opts.APIKey
	}
	if opts.PublicBaseURL != "" {
		cfg.Server.PublicBaseURL = opts.PublicBaseURL
	}
	if opts.Port == 0 {
		cfg.Server.Port = 0
	}
}

func (e *engine) registerBuiltinTools(cfg *domain.Config) error {
	e.reg.Register(clarification.NewAskUserTool())
	log.Info().Msg("ask_user clarification tool enabled")

	// Registered here rather than with the index-gated code tools: working on a
	// file needs no index, and an agent on an unindexed repository is exactly
	// the one that would otherwise fall back to `sed`. Tool policy decides who
	// may call the writers.
	e.reg.Register(code.NewReadFileTool())
	e.reg.Register(code.NewWriteFileTool())
	e.reg.Register(code.NewEditFileTool())
	e.reg.Register(code.NewEditLinesTool())
	e.reg.Register(code.NewDeleteFileTool())
	e.reg.Register(code.NewMoveFileTool())
	log.Info().Msg("workspace file tools enabled")

	if cfg.Tools.Terminal.Enabled {
		e.reg.Register(shell.New(cfg.Tools.Terminal.WorkingDir, cfg.Tools.Terminal.Timeout, cfg.Tools.Terminal.MaxTimeout, cfg.Tools.Terminal.Sandbox))
		log.Info().Str("working_dir", cfg.Tools.Terminal.WorkingDir).Msg("shell tool enabled")
	}

	if cfg.Tools.Web.Enabled {
		e.reg.Register(web.New(cfg.Tools.Web.MaxResponseBytes))
		// download_file is a WORKSPACE WRITER, so it goes where the other
		// workspace writers go and not with fetch_url, which only ever reaches
		// the network and the model's context.
		//
		e.reg.Register(web.NewDownloadTool())
		log.Info().Msg("web fetch tools enabled")
	}

	if cfg.Tools.Search.Enabled {
		// No key gate any more: web_search implements its own keyless search
		// (DuckDuckGo, falling back to Bing), so "enabled" is the whole answer.
		// The gate that used to live here existed because a provider with no
		// subscription could only ever answer "not configured", which a
		// production run read as bad luck and retried three times.
		e.reg.Register(search.New(cfg.Tools.Search.MaxResults))
		log.Info().Msg("web search tool enabled")
	}

	if cfg.Tools.Browser.Enabled {
		if e.browserSession == nil {
			e.browserSession = browsertools.NewSession()
		}
		for _, tool := range browsertools.NewExecutors(e.browserSession) {
			e.reg.Register(tool)
		}
		log.Info().Msg("browser tools enabled")
	}

	llmTimeout := cfg.LLM.Timeout
	if llmTimeout <= 0 {
		llmTimeout = 300 * time.Second
	}
	e.bootstrapLLMFromYAML(cfg.LLM.BaseURL, cfg.LLM.Model, cfg.LLM.APIKey, llmTimeout)

	return nil
}

func (e *engine) bootstrapLLMFromYAML(baseURL, model, apiKey string, timeout time.Duration) {
	clientTimeout := llmprovider.ResolveTimeoutDuration(0, domain.LLMProviderLocal, timeout)
	fallback := llm.NewOpenAICompatClient(baseURL, model, apiKey, clientTimeout)
	if e.multiLLM == nil {
		// No resolver until buildHandler installs one (it needs the database);
		// every call uses the fallback client built from config.yml.
		e.multiLLM = llm.NewMultiProviderClient(fallback, nil)
	}
	e.llmClient = e.multiLLM
}

// wireRepositoryStore builds the repository store with the boot-time cipher,
// not a lazy one: its encrypted columns' first real use is always a later HTTP
// request, after scrubProcessSecrets wiped MCP_SECRETS_KEY from the env.
func wireRepositoryStore(pgDB *pgstore.DB, cfg *domain.Config, cipher *secrets.Cipher, cipherErr error) *pgstore.RepositoryStore {
	store := pgstore.NewRepositoryStore(pgDB).
		SetHostRoots(cfg.Storage.Sessions.WorkspaceRoot, cfg.Indexer.AllowedRoots)
	store.SetCipher(cipher, cipherErr)
	return store
}

func (e *engine) buildHandler(ctx context.Context, opts Options) *httpadapter.Handler {
	e.mu.RLock()
	cfg := e.cfg
	e.mu.RUnlock()

	if e.mcpManager == nil {
		e.mcpManager = &mcpadapter.Manager{}
	}

	var auditStore port.AuditLogger
	var sessionStore port.SessionStore
	var sessionActionStore port.SessionActionStore
	var jobStore port.JobStore
	var fileStore port.FileStore
	var attachmentStore port.AttachmentStore
	var activityStore port.ActivityStore
	var apiKeyStore port.APIKeyStore
	var catalogStore port.CatalogStore
	var agentCLIStore port.AgentCLIStore
	var indexStore port.IndexStore
	var embedMapStore port.EmbeddingMapStore
	var repositoryStore port.RepositoryStore
	var boardTaskStore port.BoardTaskStore
	var criterionStore port.AcceptanceCriterionStore
	var testCaseStore port.TaskTestCaseStore
	var relationStore port.TaskRelationStore
	var documentStore port.TaskDocumentStore
	var initiativeStore port.InitiativeProjectStore
	var boardConfigStore port.BoardConfigStore
	var roleStore port.RoleStore
	var workflowStore port.WorkflowStore
	var workflowSvc *workflowapp.Service
	var commentStore port.TaskCommentStore
	var boardEventStore port.BoardEventStore
	var taskAgentRunStore port.TaskAgentRunStore
	var taskSpanStore port.TaskColumnSpanStore
	var perfStore port.AgentPerformanceStore
	var memoryStore port.AgentMemoryStore
	var evolutionStore port.AgentEvolutionStore
	var catalogVersionStore port.CatalogVersionStore
	var kpiStore port.AgentKPIStore
	var goldenStore port.GoldenTaskStore
	var usageStore port.LLMUsageStore
	var mcpStore port.MCPStore
	var settingsStore port.SettingsStore
	var githubTokens port.GitHubTokenStore
	var llmProviderStore port.LLMProviderStore
	var llmEndpointStore port.LLMEndpointStore

	if cfg.Storage.Postgres.DSN != "" {
		pgPool, err := pgstore.NewPool(ctx, cfg.Storage.Postgres.DSN, cfg.Storage.Postgres.MaxConns)
		if err != nil {
			log.Warn().Err(err).Msg("postgres connection failed, sessions/jobs/audit/rag disabled")
		} else {
			e.pgPool = pgPool
			pgDB := pgstore.NewDB(pgPool)
			e.pgDB = pgDB
			// Seeds the default board once per install and runs boot steps;
			// built here because it needs the database.
			e.bootSeed = bootseed.NewService(pgstore.NewBoardSeedStore(pgDB))
			// SetHostRoots: sessions.workspace_dir/project_root are absolute
			// paths from whichever host wrote the row; re-anchor foreign ones or
			// resuming a pod-written chat reaches os.MkdirAll("/data/...") and
			// fails the turn.
			sessionStore = pgstore.NewSessionStore(pgDB).
				SetHostRoots(cfg.Storage.Sessions.WorkspaceRoot, cfg.Indexer.AllowedRoots)
			sessionActionStore = pgstore.NewSessionActionStore(pgDB)
			jobStore = pgstore.NewJobStore(pgDB)
			auditStore = pgstore.NewAuditStore(pgDB)
			fileStore = pgstore.NewFileStore(pgDB)
			attachmentStore = pgstore.NewAttachmentStore(pgDB)
			activityStore = pgstore.NewActivityStore(pgDB)
			apiKeyStore = pgstore.NewAPIKeyStore(pgDB)
			catalogStore = pgstore.NewCatalogStore(pgDB)
			catalogVersionStore = pgstore.NewCatalogVersionStore(pgDB)
			// Same again for workspace_indexes.root_path: re-anchored so the
			// skeleton walk lands on this host's checkout of the repo.
			indexStore = pgstore.NewIndexStore(pgDB).
				SetHostRoots(cfg.Storage.Sessions.WorkspaceRoot, cfg.Indexer.AllowedRoots)
			embedMapStore = pgstore.NewEmbeddingMapStore(pgDB)
			// SetHostRoots: repositories.root_path is absolute from whichever host
			// imported it, and one database is served by two hosts (the pod's PVC
			// plus the user's Mac behind a reverse tunnel); re-anchored on read,
			// or a board run dies in git clone at "/data: read-only file system".
			repositoryStore = wireRepositoryStore(pgDB, cfg, e.secretsCipher, e.secretsCipherErr)
			boardTaskStore = pgstore.NewBoardTaskStore(pgDB)
			criterionStore = pgstore.NewAcceptanceCriterionStore(pgDB)
			testCaseStore = pgstore.NewTaskTestCaseStore(pgDB)
			relationStore = pgstore.NewTaskRelationStore(pgDB)
			documentStore = pgstore.NewTaskDocumentStore(pgDB)
			initiativeStore = pgstore.NewInitiativeProjectStore(pgDB)
			boardConfigStore = pgstore.NewBoardConfigStore(pgDB)
			roleStore = pgstore.NewRoleStore(pgDB)
			workflowStore = pgstore.NewWorkflowStore(pgDB)
			workflowSvc = workflowapp.NewService(roleStore, workflowStore)
			commentStore = pgstore.NewTaskCommentStore(pgDB)
			boardEventStore = pgstore.NewBoardEventStore(pgDB)
			taskAgentRunStore = pgstore.NewTaskAgentRunStore(pgDB)
			taskSpanStore = pgstore.NewTaskColumnSpanStore(pgDB)
			perfStore = pgstore.NewPerformanceStore(pgDB)
			memoryStore = pgstore.NewMemoryStore(pgDB)
			evolutionStore = pgstore.NewEvolutionStore(pgDB)
			kpiStore = pgstore.NewKPIStore(pgDB)
			goldenStore = pgstore.NewGoldenStore(pgDB)
			usageStore = pgstore.NewLLMUsageStore(pgDB)
			mcpStore = pgstore.NewMCPStore(pgDB)
			pgSettings := pgstore.NewSettingsStore(
				pgDB,
				cfg.Storage.Sessions.WorkspaceRoot,
				"en",
				opts.DataDir != "",
			)
			// The boot-time cipher, not a lazy one: by first use the environment
			// no longer carries MCP_SECRETS_KEY.
			pgSettings.SetCipher(e.secretsCipher, e.secretsCipherErr)
			settingsStore = pgSettings
			githubTokens = pgSettings
			llmProviderStore = pgstore.NewLLMProviderStore(pgDB)
			llmEndpointStore = pgstore.NewLLMEndpointStore(pgDB)
			agentCLIStore = pgstore.NewAgentCLIStore(pgDB)
			log.Info().Msg("postgres connected")
		}
	} else {
		log.Warn().Msg("storage.postgres.dsn empty, sessions/jobs/audit/rag disabled")
	}

	var mcpConfigs []domain.MCPServerConfig
	var mcpService *mcpsvc.Service
	if mcpStore != nil {
		if e.secretsCipherErr != nil {
			log.Warn().Err(e.secretsCipherErr).Msg("mcp secrets cipher unavailable, secret storage disabled")
		}
		// The reload hook, a sibling of the cipher leak: mcp.Service calls it
		// after each create/update/delete and it rebuilds the process-wide
		// manager. Wired unconditionally — the refusal to rebuild lives in
		// reloadMCP itself; an early guard here was walked past by engine.reload.
		mcpService = mcpsvc.NewService(mcpStore, e.secretsCipher, e.reloadMCP)
		// Default MCP server catalog, seeded as a boot step after the board seed.
		e.bootSeed.AddStep("mcp_servers", mcpService.SeedDefaultsIfEmpty)
		resolved, err := mcpService.ResolvedConfigs(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("mcp server config resolve failed")
			mcpConfigs = nil
		} else {
			mcpConfigs = resolved
		}
	} else {
		log.Warn().Msg("postgres required for mcp server management")
	}
	if opts.LazyMCP {
		e.pendingMCP = mcpConfigs
		log.Info().Int("count", len(mcpConfigs)).Msg("deferring mcp server connections")
	} else {
		e.mcpManager.LoadAndRegister(ctx, mcpConfigs, e.reg)
	}

	// The embedding model is chosen from the UI (LLM settings), pinned on the
	// MultiProviderClient; the empty name here is only the never-configured
	// fallback.
	embeddingModel := ""

	mapperSvc := mapper.NewService(cfg.Mapping)
	chunkers := chunker.DefaultRegistry()

	llmClient := e.llmClient
	if e.multiLLM != nil {
		llmClient = e.multiLLM
		// Pacing lives on the shared client so indexer, RAG uploads and query
		// rewriting draw on one embedding quota.
		e.multiLLM.SetEmbeddingLimits(cfg.Embedding)
	}
	if usageStore != nil {
		llmClient = usageapp.NewRecordingClient(llmClient, usageStore)
	}
	// Cache wraps OUTSIDE the recording client: a cache hit never calls the
	// inner client, so no usage is recorded for an unbilled call.
	llmClient = usageapp.NewCachingEmbedder(llmClient, cfg.Embedding.QueryCacheEntries)

	if indexStore != nil {
		codeKit := code.NewToolKit(indexStore, llmClient, mapperSvc, cfg.Indexer, cfg.Graph, embeddingModel)
		for _, tool := range code.NewExecutors(codeKit) {
			e.reg.Register(tool)
		}
	}

	// Ledger recording wraps the auditing registry so every run leaves a durable
	// trace of touched board entities (the loop's own trace does not outlive it).
	toolReg := registry.NewWorkspaceRegistry(
		registry.NewActionRecordingRegistry(
			registry.NewAuditingRegistry(e.reg, auditStore),
			sessionActionStore,
		),
		mcpadapter.NewScopedFilesystem(),
	)
	maxToolOutput := cfg.Tools.MaxToolOutputChars
	if maxToolOutput <= 0 {
		maxToolOutput = 16000
	}
	e.agentLoop = agent.NewLoop(llmClient, toolReg, cfg.LLM.MaxIterations, cfg.LLM.TaskMaxIterations, maxToolOutput)
	// The executor is only known to exist once the board block probes for the
	// CLI, so the router is built here with a live-object setter instead of
	// constructor wiring.
	e.agentRouter = agent.NewRouter(e.agentLoop)

	var ragSvc *rag.Service
	if fileStore != nil && cfg.RAG.Enabled {
		ragSvc = rag.NewService(fileStore, llmClient, cfg.RAG)
	}

	// Binary attachments for tasks and chat: Postgres-backed bytes, because the
	// pod disk the RAG pipeline writes to is ephemeral.
	var attachmentSvc *attachmentapp.Service
	if attachmentStore != nil {
		attachmentSvc = attachmentapp.NewService(attachmentStore, boardTaskStore)
		// Tools' screenshots go into the store the UI already fetches from, so
		// the feed can show the human the picture the model was handed.
		if e.agentLoop != nil {
			e.agentLoop.SetScreenshotArchiver(attachmentSvc)
		}
	}

	budget := appcontext.Budget{
		MaxTokens:          cfg.Context.MaxTokens,
		ReserveOutput:      cfg.Context.ReserveOutput,
		SummarizeThreshold: cfg.Context.SummarizeThreshold,
		KeepRecentMessages: cfg.Context.KeepRecentMessages,
	}
	if budget.MaxTokens <= 0 {
		budget.MaxTokens = 32000
	}
	if budget.ReserveOutput <= 0 {
		budget.ReserveOutput = 4096
	}
	// Defaulted before the loop gets the budget: it is a value, so a later fill
	// never reaches the loop's copy, and its trim reads KeepRecentMessages
	// verbatim — zero there means a running history with nothing kept.
	if budget.KeepRecentMessages <= 0 {
		budget.KeepRecentMessages = 10
	}
	summarizer := appcontext.NewLLMSummarizer(llmClient)
	titleGen := session.NewLLMTitleGenerator(llmClient)
	// The loop trims with the same budget its callers do, or tool results grow
	// the request back past it many times over.
	e.agentLoop.SetHistoryBudget(budget)
	// Condenses dropped history into one fixed-position block so the request
	// prefix stays byte-identical between trims (provider cache survives).
	e.agentLoop.SetSummarizer(summarizer)
	// Mid-run token breaker: billing gates runs only before they start and
	// iteration limits count turns, so this catches a run blowing its budget
	// during execution. 0 disables.
	e.agentLoop.SetRunTokenCap(cfg.LLM.RunMaxTotalTokens)

	var indexSvc *indexer.Service
	var indexInjector *indexer.Injector
	var repositorySvc *repository.Service
	var modelSvc *projectmodel.Service
	var initiativeSvc *initiative.Service
	var workspaceSvc *workspace.Service
	var boardDispatcher *boardapp.Dispatcher
	if indexStore != nil && cfg.Indexer.Enabled {
		indexSvc = indexer.NewService(indexStore, llmClient, mapperSvc, chunkers, cfg.Indexer, cfg.Graph, embeddingModel)
		indexSvc.SetEmbeddingLimits(cfg.Embedding)
		indexInjector = indexer.NewInjector(indexStore, llmClient, mapperSvc, embeddingModel, cfg.Graph)
		indexInjector.SetQueryRewrite(cfg.Indexer.QueryRewrite)
		if pgIndexStore, ok := indexStore.(*pgstore.IndexStore); ok && e.pgDB != nil {
			caps := pgstore.DetectVectorCapabilities(ctx, e.pgDB)
			if caps.Vector {
				caps.Vector = pgstore.BootstrapWorkspaceVectors(ctx, e.pgDB)
			}
			pgIndexStore.SetCapabilities(caps)
		}
	}

	var contextBuilder *orchestrator.ContextBuilder
	if indexInjector != nil {
		contextBuilder = orchestrator.NewContextBuilder(indexInjector, budget, cfg.Indexer, cfg.Mapping)
	}

	var catalogSvc *catalog.Service
	var orchSvc *orchestrator.Service
	if catalogStore != nil {
		catalogSvc = catalog.NewService(catalogStore, llmClient, embeddingModel)
		catalogSvc.SetSkillBudget(cfg.Evolution.MaxSkillsPerAgent)
		if llmProviderStore != nil {
			catalogSvc.SetLLMProviders(llmProviderStore)
		}
		if e.pgDB != nil {
			catalogSvc.SetTemplateStore(pgstore.NewAgentTemplateStore(e.pgDB))
		}
		if kpiStore != nil {
			catalogSvc.SetKPIStore(kpiStore)
		}
		if boardConfigStore != nil {
			catalogSvc.SetBoardConfigStore(boardConfigStore)
		}
		if catalogVersionStore != nil {
			catalogSvc.SetVersionStore(catalogVersionStore)
		}
		if workflowSvc != nil {
			catalogSvc.SetRoleAdmin(workflowSvc)
		}
		for _, tool := range skilltools.NewExecutors(&skilltools.ToolKit{
			Catalog: catalogStore,
			Creator: catalogSvc,
			Events:  evolutionStore,
			Perf:    perfStore,

			MaxSkills: cfg.Evolution.MaxSkillsPerAgent,
		}) {
			e.reg.Register(tool)
		}
		// The role agents are no longer created here: the six built-ins arrive
		// through the external catalog sync, and the gallery is populated from
		// the live agents. Boot only backfills embeddings for template agents
		// saved without a vector.
		e.bootSeed.AddStep("skill_embeddings", func(stepCtx context.Context) error {
			// Outlives the boot step's deadline on purpose: a first launch is
			// still downloading the model.
			go func() {
				backfillCtx, cancel := context.WithTimeout(context.WithoutCancel(stepCtx), 30*time.Minute)
				defer cancel()
				for {
					n, bfErr := catalogSvc.BackfillSkillEmbeddings(backfillCtx)
					if bfErr == nil {
						if n > 0 {
							log.Info().Int("updated", n).Msg("skill embeddings backfilled")
						}
						return
					}
					select {
					case <-backfillCtx.Done():
						log.Warn().Err(bfErr).Msg("skill embedding backfill gave up; skills without a vector are not found by semantic search")
						return
					case <-time.After(30 * time.Second):
					}
				}
			}()
			return nil
		})
		orchSvc = orchestrator.NewService(llmClient, catalogStore, nil, e.agentRouter, cfg.Orchestration, contextBuilder)
		// Intake and the planner run without tools; the workspace is what keeps
		// them from asking the stakeholder about repositories the system knows.
		if initiativeStore != nil && repositoryStore != nil {
			orchSvc.SetWorkspace(workspaceLister{projects: initiativeStore, repos: repositoryStore})
		}
		// A dependent subtask must read the ledger fresh to see what its
		// dependency just put on the board.
		if sessionActionStore != nil {
			orchSvc.SetSessionActions(sessionActionStore)
		}
	}

	var memorySvc *memoryapp.Service
	var memoryToolKit *memorytools.ToolKit
	if memoryStore != nil {
		memorySvc = memoryapp.NewService(memoryStore, llmClient, embeddingModel, cfg.Evolution.MemoryMaxCount)
		memoryToolKit = &memorytools.ToolKit{Memories: memorySvc}
		for _, tool := range memorytools.NewExecutors(memoryToolKit) {
			e.reg.Register(tool)
		}
	}

	var kpiSvc *kpiapp.Service
	if kpiStore != nil && perfStore != nil {
		kpiSvc = kpiapp.NewService(kpiStore, kpiapp.MetricDeps{
			Perf:  perfStore,
			Runs:  taskAgentRunStore,
			Tasks: boardTaskStore,
			Spans: taskSpanStore,
		})
		if workflowSvc != nil {
			kpiSvc.SetWorkflows(workflowSvc)
			kpiSvc.SetRoleResolver(workflowSvc)
		}
	}

	var boardRunner *boardapp.Runner
	// hostChatExecutor is the SAME mux the board's TaskExecutor uses, not a
	// second one: the concurrency cap, slot queue and subscriptions are
	// per-executor, so a duplicate would let chat and the board each believe
	// they had the whole budget. Nil means "this provider cannot run here".
	var hostChatExecutor port.ChatExecutor
	if boardConfigStore != nil {
		workspaceSvc = workspace.NewService(boardConfigStore)
	}
	if workflowSvc != nil {
		workflowSvc.SetBoardColumnLister(boardConfigStore)
		// The snapshot must exist before anything reads it — AgentForRole etc.
		// fail closed on an empty cache. Every write reloads; this is the boot.
		if err := workflowSvc.Reload(ctx); err != nil {
			log.Warn().Err(err).Msg("workflow: initial snapshot load failed; roles/workflows unavailable until the next successful write")
		}
		if workspaceSvc != nil {
			workspaceSvc.SetWorkflowStageChecker(workflowSvc)
		}
	}
	gitClient := gitadapter.NewClient()
	if githubTokens != nil {
		gitClient.SetTokenSource(githubTokens.GitHubToken)
	}
	// Billing: USD budget (shown as tokens).
	var billingSvc *billing.Service
	if e.pgDB != nil {
		billingSvc = billing.NewService(pgstore.NewBillingStore(e.pgDB))
		e.billingSvc = billingSvc
	}
	// Everything that dispatches board work — the runner's workers, the budget
	// resume tick, the stale-task reconciler — is collected here and run only
	// once all wiring below is complete.
	var activateBoard []func()
	var boardRunControl httpadapter.BoardRunControl
	// Both left nil (503) when there is no board: the stop/re-run routes and
	// the task-chat route must not dereference a runner that was never built.
	var boardTaskChat httpadapter.TaskChatControl
	var taskChatWorkspaces session.TaskWorkspaceResolver
	var localPreviewSvc *localpreviewapp.Service

	// Both stores are required: without the catalog there is nothing to write
	// out, and without the connection row nowhere to record verification. The
	// probe follows the SESSIONS: with a control plane in front, `claude` and
	// the subscription live on the member's Mac, so connect reads the pushed
	// environment report (claudecode.Preflight) instead of running anything
	// here. Without one, unchanged: the probe gets the executor's own binary
	// and settings, so it verifies the same program a board run will start.
	var agentCLISvc *agentcli.Service
	if agentCLIStore != nil && catalogStore != nil {
		bins := agentcli.ProbeBinaries{
			ClaudeBinary:         cfg.ClaudeCode.Binary,
			ClaudeSettingSources: cfg.ClaudeCode.SettingSources,
			AntigravityBinary:    cfg.Antigravity.Binary,
			CursorBinary:         cfg.CursorAgent.Binary,
			OpencodeBinary:       cfg.Opencode.Binary,
		}
		agentCLISvc = agentcli.NewService(agentcli.Deps{
			Store:         agentCLIStore,
			Catalog:       catalogStore,
			WorkspaceRoot: cfg.Storage.Sessions.WorkspaceRoot,
			Probe:         agentcli.DefaultProbe(bins),
			Models:        agentcli.DefaultModels(bins),
			Runtimes:      agentRuntimes(catalogSvc),
		})
	}

	// The criteria-loop guard is wired onto it later, next to the review- and
	// pipeline-bounce guards, once repositorySvc exists.
	var reconciler *boardapp.Reconciler

	if boardConfigStore != nil && boardEventStore != nil && taskAgentRunStore != nil && catalogStore != nil {
		// A typed-nil *indexer.Service must not become a non-nil interface.
		var branchIndexer boardapp.BranchIndexer
		if indexSvc != nil {
			branchIndexer = indexSvc
		}
		boardRunner = boardapp.NewRunner(boardapp.RunnerDeps{
			AgentLoop:     e.agentRouter,
			OrchSvc:       orchSvc,
			Catalog:       catalogStore,
			ActivityStore: activityStore,
			Runs:          taskAgentRunStore,
			Sessions:      sessionStore,
			IndexInjector: indexInjector,
			BranchIndexer: branchIndexer,
			PerfStore:     perfStore,
			Memories:      memoryStore,
			KPIs:          kpiStore,
			Notifier:      desktopadapter.NewNotifier("local-llm"),
			Git:           gitClient,
			LLM:           llmClient,
			WorkspaceRoot: cfg.Storage.Sessions.WorkspaceRoot,
			Budget:        budget,
			IndexerCfg:    cfg.Indexer,
			MappingCfg:    cfg.Mapping,
			DefaultPolicy: cfg.Tools.DefaultPolicy,
			Settings:      settingsStore,
			// The Claude Code session cap, handed to the claim so it is a
			// budget shared by every replica rather than a channel per pod.
			VerificationEnabled: cfg.Board.VerificationEnabled,
			VerifyFixAttempts:   cfg.Board.VerifyMaxFixAttempts,
			TaskTypeModels:      cfg.Board.TaskTypeModels,
		})
		// Guarded rather than passed unconditionally: a typed-nil *agentcli.Service
		// in a non-nil interface would turn "the connect flow was never wired"
		// into a nil dereference on the first claude_code dispatch.
		if agentCLISvc != nil {
			boardRunner.SetAgentCLIConnections(agentCLISvc)
		}
		boardRunner.SetToolchainDetector(localtoolchain.New(cfg.Storage.Sessions.WorkspaceRoot))
		if workflowSvc != nil {
			boardRunner.SetWorkflows(workflowSvc)
			boardRunner.SetRoleResolver(workflowSvc)
		}
		boardDispatcher = boardapp.NewDispatcher(boardConfigStore, boardEventStore, taskAgentRunStore, boardRunner, cfg.Board.DispatchEnabled)
		if workflowSvc != nil {
			boardDispatcher.SetWorkflows(workflowSvc)
			boardDispatcher.SetRoleResolver(workflowSvc)
		}
		if taskSpanStore != nil {
			boardDispatcher.SetSpans(taskSpanStore)
		}
		// A park is a move the dispatcher never sees (see board.ParkJournal), so
		// the runner writes its event and its span itself. Same two stores the
		// dispatcher was just given, deliberately: the row a park leaves behind
		// has to be indistinguishable from the one an ordinary move leaves.
		boardRunner.SetParkJournal(boardapp.NewParkJournal(boardEventStore, taskSpanStore))
		if boardTaskStore != nil {
			boardRunner.SetTaskBlocker(boardTaskStore)
			// Entering code_review is where a task's PR is guaranteed to exist, so
			// it is where the task records which PR it is in.
			boardRunner.SetTaskPRRecorder(boardTaskStore)
			boardRunControl = boardapp.NewController(taskAgentRunStore, boardEventStore, boardTaskStore, boardRunner)
		}

		// WHERE a Claude Code session runs. One decision, two true answers, and
		// not a feature flag with an off state:
		//
		//   remote — the shared cloud deployment. Neither of the two things a
		//     session needs is here: the working copy is a checkout in
		//     somebody's home directory and the subscription is on their
		//     laptop. So the session runs THERE, over the control plane's
		//     tunnel, and the board consumes its stream exactly as it consumes
		//     a local one.
		//   local — a self-hosted install or the desktop bundle, unchanged.
		//     Conditional registration, like the mobile tools above it: the
		//     switch is whether the binary exists on this host, not a config
		//     flag, because an operator cannot enable a program that is not
		//     installed and a registered-but-broken executor turns a clear boot
		//     log line into a confusing failure an hour later.
		//
		// Registering NEITHER is a complete, correct outcome: the board runner
		// then fails a claude_code agent's run with one sentence naming what is
		// missing, and every other provider is untouched.
		var claudeExecutor port.TaskExecutor
		var antigravityExecutor port.TaskExecutor
		var cursorExecutor port.TaskExecutor
		var opencodeExecutor port.TaskExecutor
		// The MCP endpoint/server serve every host CLI provider, not just
		// claude: cursor, opencode and antigravity all take the SAME claudeMCP
		// provider below, so a host missing the claude binary still serves
		// TaskTrooper's tools to whichever of the others IS installed. Built
		// unconditionally, ahead of every New() call, so none of the four
		// depends on claude's own executor having registered successfully. The
		// endpoint is the one Run already published the bound address on, so a
		// board run dispatched by the activation at the end of this function has
		// a reachable URL from its first millisecond. Allocated here only for
		// the callers that build a handler without going through Run.
		if e.mcpEndpoint == nil {
			e.mcpEndpoint = &mcpEndpoint{}
		}
		claudeMCP := &claudeCodeMCP{endpoint: e.mcpEndpoint, tokens: mcpserver.NewRunTokenRegistry(), registry: toolReg}
		// toolReg, not the bare registry: the audit and action-ledger
		// decorators are what make a CLI session's tool call show up in the
		// same places a loop run's does.
		e.mcpServer = mcpserver.New(toolReg, claudeMCP.tokens)
		// This route sits outside the prefixes the auth middlewares gate, so
		// the per-run token would otherwise be the only thing in front of
		// this install's board tools. The only legitimate clients are CLI
		// children on this host.
		e.mcpServer.SetLoopbackOnly(true)

		if executor, ccErr := claudecode.New(claudecode.Config{
			Binary:     cfg.ClaudeCode.Binary,
			MaxTurns:   cfg.ClaudeCode.MaxTurns,
			RunTimeout: cfg.ClaudeCode.RunTimeout,
			// Empty resolves to claudecode.DefaultSettingSources, which keeps the
			// operator's own hooks and plugins out of a board run.
			SettingSources: cfg.ClaudeCode.SettingSources,
			// Per-run endpoint and credential, minted at the start of each
			// session and revoked at its end. See claudecode_mcp.go.
			MCPProvider:           claudeMCP,
			MaxConcurrentSessions: cfg.ClaudeCode.MaxConcurrentSessions,
		}); ccErr != nil {
			log.Info().Err(ccErr).Msg("agent cli executor not registered; agents on the claude_code provider cannot run on this host")
		} else {
			claudeExecutor = executor
			log.Info().
				Str("mcp_endpoint", e.mcpEndpoint.get()).
				Msg("agent cli executor enabled, tasktrooper tools served over mcp")
		}

		if executor, agErr := antigravity.New(antigravity.Config{
			Binary:      cfg.Antigravity.Binary,
			RunTimeout:  cfg.Antigravity.RunTimeout,
			MCPProvider: claudeMCP,
		}); agErr != nil {
			log.Info().Err(agErr).Msg("antigravity executor not registered; agents on the antigravity provider cannot run on this host")
		} else {
			antigravityExecutor = executor
			log.Info().Str("mcp_endpoint", e.mcpEndpoint.get()).Msg("antigravity executor enabled, tasktrooper tools served over mcp")
		}

		if executor, curErr := cursor.New(cursor.Config{
			Binary:      cfg.CursorAgent.Binary,
			RunTimeout:  cfg.CursorAgent.RunTimeout,
			MCPProvider: claudeMCP,
		}); curErr != nil {
			log.Info().Err(curErr).Msg("cursor executor not registered; agents on the cursor_agent provider cannot run on this host")
		} else {
			cursorExecutor = executor
			log.Info().Str("mcp_endpoint", e.mcpEndpoint.get()).Msg("cursor executor enabled, tasktrooper tools served over mcp")
		}

		if executor, ocErr := opencode.New(opencode.Config{
			Binary:      cfg.Opencode.Binary,
			RunTimeout:  cfg.Opencode.RunTimeout,
			MCPProvider: claudeMCP,
		}); ocErr != nil {
			log.Info().Err(ocErr).Msg("opencode executor not registered; agents on the opencode provider cannot run on this host")
		} else {
			opencodeExecutor = executor
			log.Info().Str("mcp_endpoint", e.mcpEndpoint.get()).Msg("opencode executor enabled, tasktrooper tools served over mcp")
		}

		mux := &muxExecutor{
			executors: map[domain.LLMProviderType]port.TaskExecutor{
				domain.LLMProviderClaudeCode:  claudeExecutor,
				domain.LLMProviderAntigravity: antigravityExecutor,
				domain.LLMProviderCursorAgent: cursorExecutor,
				domain.LLMProviderOpencode:    opencodeExecutor,
			},
		}
		// The SAME mux answers chat, not a claude-only variable any more: two
		// providers (claude_code, opencode) now have a remote executor that
		// implements port.ChatExecutor too, and a chat turn's provider decides
		// which one exactly the way a board task's does. See
		// muxExecutor.ExecuteChat.
		hostChatExecutor = mux

		if claudeExecutor != nil || antigravityExecutor != nil || cursorExecutor != nil || opencodeExecutor != nil {
			boardRunner.SetTaskExecutor(mux)
			if e.agentRouter != nil {
				e.agentRouter.SetTaskExecutor(mux)
			}
			if catalogSvc != nil {
				catalogSvc.SetHostExecutorProbe(e.agentRouter.SupportsHostExecution)
			}
		}

		if claudeExecutor != nil {
			// The release half of the usage-limit park. A task parked because
			// the subscription was spent has nothing to wake it — no answer
			// arrives, no human drags it — so the sweep is the only way back.
			// Registered with the executor rather than unconditionally: without
			// a CLI to run, nothing can park on the quota in the first place,
			// and a sweeper for a state that cannot occur is a query per minute
			// forever for nothing. Both executors need it: a Mac has one
			// subscription too, and it is spent the same way.
			if boardTaskStore != nil && boardDispatcher != nil {
				quotaSweeper := boardapp.NewQuotaSweeper(boardTaskStore, boardDispatcher)
				activateBoard = append(activateBoard, func() {
					quotaSweeper.Start(ctx, boardapp.QuotaSweeperInterval)
				})
			}
		}

		// Starting the workers here would race the SetRepositories/SetTaskUpdater/
		// SetPipelines wiring further down: a worker that picks up a job in that
		// window dereferences a nil resolver. Activation is deferred until every
		// dependency is in place (which also makes those writes happen-before the
		// worker goroutines that read them).
		activateBoard = append(activateBoard, func() { boardRunner.Start(ctx) })
		e.boardRunner = boardRunner

		if billingSvc != nil {
			boardRunner.SetBilling(billingSvc)
			if boardTaskStore != nil && boardDispatcher != nil {
				dispatcher := boardDispatcher
				taskStore := boardTaskStore
				billingSvc.SetRedispatcher(func(rctx context.Context, repoID, taskID uuid.UUID) error {
					task, err := taskStore.Get(rctx, repoID, taskID)
					if err != nil {
						return err
					}
					return dispatcher.Dispatch(rctx, boardapp.DispatchInput{
						RepositoryID: repoID,
						Task:         task,
						EventType:    domain.BoardEventTaskMoved,
						Payload: map[string]interface{}{
							"resumed":                 "quota_renewed",
							domain.EventPayloadActor:  domain.EventActorSystem,
							domain.EventPayloadReason: domain.MoveReasonQuotaRenewed,
						},
					})
				})
			}
			// Resume on startup and every 15 minutes after: once a budget period
			// rolls over, tasks parked on the spent budget have nothing else to
			// wake them.
			activateBoard = append(activateBoard, func() {
				billingSvc.Tick(ctx)
				go func() {
					t := time.NewTicker(15 * time.Minute)
					defer t.Stop()
					for {
						select {
						case <-ctx.Done():
							return
						case <-t.C:
							billingSvc.Tick(ctx)
						}
					}
				}()
			})
		}

		if boardTaskStore != nil && cfg.Board.DispatchEnabled {
			reconciler = boardapp.NewReconciler(taskAgentRunStore, boardTaskStore, boardDispatcher, cfg.Board.ReconcileStaleAfter)
			if workflowSvc != nil {
				reconciler.SetWorkflows(workflowSvc)
				reconciler.SetRoleResolver(workflowSvc)
			}
			// No live-run checker: it asked THIS process, which on a shared deployment
			// reports every other replica's live run as abandoned; the run row's
			// own heartbeat is the answer every process can see. The plan settler
			// instead recovers a killed process's plan/subtasks, whose rows stay
			// "running" after the run row is recovered.
			reconciler.SetPlanSettler(catalogStore)
			// Sweeps immediately on start — same dispatch-this-second reason as
			// the runner above.
			activateBoard = append(activateBoard, func() {
				reconciler.Start(ctx, cfg.Board.ReconcileInterval)
			})
		}
	}

	var scoreTracker *boardapp.ScoreTracker
	if repositoryStore != nil && boardTaskStore != nil {
		repositorySvc = repository.NewService(repositoryStore, boardTaskStore, criterionStore, relationStore, documentStore, commentStore, workspaceSvc, boardDispatcher, indexSvc, cfg.Indexer.AllowedRoots)
		if workflowSvc != nil {
			repositorySvc.SetWorkflows(workflowSvc)
			repositorySvc.SetRoleResolver(workflowSvc)
		}
		repositorySvc.SetGit(gitClient, cfg.Storage.Sessions.WorkspaceRoot)
		repositorySvc.SetPublicBaseURL(cfg.Server.PublicBaseURL)
		repositorySvc.SetTestCaseStore(testCaseStore)
		// The index mirror's restorer, wired here rather than into the indexer's
		// constructor (a dependency cycle: the repository service already holds
		// the indexer). It stops a pass from walking a checkout that is not
		// there — the mirror is a pod-disk cache no restart preserves, and an
		// index pass that reported completed over nothing would show a green
		// 100% over an index answering no query.
		if indexSvc != nil {
			indexSvc.SetMirrorRestorer(repositorySvc)
		}
		if perfStore != nil {
			scoreTracker = boardapp.NewScoreTracker(perfStore)
			if taskSpanStore != nil {
				scoreTracker.SetSpans(taskSpanStore)
			}
			if testCaseStore != nil {
				scoreTracker.SetTestCases(testCaseStore)
			}
			scoreTracker.SetEvents(perfStore)
			scoreTracker.SetWorkflows(workflowSvc)
			repositorySvc.SetScorer(scoreTracker)
		}
		if taskSpanStore != nil {
			repositorySvc.SetCompletionStamper(boardapp.NewCompletionStamper(boardTaskStore, taskSpanStore))
			// The span ledger is also the review-chain gate's evidence: which stages a
			// task actually passed through.
			repositorySvc.SetSpanStore(taskSpanStore)
			if scoreTracker != nil {
				reviewGate := boardapp.NewReviewGate(taskSpanStore, scoreTracker)
				if workflowSvc != nil {
					reviewGate.SetWorkflows(workflowSvc)
					reviewGate.SetRoleResolver(workflowSvc)
				}
				repositorySvc.SetReviewGate(reviewGate)
			}
		}
		if boardRunner != nil {
			boardRunner.SetRepositories(repositorySvc)
			boardRunner.SetTaskUpdater(repositorySvc)
		}
		repositorySvc.SetRequireCriteriaComplete(cfg.Board.RequireCriteriaComplete)

		// Work order — the `blocks` relation, enforced where a run starts (the
		// dispatcher) and a park's only exit (the sweeper). Registered on the
		// relation store being present; without it the board dispatches as before.
		if relationStore != nil && boardDispatcher != nil {
			workOrder := boardapp.NewWorkOrder(relationStore, boardTaskStore)
			// The commenter is the SERVICE, not the store, so the comment lands
			// with the board event that makes it visible in task history.
			workOrder.SetCommenter(repositorySvc)
			boardDispatcher.SetWorkOrder(workOrder)
			workOrderSweeper := boardapp.NewWorkOrderSweeper(boardTaskStore, relationStore, boardDispatcher)
			workOrderSweeper.SetDependents(relationStore)
			workOrderSweeper.SetCommenter(repositorySvc)
			repositorySvc.SetWorkOrderSweeper(workOrderSweeper)
			activateBoard = append(activateBoard, func() {
				workOrderSweeper.Start(ctx, boardapp.WorkOrderSweeperInterval)
			})
		}

		// Review-cycle cap: a task stuck bouncing in need_revision with no human
		// stops being dispatched. The streak IS board history, so the guard
		// hangs off the event store — nothing else survives restarts and the
		// multi-pod delivery the loop it brakes ran across.
		if boardDispatcher != nil && boardEventStore != nil {
			reviewLoop := boardapp.NewReviewLoopGuard(boardEventStore, boardTaskStore)
			reviewLoop.SetCommenter(repositorySvc)
			// The parking move out of need_revision, written by no one else;
			// without it the card jumps to blocked unexplained and the
			// need_revision span never closes.
			reviewLoop.SetParkJournal(boardapp.NewParkJournal(boardEventStore, taskSpanStore))
			boardDispatcher.SetReviewLoopGuard(reviewLoop)
		}

		// Criteria-loop cap, the third self-talking shape, closed the same way: a
		// task whose runs keep exhausting the criteria sweep with the same
		// criteria open stops being retried. Not detected on an incoming event,
		// so it hangs off the reconciler rather than the dispatcher.
		if reconciler != nil && boardEventStore != nil && boardTaskStore != nil {
			criteriaLoop := boardapp.NewCriteriaLoopGuard(boardEventStore, boardTaskStore, repositorySvc)
			criteriaLoop.SetCommenter(repositorySvc)
			criteriaLoop.SetParkJournal(boardapp.NewParkJournal(boardEventStore, taskSpanStore))
			reconciler.SetCriteriaLoopGuard(criteriaLoop)
		}

		// Project model: the scanned component/check/link structure that
		// replaced the markdown profile. Postgres-only, like the profile it
		// replaces.
		if e.pgDB != nil {
			modelSvc = projectmodel.NewService(projectmodel.Deps{
				Store:     pgstore.NewProjectModelStore(e.pgDB),
				Repos:     repositoryStore,
				Projector: repositoryStore,
				Projects:  initiativeStore,
				Pipelines: pgstore.NewRepositoryPipelineJobStore(e.pgDB),
				Scanner:   discovery.New(),
				Legacy:    pgstore.NewLegacyModelSource(e.pgDB),
			})
			modelSvc.SetBackgroundContext(ctx)
			modelSvc.Boot(ctx)
			for _, tool := range projectmodeltools.NewExecutors(&projectmodeltools.ToolKit{Model: modelSvc}) {
				e.reg.Register(tool)
			}
			repositorySvc.SetModelRefresher(modelSvc)
			repositorySvc.SetComponentResolver(modelSvc)
			if boardRunner != nil {
				boardRunner.SetProjectModel(modelSvc)
			}
			if orchSvc != nil {
				orchSvc.SetProjectModel(modelSvc)
			}
		}
		e.projectModelSvc = modelSvc

		if attachmentStore != nil {
			// Task detail responses carry attachment metadata alongside documents.
			repositorySvc.SetAttachmentStore(attachmentStore)
		}
		boardKit := &board.ToolKit{Tasks: repositorySvc}
		if workflowSvc != nil {
			boardKit.Workflows = workflowSvc
			boardKit.Roles = workflowSvc
		}
		if initiativeStore != nil {
			boardKit.Workspace = workspaceLister{projects: initiativeStore, repos: repositoryStore}
		}
		if catalogStore != nil {
			boardKit.Team = catalogStore
		}
		if boardConfigStore != nil {
			boardKit.Subscriptions = boardConfigStore
		}
		if attachmentSvc != nil {
			boardKit.Attachments = attachmentSvc
		}
		if modelSvc != nil {
			boardKit.Components = modelSvc
		}
		// The task<->pull-request use case needs GitHub for everything but the
		// commit, so these tools are registered only when a token store exists —
		// otherwise three tools that could only report "GitHub is not connected".
		var taskPRSvc *boardapp.TaskPRService
		if githubTokens != nil {
			taskPRSvc = boardapp.NewTaskPRService(boardapp.TaskPRServiceDeps{
				Tasks:  boardTaskStore,
				Repos:  repositorySvc,
				Git:    gitClient,
				PRs:    githubapi.NewPRAPI(),
				Tokens: githubTokens.GitHubToken,
				Agents: catalogStore,
				LLM:    llmClient,
				// The merge re-asks the same gates that guard the move into done,
				// so there is one definition of each.
				Gates:         repositorySvc,
				WorkspaceRoot: cfg.Storage.Sessions.WorkspaceRoot,
			})
			boardKit.PullRequests = taskPRSvc
			// The same reader the tools use, so a revision run is handed the
			// reviewer's comments without a tool call for them.
			if boardRunner != nil {
				boardRunner.SetPullRequestReader(taskPRSvc)
			}
		}
		for _, tool := range board.NewExecutors(boardKit) {
			e.reg.Register(tool)
		}
		// The task chat and its branch checkout; built here beside the tools the
		// chat's agent calls.
		if sessionStore != nil {
			boardTaskChat = boardapp.NewTaskChatOpener(boardapp.TaskChatOpenerDeps{
				Tasks:    boardTaskStore,
				Sessions: sessionStore,
				Agents:   catalogStore,
				Columns:  boardConfigStore,
				Repos:    repositorySvc,
				Criteria: criterionStore,
			})
		}
		taskChatWorkspaces = taskChatWorkspace{
			tasks:         boardTaskStore,
			criteria:      criterionStore,
			repos:         repositorySvc,
			git:           gitClient,
			workspaceRoot: cfg.Storage.Sessions.WorkspaceRoot,
		}
		// The human_uat reviewer's manual pass at a task's branch — same checkout
		// as the chat above, run instead of talked to.
		localPreviewSvc = localpreviewapp.NewService(localpreviewapp.Deps{
			Tasks:         boardTaskStore,
			Repositories:  repositorySvc,
			Git:           gitClient,
			WorkspaceRoot: cfg.Storage.Sessions.WorkspaceRoot,
		})

		if settingsStore != nil && cfg.Tools.BoilerplateCatalog.Enabled {
			e.reg.Register(boilerplatetools.New(settingsStore))
			log.Info().Msg("boilerplate catalog tool enabled")
		}

		if e.pgDB != nil {
			pipelineStore := pgstore.NewPipelineStore(e.pgDB)
			pipelineJobStore := pgstore.NewRepositoryPipelineJobStore(e.pgDB)
			if boardRunner != nil {
				boardRunner.SetPipelines(pipelineStore)
			}
			// QA stays a nil interface (not a typed-nil *Dispatcher) when
			// boardDispatcher is nil, so PipelineRunner's `p.qa != nil` check
			// does not trip the typed-nil interface trap.
			var qaDispatcher boardapp.QADispatcher
			if boardDispatcher != nil {
				qaDispatcher = boardDispatcher
			}
			var tokenSource boardapp.TokenSource
			if githubTokens != nil {
				tokenSource = githubTokens.GitHubToken
			}
			// Built here rather than beside deploySvc below: the pipeline runner needs
			// it to tell a store prod deploy from a server one.
			deployTargetStore := pgstore.NewDeployTargetStore(e.pgDB)
			pipelineRunner := boardapp.NewPipelineRunner(boardapp.PipelineRunnerDeps{
				Store:         pipelineStore,
				Jobs:          pipelineJobStore,
				DeployTargets: deployTargetStore,
				Repos:         repositorySvc,
				Tasks:         repositorySvc,
				QA:            qaDispatcher,
				Git:           gitClient,
				Tokens:        tokenSource,
				WorkspaceRoot: cfg.Storage.Sessions.WorkspaceRoot,
				TaskPRs:       boardTaskStore,
			})
			pipelineRunner.SetWorkflows(workflowSvc)
			if boardDispatcher != nil {
				boardDispatcher.SetPipelineGate(true)
			}
			pipelineRunner.SetStageVerifier(repositorySvc)
			// Lets a finished prod deploy re-read the task and post its
			// after_deploy steps against the current text, not the snapshot the
			// pipeline was queued with.
			pipelineRunner.SetTaskReader(repositorySvc)
			if modelSvc != nil {
				pipelineRunner.SetComponentPaths(componentPathReader{model: modelSvc})
			}
			// Same-commit re-bounce brake. Without it a permanently red check —
			// a build the branch cannot fix, an Actions account with no credit —
			// bounces the card to need_revision on every lap of the review
			// cycle, and each lap re-triggers the identical pipeline.
			bounceGuard := boardapp.NewPipelineBounceGuard(pipelineStore, repositorySvc, boardTaskStore)
			bounceGuard.SetParkJournal(boardapp.NewParkJournal(boardEventStore, taskSpanStore))
			// The human-touch reset: a person who has been shown the red build
			// gets told about the next one. Same board history the review-loop
			// cap reads, and the same reset, so the two guards agree.
			bounceGuard.SetEventHistory(boardEventStore)
			// The task on a pipeline job is a snapshot from when it was queued.
			// A pipeline settling half an hour later must not park a card a
			// human has since moved, nor overwrite a quota/device/work-order
			// park with human_decision — which no sweeper releases.
			bounceGuard.SetTaskReader(repositorySvc)
			pipelineRunner.SetBounceGuard(bounceGuard)
			repositorySvc.SetPipelineRunner(pipelineRunner)
			repositorySvc.SetPipelineStore(pipelineStore)
			// Delivery dedupe that outlives the process: GitHub retries to whichever
			// replica the load balancer picks, so an in-memory map only answers
			// for the pod that got the first attempt.
			repositorySvc.SetDeliveryLedger(pgstore.NewWebhookDeliveryStore(e.pgDB))
			repositorySvc.SetPipelineJobStore(pipelineJobStore)
			if githubTokens != nil {
				repositorySvc.SetGitHubTokenSource(githubTokens.GitHubToken)
			}
			// Two boot-time convergence passes, backgrounded on one goroutine —
			// both are GitHub round trips per repo, neither on any request path.
			// webhook_reconcile: repos registered before webhook support get a
			// hook installed (all existing hooks were push-only; that is why the
			// code-review gate had no signal). index_freshness: catch-up for
			// pushes that landed while this pod was down, which GitHub does not
			// redeliver.
			go func() {
				bootCtx, cancel := context.WithTimeout(ctx, bootConvergeTimeout)
				defer cancel()
				repositorySvc.ReconcileWebhooks(bootCtx)
				repositorySvc.SweepIndexFreshness(bootCtx)
				repositorySvc.ResumeUnfinishedIndexes(bootCtx)
			}()
			// GitHub will not deliver webhooks to loopback, and a desktop install
			// listens on 127.0.0.1 — poll instead; the sweep reindexes a clone
			// whose default branch moved, throttled per repo.
			if !repositorySvc.WebhooksReachable() {
				go func() {
					t := time.NewTicker(localPushPollInterval)
					defer t.Stop()
					for {
						select {
						case <-ctx.Done():
							return
						case <-t.C:
							repositorySvc.SweepIndexFreshness(ctx)
						}
					}
				}()
			}
			if catalogStore != nil {
				repositorySvc.SetAgentLister(catalogStore.ListAgents)
			}
			pipelineRunner.Start(ctx)
			e.pipelineRunner = pipelineRunner

			// The belt to the in-process poll's braces: the poll dies with its pod and
			// its queue is in memory, so this reads the rows and opens the
			// code-review gate with a reason instead of leaving a card wedged
			// behind a signal that never comes.
			boardapp.NewPipelineGateSweeper(pipelineStore, pipelineRunner, cfg.Board.PipelineGateTimeout).
				Start(ctx, cfg.Board.PipelineGateInterval)

			// Deploy definitions and production incidents, both authored on the board;
			// wired after the runner so it can report failed deploys as incidents.
			deploySvc := deploy.NewService(deployTargetStore, repositoryStore)
			if workflowSvc != nil {
				deploySvc.SetWorkflows(workflowSvc)
				deploySvc.SetRoleResolver(workflowSvc)
			}
			deploySvc.SetTaskCreator(repositorySvc)
			repositorySvc.SetDeployTargets(deployTargetStore)
			e.deploySvc = deploySvc

			// Reference docs (coding/test standards, architecture): the same "a human
			// names where it lives, an agent can be asked to write it" idea as
			// deploySvc, so it shares the TaskCreator/role-resolver wiring.
			repoDocsSvc := repodocs.NewService(repositorySvc)
			if workflowSvc != nil {
				repoDocsSvc.SetWorkflows(workflowSvc)
				repoDocsSvc.SetRoleResolver(workflowSvc)
			}
			repoDocsSvc.SetTaskCreator(repositorySvc)
			// The docs bundle lands as one PR, merged through the same use case and
			// refusals as any other task's. nil without GitHub.
			if taskPRSvc != nil {
				repoDocsSvc.SetTaskPRMerger(taskPRSvc)
			}
			e.repoDocsSvc = repoDocsSvc

			// Cloud accounts, environments and runtime: which provider account backs
			// each component's environment, and the live picture (deployments, logs,
			// errors) read back through the matching provider. The two secret-bearing
			// stores get the boot-time cipher exactly like pgSettings/mobileStore
			// above and below — their first real use is a later HTTP request, after
			// scrubProcessSecrets has already wiped MCP_SECRETS_KEY from the env.
			cloudAccountStore := pgstore.NewCloudAccountStore(e.pgDB)
			cloudAccountStore.SetCipher(e.secretsCipher, e.secretsCipherErr)
			environmentStore := pgstore.NewEnvironmentStore(e.pgDB)
			legacyCloudSource := pgstore.NewLegacyCloudSourceStore(e.pgDB)
			legacyCloudSource.SetCipher(e.secretsCipher, e.secretsCipherErr)
			cloudComponents := pgstore.NewProjectModelStore(e.pgDB)

			vercelClient := vercelapi.New()
			cloudSvc := cloudapp.NewService(cloudapp.Deps{
				Accounts:     cloudAccountStore,
				Environments: environmentStore,
				Providers: []port.CloudProvider{
					vercelapi.NewProvider(vercelClient),
					gcloud.NewProvider(),
					awsprovider.NewProvider(),
				},
				Components:    cloudComponents,
				Scans:         cloudComponents,
				Repos:         repositoryStore,
				DeployTargets: deployTargetStore,
				Tasks:         repositorySvc,
				Legacy:        legacyCloudSource,
			})
			cloudSvc.SetBackgroundContext(ctx)
			// Background health sweep of every bound environment, and the one-time
			// carry-forward of a pre-cloud-accounts Vercel/GCloud connection and its
			// bindings (cloud.Service.Boot's own doc comment covers idempotency).
			cloudSvc.Start(ctx)
			cloudSvc.Boot(ctx)
			e.cloudSvc = cloudSvc
			if modelSvc != nil {
				// A finished scan turns its deploy signals into environment rows
				// through the same service the runtime routes read, and the model
				// reads environments back for RepositoryModel/RepositorySummary.
				modelSvc.SetDeployMatcher(cloudSvc)
				modelSvc.SetEnvironmentReader(environmentStore)
				cloudSvc.SetDeliveryRefresher(modelSvc)
				// A confirmed environment binding can resolve another repository's
				// dangling link target, the same relationship in reverse.
				cloudSvc.SetRelinker(modelSvc)
			}
			for _, tool := range runtimetools.NewExecutors(&runtimetools.ToolKit{Components: cloudComponents, Cloud: cloudSvc}) {
				e.reg.Register(tool)
			}

			// Real-device tools, registered here because their guard IS the deploy
			// target store, which does not exist until this point. No enable
			// flag: an install with no device registers no tools, rather than
			// handing agents a mobile_tap that answers "no device configured".
			mobileStore := pgstore.NewMobileDeviceStore(e.pgDB)
			mobileStore.SetCipher(e.secretsCipher, e.secretsCipherErr)
			envDevice := domain.MobileDevice{
				HubURL:          cfg.Tools.Mobile.HubURL,
				DeviceUDID:      cfg.Tools.Mobile.DeviceUDID,
				PlatformVersion: cfg.Tools.Mobile.PlatformVersion,
				DevicePIN:       cfg.Tools.Mobile.DevicePIN,
				HubToken:        cfg.Tools.Mobile.AuthToken,
			}
			mobileDeviceSvc := mobiledevice.NewService(
				mobileStore,
				// nil when MOBILE_BRIDGE_URL is unset (the normal case for simulators):
				// its absence must not disable anything else — it serves
				// remote_adb only, and registration is driven by effective devices.
				deviceagent.New(cfg.Tools.Mobile.BridgeURL, cfg.Tools.Mobile.BridgeToken),
				envDevice,
			)
			// What this machine itself can drive, detected rather than
			// configured; New() never fails, a host with no Xcode/SDK reports none.
			mobileDeviceSvc.SetLocalHost(localdevice.New(localdevice.Config{}))
			e.mobileDeviceSvc = mobileDeviceSvc

			// The pool exists even with no phone attached, so a device registered
			// later in the UI swaps in without a restart. What is gated is the
			// TOOLS — an absent matching tool says for free what a present one
			// would spend a turn on saying.
			if e.mobilePool == nil {
				e.mobilePool = mobiletools.NewPool()
			}
			mobileDeviceSvc.SetProbe(e.mobilePool)
			mobileTools := mobiletools.NewExecutorsFor(e.mobilePool, deployAppResolver{targets: deployTargetStore})
			var mobileToolsOnce sync.Once
			registerMobileTools := func() {
				mobileToolsOnce.Do(func() {
					for _, tool := range mobileTools {
						e.reg.Register(tool)
					}
					log.Info().Msg("mobile device tools enabled")
				})
			}

			// A callback rather than a config read so a live phone add/remove is
			// retargeted in place. Registration is one-way: after the last phone
			// the tools stay and explicitly fail — truthful "the phone is gone".
			mobileDeviceSvc.SetReloader(func(_ context.Context, devices []domain.MobileDevice) error {
				e.mobilePool.Reconfigure(mobileConfigsOf(devices))
				if len(devices) > 0 {
					registerMobileTools()
				}
				return nil
			})

			// Whatever is registered, or failing that in the environment, is what agents
			// drive from the first run — read as a boot step after the board seed.
			e.bootSeed.AddStep("mobile_devices", func(stepCtx context.Context) error {
				devices, source, derr := mobileDeviceSvc.Effective(stepCtx)
				if derr != nil {
					return derr
				}
				if len(devices) == 0 {
					return nil
				}
				e.mobilePool.Reconfigure(mobileConfigsOf(devices))
				registerMobileTools()
				log.Info().Int("devices", len(devices)).Str("source", source).Msg("mobile devices attached")
				return nil
			})

			// The release half of the device lease: a task parked on a taken phone has
			// nothing to wake it, so the sweep is the only way back. Started
			// unconditionally so a later-attached device needs no restart, and a
			// sweep over nothing parked is a probe that returns immediately.
			if boardTaskStore != nil && boardDispatcher != nil {
				sweeper := boardapp.NewDeviceSweeper(boardTaskStore, e.mobilePool, boardDispatcher)
				activateBoard = append(activateBoard, func() {
					sweeper.Start(ctx, boardapp.DeviceSweeperInterval)
				})
			}

			// Task checkouts are hundreds of megabytes each and nothing ever removed
			// one, so the volume filled and every build on it failed with ENOSPC
			// — in tasks unrelated to whoever held the space. Deleting a task
			// takes its directory; this collects the rest once they have been
			// finished long enough that nobody is about to drag the card back.
			if boardTaskStore != nil && cfg.Storage.Sessions.WorkspaceRoot != "" {
				// The run STORE is the in-use probe, not the local runner: a
				// checkout mid-commit on another replica looks idle to this
				// host's map, and the reaper would delete it.
				reaper := boardapp.NewWorkspaceReaper(
					boardTaskStore,
					taskAgentRunStore,
					cfg.Storage.Sessions.WorkspaceRoot,
					boardapp.WorkspaceReapGrace,
				)
				activateBoard = append(activateBoard, func() {
					reaper.Start(ctx, boardapp.WorkspaceReaperInterval)
				})
			}

			incidentStore := pgstore.NewIncidentStore(e.pgDB)
			prodOpsDeps := prodops.Deps{
				Incidents: incidentStore,
				Targets:   deployTargetStore,
				Repos:     repositoryStore,
				Tasks:     repositorySvc,
				Deploys:   pipelineStore,
			}
			prodOpsSvc := prodops.NewService(prodOpsDeps)
			if workflowSvc != nil {
				prodOpsSvc.SetWorkflows(workflowSvc)
				prodOpsSvc.SetRoleResolver(workflowSvc)
			}
			e.prodOpsSvc = prodOpsSvc
			pipelineRunner.SetIncidentReporter(prodOpsSvc)

			for _, tool := range opstools.NewExecutors(&opstools.ToolKit{
				Incidents: prodOpsSvc,
				Deploys:   deploySvc,
			}) {
				e.reg.Register(tool)
			}

			if cfg.ProdOps.MonitorEnabled {
				monitor := prodops.NewMonitor(deployTargetStore, prodOpsSvc)
				monitor.Start(ctx, cfg.ProdOps.ProbeInterval)
				e.healthMonitor = monitor
			}

			// Store console credential vault + mobile app registry, wired
			// unconditionally so a store deploy target always reads the real
			// onboarding state instead of silently no-op'ing.
			storeCredentialStore := pgstore.NewStoreCredentialStore(e.pgDB)
			storeAppStore := pgstore.NewMobileStoreAppStore(e.pgDB)
			storeSigningStore := pgstore.NewSigningAssetStore(e.pgDB)

			// A missing cipher must not stop boot — same graceful-degrade as
			// the mcp/llmprovider ciphers: only the credential-vault paths that
			// actually encrypt/decrypt fail until the key is set.
			if e.secretsCipherErr != nil {
				log.Warn().Err(e.secretsCipherErr).Msg("storeops secrets cipher unavailable, store credential vault disabled")
			}

			storeOpsSvc := storeops.NewService(storeops.Deps{
				Credentials: storeCredentialStore,
				Apps:        storeAppStore,
				Signing:     storeSigningStore,
				Cipher:      e.secretsCipher,
				NewASC: func(cred domain.StoreCredential) (port.AppStoreClient, error) {
					return appstore.New(cred)
				},
				NewPlay: func(cred domain.StoreCredential) (port.GooglePlayClient, error) {
					return googleplay.New(cred)
				},
				// PushSecret resolves owner/repo/token the way the pipeline's dispatch does,
				// adapted from a task workspace to a bare repo ID: the repo's own
				// RootPath stands in for the task workspace dir.
				PushSecret: func(ctx context.Context, repositoryID uuid.UUID, name, value string) error {
					if gitClient == nil || githubTokens == nil {
						return fmt.Errorf("storeops: push secret: git client or github token not configured")
					}
					repo, err := repositoryStore.Get(ctx, repositoryID)
					if err != nil {
						return fmt.Errorf("storeops: push secret: resolving repository: %w", err)
					}
					token, err := githubTokens.GitHubToken(ctx)
					if err != nil {
						return fmt.Errorf("storeops: push secret: resolving github token: %w", err)
					}
					info, err := gitClient.TaskGitInfo(ctx, repo.RootPath)
					if err != nil {
						return fmt.Errorf("storeops: push secret: resolving repository git info: %w", err)
					}
					return githubapi.PutRepoSecret(ctx, token, info.Owner, info.Repo, name, value)
				},
				Repos:    repositoryStore,
				Tasks:    repositorySvc,
				Comments: repositorySvc,
			})
			e.storeOpsSvc = storeOpsSvc
			// ops_audit_log sink: every store release action is recorded,
			// success or failure. Unconditional — writing an audit row never
			// depends on the cipher or a credential being present.
			storeOpsSvc.SetAuditor(pgstore.NewOpsAuditStore(e.pgDB))

			// Engine probes are closures rather than port interfaces because they exist
			// only to let the application ask an adapter a yes/no it must not
			// import to ask. The Actions probe looks for the EXACT file the
			// dispatch will ask for — prefix matching was wrong: a monorepo
			// renders mobile-release-<sub>.yml and a stale mobile-release.yml
			// answered yes before GitHub 404s on the dispatch.
			storeOpsSvc.SetEngineProbes(
				func(ctx context.Context, repo domain.Repository, _, workflowFile string) error {
					if gitClient == nil || githubTokens == nil {
						return fmt.Errorf("storeops: github client or token not configured")
					}
					token, err := githubTokens.GitHubToken(ctx)
					if err != nil {
						return fmt.Errorf("storeops: resolving github token: %w", err)
					}
					info, err := gitClient.TaskGitInfo(ctx, repo.RootPath)
					if err != nil {
						return fmt.Errorf("storeops: resolving repository git info: %w", err)
					}
					jobs, err := githubapi.ParseWorkflowJobs(ctx, token, info.Owner, info.Repo)
					if err != nil {
						return err
					}
					for _, job := range jobs {
						if workflowFile != "" && job.WorkflowFile == workflowFile {
							return nil
						}
					}
					return storeops.ErrActionsNoWorkflow
				},
				func(ctx context.Context) (storeops.LocalRunnerHost, error) {
					// The host IS this machine; the Mac test reads its own
					// reported capability.
					return storeops.LocalRunnerHost{
						Paired: true,
						MacOS:  localdevice.New(localdevice.Config{}).SupportsIOSSimulators(ctx),
					}, nil
				},
			)
			if boardTaskStore != nil {
				storeOpsSvc.SetReleaseParker(boardTaskStore)
			}
			storeOpsSvc.SetReleaseStarter(func(ctx context.Context, repo domain.Repository, app domain.MobileStoreApp, engine string, artifacts []pipeline.Artifact) error {
				if gitClient == nil || githubTokens == nil {
					return fmt.Errorf("storeops: github client or token not configured")
				}
				token, err := githubTokens.GitHubToken(ctx)
				if err != nil {
					return fmt.Errorf("storeops: resolving github token: %w", err)
				}
				info, err := gitClient.TaskGitInfo(ctx, repo.RootPath)
				if err != nil {
					return fmt.Errorf("storeops: resolving repository git info: %w", err)
				}

				// The files land BEFORE anything is started, for both engines — they ARE
				// the release procedure (Actions dispatches the workflow by name,
				// a local run executes the script) — and they are COMMITTED to
				// the repository, not the mirror: CommitAndPush stages the whole
				// tree, and the mirror is disposable, so strays there would ride
				// along into the user's default branch.
				files := make([]githubapi.FileChange, 0, len(artifacts))
				for _, artifact := range artifacts {
					files = append(files, githubapi.FileChange{Path: artifact.Path, Body: artifact.Body, Mode: artifact.Mode})
				}
				if _, _, err := githubapi.CommitFiles(ctx, token, info.Owner, info.Repo, info.Branch,
					fmt.Sprintf("chore(release): generate the %s release pipeline for %s", app.Platform, app.Identifier),
					files); err != nil {
					return fmt.Errorf("storeops: writing the generated release pipeline to %s: %w", info.Branch, err)
				}

				if engine != domain.ReleaseEngineActions {
					// The local engine is not wired here: it needs a checkout
					// path (a workspace.prepare round-trip) and a member whose
					// machine runs it, so there is no honest value to return.
					// ErrNoReleaseEngine is what makes the board park the card.
					return fmt.Errorf(
						"storeops: the local release engine is not wired on this deployment: %w", domain.ErrNoReleaseEngine)
				}
				workflow := ""
				for _, artifact := range artifacts {
					if strings.HasPrefix(artifact.Path, workflowDir) {
						workflow = strings.TrimPrefix(artifact.Path, workflowDir)
					}
				}
				if workflow == "" {
					return fmt.Errorf("storeops: the generated release pipeline for %s carries no workflow", app.Identifier)
				}
				return githubapi.DispatchWorkflow(ctx, token, info.Owner, info.Repo, workflow, info.Branch)
			})
			// Onboard returns the (possibly test_ready) app row; the hook only needs
			// to know whether onboarding succeeded, so the row is dropped here.
			deploySvc.SetStoreOnboarder(func(ctx context.Context, repositoryID uuid.UUID, provider, identifier, appName string) error {
				_, err := storeOpsSvc.Onboard(ctx, repositoryID, provider, identifier, appName)
				return err
			})
			// Runs BEFORE the target is written, unlike the onboarder whose
			// failures SaveTarget tolerates: a re-pointed live app must fail
			// the save outright.
			deploySvc.SetStoreIdentifierGuard(storeOpsSvc.EnsureIdentifierAllowed)
			repositorySvc.SetMobileStoreApps(storeAppStore)
			// A successful store prod deploy IS the submit for review — hand it
			// to storeops so the monitor polls the verdict.
			pipelineRunner.SetStoreSubmitter(storeOpsSvc)

			// Gated like the prodops monitor: without a cipher the vault cannot
			// build an ASC/Play client, so the sweep would only log warnings.
			if e.secretsCipher != nil {
				storeMonitor := storeops.NewMonitor(storeOpsSvc, storeAppStore, prodOpsSvc)
				storeMonitor.Start(ctx, cfg.Storeops.PollInterval)
				e.storeMonitor = storeMonitor
			}

			// Deploy operations console: mirrors Actions deploy runs locally,
			// attributes console-triggered dispatches back to their author, and
			// turns a failed deploy into an incident. Gated on the token STORE,
			// not a token: the API resolves the stored token per call, so an
			// unconnected install gets GitHub's own answer rather than a dead
			// capability.
			if githubTokens != nil {
				deployToken := githubapi.TokenSource(githubTokens.GitHubToken)
				deploymentRunStore := pgstore.NewDeploymentRunStore(e.pgDB)
				deployOpsSvc := deployops.New(
					deploymentRunStore,
					pgstore.NewDeployDispatchStore(e.pgDB),
					pgstore.NewOpsAuditStore(e.pgDB),
					deployTargetStore,
					repositoryStore,
					pipelineJobStore,
					githubapi.NewActionsAPIFor(deployToken),
				)
				// domain.Repository carries no GitHub owner/repo — resolve them from the
				// checkout's git origin, same source as pipeline.go and the
				// PushSecret closure above.
				resolveRepoCoordinates := func(ctx context.Context, repo domain.Repository) (string, string, error) {
					if gitClient == nil {
						return "", "", fmt.Errorf("deployops: git client not configured")
					}
					info, err := gitClient.TaskGitInfo(ctx, repo.RootPath)
					if err != nil {
						return "", "", fmt.Errorf("deployops: resolving repository git info: %w", err)
					}
					return info.Owner, info.Repo, nil
				}
				deployOpsSvc.SetRepoResolver(resolveRepoCoordinates)
				e.deployOpsSvc = deployOpsSvc

				// Break-glass recorder, registered here because the console service only
				// exists once a GitHub token resolved: an agent that deploys from
				// the machine while Actions is down reports through this tool so
				// "what is live where" keeps answering.
				for _, tool := range opstools.NewLocalDeployExecutors(deployOpsSvc) {
					e.reg.Register(tool)
				}

				if cfg.DeployOps.MonitorEnabled {
					deployMonitor := deployops.NewMonitor(deployOpsSvc, prodOpsSvc)
					deployMonitor.Start(ctx, cfg.DeployOps.PollInterval)
					e.deployMonitor = deployMonitor
				}

				// The deploy watch: what production did to the commit a task's merge
				// produced, and the rollback when the answer is bad. Wired here
				// because it needs the deployment-run store and resolved token,
				// which do not exist at registration time; the ToolKit is a
				// pointer read at tool-call time, so this assignment turns the
				// three already-registered tools on.
				deployWatchSvc := deploywatch.New(deploywatch.Deps{
					Tasks:           boardTaskStore,
					Comments:        repositorySvc,
					Targets:         deployTargetStore,
					Repos:           repositoryStore,
					Runs:            deploymentRunStore,
					Pipeline:        pipelineJobStore,
					Actions:         githubapi.NewActionsAPIFor(deployToken),
					Rollbacks:       deployOpsSvc,
					Git:             gitClient,
					Incidents:       prodOpsSvc,
					RepoCoordinates: resolveRepoCoordinates,
					HealthWindow:    cfg.DeployOps.HealthWindow,
				})
				e.deployWatchSvc = deployWatchSvc
				boardKit.DeployWatch = deployWatchSvc

				releaseDeps := releaseapp.Deps{
					Store:               pgstore.NewReleaseStore(e.pgDB),
					Tasks:               repositorySvc,
					ParkedTasks:         boardTaskStore,
					MergeState:          pgstore.NewBoardTaskStore(e.pgDB),
					LegacyTargets:       deployTargetStore,
					DeployStatus:        deployWatchSvc,
					Actions:             githubapi.NewActionsAPIFor(deployToken),
					Reverter:            gitClient,
					Repos:               repositorySvc,
					Incidents:           prodOpsSvc,
					RepoCoordinates:     resolveRepoCoordinates,
					IsRefAlreadyExists:  githubapi.IsRefAlreadyExists,
					IsCIUnavailableText: githubapi.IsCIUnavailableText,
					HealthWindow:        cfg.DeployOps.HealthWindow,
					BeforeDeploy:        repository.BeforeDeployConfirmer{Service: repositorySvc},
					Locator:             repositorySvc,
				}
				if relations, ok := relationStore.(*pgstore.TaskRelationStore); ok {
					releaseDeps.DeployOrder = relations
				}
				releaseDeps.Git = gitClient
				e.localRunner = localexec.NewRunner()
				releaseDeps.LocalRunner = e.localRunner
				releaseDeps.DataDir = opts.DataDir
				if releaseDeps.DataDir == "" {
					releaseDeps.DataDir = filepath.Dir(cfg.AgentCatalog.CacheDir)
				}
				if e.storeOpsSvc != nil {
					releaseDeps.StoreOps = e.storeOpsSvc
				}
				if modelSvc != nil {
					releaseDeps.Components = modelSvc
				}
				if e.cloudSvc != nil {
					releaseDeps.Environments = e.cloudSvc
				}
				if boardDispatcher != nil {
					releaseDeps.Waker = boardapp.NewReleaseWaker(boardDispatcher)
				}
				releaseSvc := releaseapp.New(releaseDeps)
				e.releaseSvc = releaseSvc
				boardKit.Releases = releaseSvc
				if taskPRSvc != nil {
					taskPRSvc.SetReleaseOpener(releaseSvc)
				}
				if modelSvc != nil {
					modelSvc.SetDeliveryConfirmedHook(func(ctx context.Context, repositoryID, componentID uuid.UUID) {
						if _, err := releaseSvc.OpenPending(ctx, repositoryID, componentID); err != nil {
							log.Warn().Err(err).Str("component_id", componentID.String()).Msg("release: opening releases for tasks waiting on delivery confirmation failed")
						}
					})
				}
				activateBoard = append(activateBoard, func() {
					releaseSvc.Start(ctx, releaseapp.DefaultSweepInterval)
				})

				// The park's release mechanism: without it a task parked on a running
				// deploy stays blocked forever — the tool that parked it is the
				// only thing that could unpark it, and it is not running.
				if boardDispatcher != nil {
					deploySweeper := boardapp.NewDeploySweeper(boardTaskStore, deployWatchSvc, boardDispatcher)
					activateBoard = append(activateBoard, func() {
						deploySweeper.Start(ctx, boardapp.DeploySweeperInterval)
					})

					// auto_rollback is real here: an incident in a release's health window is
					// attributed to the releasing task and its owner is woken to
					// roll back (or write the proposal when the flag is off).
					prodOpsSvc.SetReleaseAttributor(releaseAttributor{primary: releaseSvc, fallback: deployWatchSvc})
					prodOpsSvc.SetReleaseRollbackDispatcher(
						boardapp.NewReleaseRollbackDispatcher(boardDispatcher, repositorySvc))
				}
			}
		}
	}

	// Every board dependency is wired now — safe to let work start flowing.
	for _, activate := range activateBoard {
		activate()
	}

	if evolutionStore != nil && catalogSvc != nil && catalogStore != nil && perfStore != nil && taskAgentRunStore != nil {
		e.evolutionSvc = evolution.NewService(evolution.Deps{
			Store:     evolutionStore,
			Board:     boardConfigStore,
			Catalog:   catalogStore,
			Manager:   catalogSvc,
			Memories:  memorySvc,
			KPIs:      kpiSvc,
			Perf:      perfStore,
			Sessions:  sessionStore,
			Runs:      taskAgentRunStore,
			Comments:  commentStore,
			Golden:    goldenStore,
			LLM:       llmClient,
			AgentLoop: e.agentRouter,
			Config:    cfg.Evolution,
		})
		if repositorySvc != nil {
			repositorySvc.SetEvolution(e.evolutionSvc)
		}
		// save_memory gets first refusal on reusable know-how — the evolution
		// service classifies it and writes a skill instead of a memory.
		if memoryToolKit != nil {
			memoryToolKit.SetPromoter(e.evolutionSvc)
		}
		e.evolutionSvc.Start(ctx)
	}
	if initiativeStore != nil {
		initiativeSvc = initiative.NewService(initiativeStore)
	}

	// External agents/skills catalog: a git repo (or local dir) the app pulls
	// definitions from. catalogRepo/catalogSyncStoreForHandler feed the
	// Handler Config below — without them /v1/catalog/* always answers "not
	// configured" even though the sync below is running fine. The boot-time
	// sync itself is wired further down, as a boot step sequenced after
	// embeddings_endpoint: a fresh install's first sync creates skills that
	// need embedding, and running it any earlier fails every skill with
	// "unsupported protocol scheme" on the not-yet-configured embedder.
	var catalogRepo port.CatalogRepoReader
	var catalogSyncStoreForHandler port.CatalogSyncStore
	var catalogSyncOnce func(context.Context)
	if strings.TrimSpace(cfg.AgentCatalog.Source) != "" && e.pgDB != nil && catalogSvc != nil {
		catalogSyncStore := pgstore.NewCatalogSyncStore(e.pgDB)
		reader := &catalogrepo.Reader{Source: cfg.AgentCatalog.Source, CacheDir: cfg.AgentCatalog.CacheDir}
		catalogSyncStoreForHandler = catalogSyncStore
		catalogRepo = reader
		catalogSyncOnce = func(runCtx context.Context) {
			syncCtx, cancel := context.WithTimeout(runCtx, 10*time.Minute)
			defer cancel()
			res, err := catalogSvc.SyncFromCatalog(syncCtx, reader, catalogSyncStore)
			if err != nil {
				log.Warn().Err(err).Msg("agent catalog sync failed")
				return
			}
			log.Info().Str("ref", res.RepoRef).
				Int("created", res.Created).Int("updated", res.Updated).
				Int("merged", res.Merged).Int("pending", res.Pending).
				Msg("agent catalog synced")
		}
	}

	var settingsSvc *settings.Service
	if settingsStore != nil {
		settingsSvc = settings.NewService(settingsStore)
		if workflowSvc != nil {
			settingsSvc.SetWorkflows(workflowSvc)
			settingsSvc.SetRoleResolver(workflowSvc)
		}
		if catalogSvc != nil {
			settingsSvc.SetAgentCatalog(catalogSvc)
			if workflowSvc != nil {
				workflowSvc.SetAgentCatalog(catalogSvc)
			}
		}
	}

	var llmProviderSvc *llmprovider.Service
	if llmProviderStore != nil {
		if e.secretsCipherErr != nil {
			log.Warn().Err(e.secretsCipherErr).Msg("llm secrets cipher unavailable, api key storage disabled")
		}
		llmTimeout := cfg.LLM.Timeout
		if llmTimeout <= 0 {
			llmTimeout = 300 * time.Second
		}
		// The provider cache and the resolver the LLM client asks every call;
		// constructed before the service so it gets the invalidation hook, and
		// installed on the client immediately — from that moment the client
		// answers from stored settings instead of the config.yml fallback.
		providers := newProviderCache(func(rctx context.Context) (llmprovider.Resolved, error) {
			return llmProviderSvc.Resolve(rctx)
		}, llmTimeout)
		llmProviderSvc = llmprovider.NewService(llmProviderStore, llmEndpointStore, e.secretsCipher, llmTimeout, providers.Invalidate)
		if e.multiLLM != nil {
			e.multiLLM.SetResolver(providers)
		}
		if catalogSvc != nil && agentCLISvc != nil {
			llmProviderSvc.SetAfterChange(func(changeCtx context.Context) {
				connected, err := agentCLISvc.ConnectedProviders(changeCtx)
				if err != nil {
					return
				}
				if _, err := catalogSvc.ReconcileAgentRuntimes(changeCtx, connected); err != nil {
					log.Warn().Err(err).Msg("llm provider: reconciling agent runtimes failed")
				}
			})
		}
		// The operator and the user are the same person, so config.yml's
		// llm.base_url and the API key env vars are that person's own
		// credentials; seeding them saves a trip to the settings page. A boot
		// step, after the board seed wrote the provider list.
		e.bootSeed.AddStep("llm_providers", func(stepCtx context.Context) error {
			if err := llmProviderSvc.BootstrapFromYAML(stepCtx, cfg.LLM); err != nil {
				return err
			}
			llmProviderSvc.BootstrapFromEnv(stepCtx)
			return nil
		})
		if opts.EmbeddingsBaseURL != "" {
			embeddingsBaseURL := opts.EmbeddingsBaseURL
			e.bootSeed.AddStep("embeddings_endpoint", func(stepCtx context.Context) error {
				return llmProviderSvc.BootstrapEmbeddings(stepCtx, embeddingsBaseURL)
			})
			log.Info().Str("embeddings_base_url", embeddingsBaseURL).
				Msg("embeddings resolve to the bundled local embedder")
		}

		// What an index pass stamps itself with, and what a search compares
		// against. One call, not two: the indexer forwards the resolver to its
		// store, so wiring only the search guard would look exactly like the
		// guard working until a stale index is searched.
		if indexSvc != nil {
			indexSvc.SetEmbeddingResolver(llmProviderSvc)
		}

	}

	// The boot-time catalog sync, sequenced as a boot step so it starts only
	// after embeddings_endpoint above has run (steps run in registration
	// order). It launches onto its own goroutine, on the long-lived ctx
	// rather than the step's, and the step returns immediately: a full first
	// sync (skill embeds for every agent) easily outlasts bootSeed's shared
	// stepTimeout, which is a budget for quick steps, not this one. The UI's
	// manual sync button shares SyncFromCatalog and its mutex with both this
	// and the recurring tick below, so the three never interleave.
	if catalogSyncOnce != nil {
		e.bootSeed.AddStep("agent_catalog_sync", func(context.Context) error {
			go catalogSyncOnce(ctx)
			return nil
		})
		go func(runCtx context.Context) {
			ticker := time.NewTicker(cfg.AgentCatalog.Interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					catalogSyncOnce(runCtx)
				case <-runCtx.Done():
					return
				}
			}
		}(ctx)
	}

	var sessionSvc *session.Service
	if sessionStore != nil {
		// @-mention resolution needs the workspace roster; without both stores it
		// falls back to agents only.
		var sessionWorkspace session.WorkspaceLister
		if initiativeStore != nil && repositoryStore != nil {
			sessionWorkspace = workspaceLister{projects: initiativeStore, repos: repositoryStore}
		}
		sessionSvc = session.NewService(sessionStore, activityStore, e.agentLoop, orchSvc, settingsSvc, cfg.Storage.Sessions.TTL, ragSvc, &session.Deps{
			IndexInjector:      indexInjector,
			RepositoryResolver: repositorySvc,
			Board:              boardConfigStore,
			Catalog:            catalogStore,
			Budget:             budget,
			Summarizer:         summarizer,
			TitleGenerator:     titleGen,
			ContextCfg:         cfg.Context,
			IndexerCfg:         cfg.Indexer,
			MappingCfg:         cfg.Mapping,
			Memories:           memoryStore,
			KPIs:               kpiStore,
			Actions:            sessionActionStore,
			Workspace:          sessionWorkspace,
		})
		// Loop closer for human-in-the-loop: a question parks the task on the
		// clarification chat and answering re-dispatches it; repositorySvc
		// records the answer on the task so later runs read what was settled.
		if boardTaskStore != nil && boardDispatcher != nil {
			sessionSvc.SetAnswerResumer(boardapp.NewAnswerResumer(boardTaskStore, boardDispatcher, repositorySvc))
		}
		if attachmentSvc != nil {
			// Guarded so a nil *Service never lands in the interface as a
			// typed non-nil.
			sessionSvc.SetAttachments(attachmentSvc)
		}
		if taskChatWorkspaces != nil {
			// A chat bound to a board task works in that task's branch
			// checkout instead of the shared mirror clone, so what the agent
			// changes reaches the task's pull request.
			sessionSvc.SetTaskWorkspaces(taskChatWorkspaces)
		}
		if hostChatExecutor != nil {
			// Chat with a host-executed provider goes to the local CLI, exactly
			// as its board tasks already do; without this the turn fell to the
			// HTTP client, which has no base URL for a binary provider. Guarded
			// because a typed-nil executor in a non-nil interface would be
			// non-nil and non-Supporting at once.
			sessionSvc.SetChatExecutor(hostChatExecutor)

			// The chat's quota sweeper, started here so it never ticks before
			// SetChatExecutor ran — resuming a parked turn goes through the
			// same executor. Guarded as the board's sweeper is: with no CLI, a
			// chat cannot park on its quota in the first place.
			session.NewSessionQuotaSweeper(sessionStore, sessionSvc).Start(ctx, session.QuotaSweeperInterval)
		}
	}

	var apiKeySvc *apikey.Service
	if apiKeyStore != nil && opts.DataDir == "" {
		apiKeySvc = apikey.NewService(apiKeyStore)
	}

	if jobStore != nil {
		e.jobSvc = job.NewService(jobStore, e.agentLoop, ragSvc, cfg.Jobs.MaxConcurrent, cfg.Jobs.Timeout, cfg.Tools.DefaultPolicy)
		e.jobSvc.Start(ctx)
	}

	// The embedding map reads the chunk tables the indexer writes; it needs no
	// LLM and no indexer config, only Postgres.
	var embedMapSvc *embedmap.Service
	if embedMapStore != nil {
		embedMapSvc = embedmap.New(embedMapStore)
		// So the map can LABEL a source built with a different embedding model
		// rather than draw it as comparable — deliberately a label, not a
		// refusal: PCA drops rows whose length does not match, so a model
		// change would quietly re-derive the cloud's axes from the survivors
		// and a person could not tell.
		if llmProviderSvc != nil {
			embedMapSvc.SetEmbeddingResolver(llmProviderSvc)
		}
	}

	var releaseHTTP httpadapter.ReleaseService
	var releaseWaker httpadapter.ReleaseWaker
	if e.releaseSvc != nil {
		releaseHTTP = e.releaseSvc
		releaseWaker = e.releaseSvc
	}
	handler := httpadapter.NewHandler(httpadapter.Config{
		AgentLoop:         e.agentLoop,
		LLMClient:         llmClient,
		Registry:          toolReg,
		DefaultPolicy:     cfg.Tools.DefaultPolicy,
		LegacyAPIKey:      cfg.Server.APIKey,
		APIKeys:           cfg.Server.APIKeys,
		APIKeySvc:         apiKeySvc,
		SessionSvc:        sessionSvc,
		SettingsSvc:       settingsSvc,
		GitHubTokens:      githubTokens,
		LLMProviderSvc:    llmProviderSvc,
		AgentCLISvc:       agentCLISvc,
		JobSvc:            e.jobSvc,
		RAGSvc:            ragSvc,
		AttachmentSvc:     attachmentSvc,
		MobileDeviceSvc:   e.mobileDeviceSvc,
		CatalogSvc:        catalogSvc,
		CatalogRepo:       catalogRepo,
		CatalogSyncStore:  catalogSyncStoreForHandler,
		MCPSvc:            mcpService,
		AuditStore:        auditStore,
		MCPManager:        e.mcpManager,
		ReloadFn:          e.reload,
		IndexSvc:          indexSvc,
		IndexAllowedRoots: cfg.Indexer.AllowedRoots,
		EmbedMapSvc:       embedMapSvc,
		RepositorySvc:     repositorySvc,
		DeploySvc:         e.deploySvc,
		RepoDocsSvc:       e.repoDocsSvc,
		ProdOpsSvc:        e.prodOpsSvc,
		StoreOpsSvc:       e.storeOpsSvc,
		DeployOpsSvc:      e.deployOpsSvc,
		ProjectModelSvc:   e.projectModelSvc,
		CloudSvc:          e.cloudSvc,
		ReleaseSvc:        releaseHTTP,
		ReleaseWaker:      releaseWaker,
		LocalPreviewSvc:   localPreviewSvc,
		InitiativeSvc:     initiativeSvc,
		WorkspaceSvc:      workspaceSvc,
		WorkflowSvc:       workflowSvc,
		BoardEvents:       boardEventStore,
		TaskRuns:          taskAgentRunStore,
		RunControl:        boardRunControl,
		TaskChat:          boardTaskChat,
		EvolutionSvc:      e.evolutionSvc,
		MemorySvc:         memorySvc,
		KPISvc:            kpiSvc,
		PerfStore:         perfStore,
		GoldenStore:       goldenStore,
		UsageStore:        usageStore,
		BillingSvc:        e.billingSvc,
		UIRoot:            opts.UIRoot,
		UIFS:              opts.UIFS,
		// MCPToolServer is nil unless the Claude Code executor registered, in which
		// case no /mcp route is mounted at all.
		MCPToolServer: e.mcpServer,
		BootSeed:      e.bootSeed,
	})
	if scoreTracker != nil {
		scoreTracker.OnScoreUpdated = handler.RecordAgentScore
	}
	return handler
}

// reloadMCP rebuilds the process-wide MCP manager and the browser it shares a
// lifetime with.
func (e *engine) reloadMCP(configs []domain.MCPServerConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.mcpManager != nil {
		e.mcpManager.Close()
	}
	// Reload also drops the shared browser; the next browser_* call lazily
	// starts a fresh one, so a stale chromium never outlives a config change.
	if e.browserSession != nil {
		e.browserSession.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	e.mcpManager = &mcpadapter.Manager{}
	e.mcpManager.LoadAndRegister(ctx, configs, e.reg)
	return nil
}

// reload re-reads config.yml. It is reached from POST /admin/reload.
func (e *engine) reload() error {
	log.Info().Msg("reloading config")
	cfg, err := appconfig.Load(e.configPath)
	if err != nil {
		return err
	}
	// The same overrides boot applies: skipping them let a reload overwrite the
	// values that only exist in the environment — the DSN and the API key.
	applyLocalOverrides(cfg, e.opts)
	e.mu.Lock()
	e.cfg = cfg
	e.mu.Unlock()

	llmTimeout := cfg.LLM.Timeout
	if llmTimeout <= 0 {
		llmTimeout = 300 * time.Second
	}
	e.bootstrapLLMFromYAML(cfg.LLM.BaseURL, cfg.LLM.Model, cfg.LLM.APIKey, llmTimeout)

	mcpConfigs := []domain.MCPServerConfig{}
	if cfg.Storage.Postgres.DSN != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		pgPool, err := pgstore.NewPool(ctx, cfg.Storage.Postgres.DSN, cfg.Storage.Postgres.MaxConns)
		if err == nil {
			mcpStore := pgstore.NewMCPStore(pgstore.NewDB(pgPool))
			// The boot-time cipher, not a fresh NewCipherFromEnv: reload runs long
			// after MCP_SECRETS_KEY left the environment, so re-deriving here
			// would silently drop every stored MCP secret from the configs.
			if e.secretsCipherErr != nil {
				log.Warn().Err(e.secretsCipherErr).Msg("mcp secrets cipher unavailable during reload")
			}
			mcpService := mcpsvc.NewService(mcpStore, e.secretsCipher, nil)
			if configs, resolveErr := mcpService.ResolvedConfigs(ctx); resolveErr == nil {
				mcpConfigs = configs
			}
			pgPool.Close()
		}
	}
	return e.reloadMCP(mcpConfigs)
}

func (e *engine) loadMCPAsync(configs []domain.MCPServerConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	log.Info().Int("count", len(configs)).Msg("connecting mcp servers in background")
	e.mcpManager.LoadAndRegister(ctx, configs, e.reg)
}

// deployAppResolver: which package a repository's build is for this env,
// and where the artifact is, from the deploy target a human configured.
// Read-only by construction — a guard an agent could write to would be a
// formality.
//
// mobileConfigsOf turns registrations into what the session pool drives.
// Devices with no UDID are dropped: the UDID is allocated by whoever
// attaches the device (bridge port or console port), so a registration
// without one has never been reached, and a session pointed at an empty
// udid is one Appium would satisfy with "whatever adb lists first" — a
// different device than the operator registered. A simulator is the
// exception: its UDID is known at registration.
func mobileConfigsOf(devices []domain.MobileDevice) []mobiletools.Config {
	out := make([]mobiletools.Config, 0, len(devices))
	for _, d := range devices {
		if !d.Configured() {
			continue
		}
		out = append(out, mobiletools.Config{
			HubURL: d.HubURL,
			// The kind rides along because the session's capability set switches on it —
			// without it a registered simulator would be driven with UiAutomator2
			// capabilities and fail at session creation.
			Kind:            d.DeviceKind(),
			DeviceUDID:      d.DeviceUDID,
			PlatformVersion: d.PlatformVersion,
			DevicePIN:       d.DevicePIN,
			AuthToken:       d.HubToken,
		})
	}
	return out
}

type deployAppResolver struct {
	targets *pgstore.DeployTargetStore
}

func (r deployAppResolver) ResolveApp(ctx context.Context, repositoryID uuid.UUID, env string) (string, string, error) {
	targets, err := r.targets.ListByRepository(ctx, repositoryID)
	if err != nil {
		return "", "", err
	}
	for _, target := range targets {
		if target.Env == env {
			return target.AppPackage, target.AppURL, nil
		}
	}
	return "", "", fmt.Errorf("no deploy target configured for env %s", env)
}

type muxExecutor struct {
	executors map[domain.LLMProviderType]port.TaskExecutor
}

func (m *muxExecutor) Supports(provider domain.LLMProviderType) bool {
	ex, ok := m.executors[provider]
	if !ok || ex == nil {
		return false
	}
	return ex.Supports(provider)
}

func (m *muxExecutor) Execute(ctx context.Context, req domain.TaskExecution) (domain.AgentResponse, error) {
	ex, ok := m.executors[req.Provider]
	if !ok || ex == nil {
		return domain.AgentResponse{}, fmt.Errorf("no host executor for provider %q", req.Provider)
	}
	return ex.Execute(ctx, req)
}

// ExecuteChat routes a chat turn by provider, the same map Execute reads.
// Not every stored executor answers chat, so the type assertion makes that
// difference explicit: a provider with an executor that cannot chat gets the
// same refusal a provider with none does, instead of a panic.
func (m *muxExecutor) ExecuteChat(ctx context.Context, req domain.ChatExecution, out port.ChatStream) (domain.ChatResult, error) {
	ex, ok := m.executors[req.Provider]
	if !ok || ex == nil {
		return domain.ChatResult{}, domain.ErrHostExecutedProvider(req.Provider)
	}
	chatEx, ok := ex.(port.ChatExecutor)
	if !ok {
		return domain.ChatResult{}, domain.ErrHostExecutedProvider(req.Provider)
	}
	return chatEx.ExecuteChat(ctx, req, out)
}
