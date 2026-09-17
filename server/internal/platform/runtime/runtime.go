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
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/agentcli/antigravity"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/agentcli/claudecode"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/agentcli/cursor"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/agentcli/opencode"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/appstore"
	desktopadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/desktop"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/deviceagent"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/gcloud"
	gitadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/git"
	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/github"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/googleplay"
	httpadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/http"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/llm"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/localdevice"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/localtoolchain"
	mcpadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/mcp"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/mcpserver"
	pgstore "github.com/makifbaysal/tasktrooper/server/internal/adapter/store/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/board"
	boilerplatetools "github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/boilerplate"
	browsertools "github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/browser"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/clarification"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/code"
	memorytools "github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/memory"
	mobiletools "github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/mobile"
	opstools "github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/ops"
	repoprofiletools "github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/repoprofile"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/search"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/shell"
	skilltools "github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/skill"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/web"
	vercelapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vercel"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agentcli"
	"github.com/makifbaysal/tasktrooper/server/internal/application/apikey"
	attachmentapp "github.com/makifbaysal/tasktrooper/server/internal/application/attachment"
	"github.com/makifbaysal/tasktrooper/server/internal/application/billing"
	boardapp "github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/bootseed"
	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/application/chunker"
	appconfig "github.com/makifbaysal/tasktrooper/server/internal/application/config"
	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/application/deploy"
	"github.com/makifbaysal/tasktrooper/server/internal/application/deployops"
	"github.com/makifbaysal/tasktrooper/server/internal/application/deploywatch"
	"github.com/makifbaysal/tasktrooper/server/internal/application/embedmap"
	"github.com/makifbaysal/tasktrooper/server/internal/application/evolution"
	"github.com/makifbaysal/tasktrooper/server/internal/application/gcloudops"
	hostingapp "github.com/makifbaysal/tasktrooper/server/internal/application/hosting"
	"github.com/makifbaysal/tasktrooper/server/internal/application/indexer"
	"github.com/makifbaysal/tasktrooper/server/internal/application/initiative"
	"github.com/makifbaysal/tasktrooper/server/internal/application/job"
	kpiapp "github.com/makifbaysal/tasktrooper/server/internal/application/kpi"
	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	mcpsvc "github.com/makifbaysal/tasktrooper/server/internal/application/mcp"
	memoryapp "github.com/makifbaysal/tasktrooper/server/internal/application/memory"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mobiledevice"
	"github.com/makifbaysal/tasktrooper/server/internal/application/orchestrator"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/application/rag"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/repodependency"
	"github.com/makifbaysal/tasktrooper/server/internal/application/repodocs"
	repoprofileapp "github.com/makifbaysal/tasktrooper/server/internal/application/repoprofile"
	"github.com/makifbaysal/tasktrooper/server/internal/application/repository"
	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/application/settings"
	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops/pipeline"
	usageapp "github.com/makifbaysal/tasktrooper/server/internal/application/usage"
	"github.com/makifbaysal/tasktrooper/server/internal/application/vercelops"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// workflowDir is where application/storeops/pipeline renders the mobile
// release workflow, because GitHub reads workflows nowhere else. The file's
// NAME carries the sub-project, so one monorepo's two apps do not overwrite
// each other — which is why nothing here matches on a name prefix: the probe
// and the dispatch must agree on one exact file.
const workflowDir = ".github/workflows/"

// workspaceLister adapts the project + repository stores to the board toolkit's
// WorkspaceLister so agents can list projects and code repositories.
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
	// APIKey is the bearer token every request must carry. It arrives in the
	// environment rather than config.yml so it can be scrubbed after boot, and
	// a config reload re-applies it from here for the same reason.
	APIKey string
	// CORSOrigins are the origins allowed to call this server. The UI is served
	// from app://tasktrooper in the desktop bundle and from the Vite dev server
	// in a checkout, so every call it makes is cross-origin.
	CORSOrigins []string
	// EmbeddingsBaseURL is an OpenAI-compatible host exposing /v1/embeddings.
	// Set, it is bootstrapped into an embedding provider at boot so RAG works
	// without anyone opening the settings page.
	EmbeddingsBaseURL string
	// PublicBaseURL is the origin a Claude Code session calls TaskTrooper's own
	// tools back on. Empty until the listener is bound — with PORT=0 nobody
	// knows the port before then — so Run fills it in, and a reload then
	// re-applies that value instead of the empty one config.yml expands to.
	PublicBaseURL string
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
	// opts is what the process was started with, kept so a config reload can
	// re-apply the same overrides and the same validation boot did. Written
	// once, in load, before anything reads it.
	opts            Options
	reg             port.ToolRegistry
	mcpManager      *mcpadapter.Manager
	browserSession  *browsertools.Session
	mobilePool      *mobiletools.Pool
	mobileDeviceSvc *mobiledevice.Service
	llmClient       port.LLMClient
	multiLLM        *llm.MultiProviderClient
	agentLoop       *agent.Loop
	// agentRouter is what every agentic consumer is handed instead of agentLoop.
	// It is the loop plus one decision — HTTP loop or host executor — made from
	// the provider the caller already passes. See agent.Router.
	//
	// agentLoop stays beside it because the loop's own setters (history budget,
	// summarizer, run token cap, screenshot archiver) are the loop's, not the
	// router's: the router forwards runs, it does not configure them.
	agentRouter       *agent.Router
	jobSvc            *job.Service
	boardRunner       *boardapp.Runner
	billingSvc        *billing.Service
	pipelineRunner    *boardapp.PipelineRunner
	deploySvc         *deploy.Service
	repoDocsSvc       *repodocs.Service
	hostingSvc        *hostingapp.Service
	repoDependencySvc *repodependency.Service
	vercelOpsSvc      *vercelops.Service
	gcloudOpsSvc      *gcloudops.Service
	prodOpsSvc        *prodops.Service
	healthMonitor     *prodops.Monitor
	storeOpsSvc       *storeops.Service
	storeMonitor      *storeops.Monitor
	deployOpsSvc      *deployops.Service
	deployWatchSvc    *deploywatch.Service
	deployMonitor     *deployops.Monitor
	evolutionSvc      *evolution.Service
	// pgPool is kept beside pgDB for the two jobs that are not row data:
	// closing the pool, and the pgvector bootstrap (CREATE EXTENSION plus the
	// index DDL below), which is schema work.
	// Everything else in the process reaches Postgres through pgDB.
	pgPool *pgxpool.Pool
	pgDB   *pgstore.DB
	// bootSeed seeds the default board once and runs the boot steps.
	bootSeed   *bootseed.Service
	pendingMCP []domain.MCPServerConfig
	// mcpServer and mcpEndpoint are the two halves of the per-run tool endpoint
	// the Claude Code CLI calls back on. mcpServer is nil unless the executor
	// was registered (see buildHandler): with no CLI on the host there is no
	// session to serve. mcpEndpoint is allocated and published by Run BEFORE
	// buildHandler, because buildHandler starts the board workers and they must
	// never see an unpublished address; see claudecode_mcp.go.
	mcpServer   *mcpserver.Server
	mcpEndpoint *mcpEndpoint
	// secretsCipher is derived from MCP_SECRETS_KEY (or SERVER_API_KEY) exactly
	// once, at boot, because the process environment stops carrying that key
	// immediately afterwards — see scrubProcessSecrets. Every consumer that used
	// to call secrets.NewCipherFromEnv for itself now reads these two fields,
	// including engine.reload, which runs long after boot from a request
	// goroutine. Written in Run before the listener exists, read-only after, so
	// the mutex above does not need to cover them.
	secretsCipher    *secrets.Cipher
	secretsCipherErr error
}

// initSecretsCipher derives the at-rest encryption key while the environment
// still holds it. The error is kept rather than returned: a missing key is a
// degraded mode, not a boot failure — desktop installs run without one, and
// only the credential-vault paths that actually encrypt or decrypt fail.
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
	// LISTENING address the desktop supervisor parses) and a log line landing
	// beside it would have to be told apart from it.
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
}

// flattenLegacyWorkspaces moves an install still on the per-tenant workspace
// layout to the flat one (see workspace.FlattenLegacyLayout). It opens its own
// short-lived pool because the long-lived one is created inside buildHandler,
// which starts workers as soon as it exists. Nothing here is fatal: a path that
// could not be moved or re-pointed is still re-anchored on read, and the next
// boot tries again.
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

// configuredPort is the port the HTTP listener will ask for: the Options
// override when there is one, the config's otherwise. 0 means "let the kernel
// pick", which is what desktop installs and every test get — and the reason the
// MCP endpoint's URL cannot be known before the listener exists.
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

	// Everything that needs a pod credential from the environment has to read it
	// before this line, because after it the environment no longer has one. load
	// has taken the DSN and the internal auth key into cfg; initSecretsCipher
	// takes MCP_SECRETS_KEY, which is otherwise re-read on every config reload.
	// Ordering matters more than it looks: buildHandler starts the job worker and
	// the board dispatcher, so an agent-run child process can exist from that
	// point on, and it must never exist while the secrets are still reachable.
	e.initSecretsCipher()
	scrubProcessSecrets()
	if err := denyProcEnvironReads(); err != nil {
		// Not fatal. The scrubs above still hold; this only means a child that
		// goes looking in /proc can still find what execve put there, which is
		// the state every build before this one shipped in.
		log.Warn().Err(err).Msg("could not make the process undumpable; /proc/<pid>/environ stays readable to same-uid processes")
	}

	runCtx, cancel := context.WithCancel(ctx)

	// The listener is opened BEFORE the handler is built, and that order is
	// load-bearing rather than tidy.
	//
	// buildHandler ends by activating the board: the workers start and the
	// reconciler sweeps immediately, so a claude_code run can be executing
	// before buildHandler has returned. That run needs the address of the MCP
	// tool endpoint, and until now the address could only be guessed from the
	// CONFIGURED port — which is 0 on precisely the hosts that can run a
	// claude_code agent at all. The cloud image ships no `claude` binary, so the
	// executor only ever registers on a desktop or runner install, and
	// applyDesktopOverrides sets Server.Port to 0 there: configuredPort returned
	// 0, the guard publish never fired, and the endpoint URL did not exist until
	// after every worker was already running. Binding first replaces the guess
	// with the port the kernel actually gave us, published before anything can
	// dispatch.
	//
	// Nothing serves on it yet — app.Listener starts accepting further down —
	// so a request that arrives in between waits in the accept backlog for the
	// few milliseconds it takes to register the routes. Waiting is the correct
	// outcome; being told the wrong port, or none, is not.
	portNum := configuredPort(e.cfg, opts)
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", portNum))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("listen: %w", err)
	}
	addr := listener.Addr().String()
	e.mcpEndpoint = &mcpEndpoint{}
	e.mcpEndpoint.publish(addr)

	// The one line on stdout. The desktop supervisor spawns this process and
	// reads the port back off it, because PORT=0 is how it avoids colliding
	// with whatever else the user is running. Printed before the routes are
	// registered so the supervisor can begin polling /health immediately; a
	// request that beats the registration waits in the accept backlog.
	fmt.Fprintf(os.Stdout, "LISTENING http://%s\n", addr)
	_ = os.Stdout.Sync()

	if e.cfg.Server.PublicBaseURL == "" {
		e.cfg.Server.PublicBaseURL = "http://" + addr
		e.opts.PublicBaseURL = e.cfg.Server.PublicBaseURL
	}

	// Before buildHandler: every store, service and worker it builds reads
	// workspace paths, and all of them must see the flat layout.
	flattenLegacyWorkspaces(runCtx, e.cfg)

	handler := e.buildHandler(runCtx, opts)
	// Seed now rather than on the first request, so the board exists and the
	// role agents are already being written when the desktop's first /health
	// answers and the window opens.
	if e.bootSeed != nil {
		if err := e.bootSeed.Ensure(context.Background()); err != nil {
			log.Warn().Err(err).Msg("seeding the board at boot failed; the first request retries it")
		}
	}

	log.Info().Str("addr", addr).Msg("listening")
	if e.mcpServer != nil {
		log.Info().Str("mcp_endpoint", e.mcpEndpoint.get()).Msg("tasktrooper tools reachable over mcp for claude code sessions")
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
		// Off (the default) the router matches a lowercased path while c.Path()
		// returns the raw one, so `/V1/settings` reached the settings handler
		// while isPublicPath read a path that matched neither its `/v1` nor its
		// `/admin` rule — an unauthenticated request onto a configuration
		// endpoint. On, the router refuses the spelling outright. StrictRouting stays off: a
		// trailing slash never desynced anything, and turning it on would break
		// clients that send one.
		CaseSensitive: true,
		// stdout carries one machine-read line and nothing else; Fiber's banner
		// would land beside the LISTENING address the desktop parses.
		DisableStartupMessage: true,
		JSONEncoder:           goccyjson.Marshal,
		JSONDecoder:           goccyjson.Unmarshal,
		ReadTimeout:           sessionTimeout,
		WriteTimeout:          sessionTimeout,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			// Honour the status a handler asked for. Collapsing everything to 500
			// turned every fiber.NewError (403 "managed by control plane", 501
			// "push disabled", …) and every router 404 into an internal error,
			// which the web client then retries as if the workspace were waking.
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

	// A panic in any handler (or in a service it calls) would otherwise take the
	// whole process down, killing every in-flight SSE stream and board run.
	app.Use(recovermw.New(recovermw.Config{EnableStackTrace: true}))

	// The UI is never same-origin with this server: the desktop bundle serves it
	// from app://tasktrooper and a checkout from the Vite dev server, while the
	// API answers on 127.0.0.1:<port>. AllowCredentials stays off — the bearer
	// token is in a header, not a cookie — which is what lets the origin list
	// be trusted as written.
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

// bootConvergeTimeout bounds the whole boot-time convergence pass — the webhook
// reconcile and index-freshness check together. It is a bound rather than a
// budget nobody may exceed: a pass that runs out is retried on the next start,
// and nothing waits on it.
const bootConvergeTimeout = 15 * time.Minute

// localPushPollInterval is how often an instance GitHub cannot deliver
// webhooks to checks its clones for new commits on the default branch.
const localPushPollInterval = 5 * time.Minute

// httpDrainTimeout bounds step 1 of Shutdown. HTTP requests are short; the
// drain budget belongs to the agent runs in step 2.
const httpDrainTimeout = 15 * time.Second

// Shutdown drains rather than severs. Order matters: a SIGTERM (the desktop
// quitting, an update) very likely lands on a workspace mid-run.
// Previously the run context was cancelled and the DB pool closed before HTTP
// was drained, which failed every in-flight request and abandoned the board
// runner's queue outright.
func (s *Server) Shutdown(ctx context.Context) error {
	var err error
	// 1. Stop accepting connections and let in-flight requests finish; they still
	//    have their context and the DB. Bounded separately from ctx: an open SSE
	//    stream would otherwise hold the whole drain budget that step 2 needs.
	if s.app != nil {
		httpCtx, cancelHTTP := context.WithTimeout(ctx, httpDrainTimeout)
		err = s.app.ShutdownWithContext(httpCtx)
		cancelHTTP()
	}
	// 2. Let background workers finish the job they are on. The board runner owns
	//    task_agent_runs rows — dropping it mid-run leaves them stuck 'running'
	//    until the reconciler's stale sweep, minutes later. Drain waits for the
	//    agent runs themselves (minutes of real work: a branch, edits, a build)
	//    and only cancels if ctx — the pod's grace period — runs out first.
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
	// The store monitor is mid-sweep more often than not (it renews signing
	// assets and pushes GitHub secrets); yanking its context at step 3 could
	// cut a Upsert or a pushSecrets in half.
	if s.engine.storeMonitor != nil {
		s.engine.storeMonitor.Stop()
	}
	// Same mid-sweep concern as the store monitor above: a GitHub Actions
	// list call or a dispatch reconciliation could be in flight.
	if s.engine.deployMonitor != nil {
		s.engine.deployMonitor.Stop()
	}
	if s.engine.evolutionSvc != nil {
		s.engine.evolutionSvc.Stop()
	}
	// 3. Only now cancel anything still holding the run context.
	s.cancel()
	// 4. External processes, then the pool everything above was using.
	if s.engine.mcpManager != nil {
		s.engine.mcpManager.Close()
	}
	if s.engine.browserSession != nil {
		s.engine.browserSession.Close()
	}
	// Releases every device lease this process still holds. Appium's own
	// newCommandTimeout would reap them eventually; doing it here means the next
	// pod can take the phones immediately rather than after that timeout.
	if s.engine.mobilePool != nil {
		s.engine.mobilePool.Close()
	}
	// The same obligation on the cloud path, where the device is a simulator on
	// somebody's laptop: a replica that goes away mid-rollout without deleting
	// its Appium sessions leaves that person's Mac holding devices nothing is
	// using until the hub's own timeout.
	if s.engine.pgPool != nil {
		s.engine.pgPool.Close()
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

	// Registered here rather than with the other code tools below, which are
	// gated on a semantic index: working on a file needs no index, and an agent
	// on an unindexed repository is exactly the one that would otherwise fall
	// back to `sed -n` for reading and `sed -i` for writing. The tool policy is
	// what decides who may call the writers — see domain.workspaceWriteTools.
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
		// No resolver yet: it needs the database, which does not exist this
		// early. Until buildHandler installs one, every call uses the fallback
		// above — the client built from config.yml.
		//
		// The old code also registered that same fallback as the `local`
		// PROVIDER here. That is gone with the rest of the shared client map:
		// `local` is a stored provider row like every other, and an install that
		// has not configured one must not inherit whatever base_url is in the
		// file.
		e.multiLLM = llm.NewMultiProviderClient(fallback, nil)
	}
	e.llmClient = e.multiLLM
}

// wireRepositoryStore builds the postgres-backed repository store with the
// boot-time cipher injected, not a lazy one: SetWebhook/WebhookSecret are the
// only encrypted columns on this store, and their first real use is always a
// later HTTP request — after scrubProcessSecrets has already wiped
// MCP_SECRETS_KEY from the process environment. See pgSettings.SetCipher and
// mobileStore.SetCipher below for the same fix on their stores.
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
	var vercelCreds port.VercelCredentialStore
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
			// Seeds the default board once per install (install_state) and runs
			// the boot steps; built here because it needs the database.
			e.bootSeed = bootseed.NewService(pgstore.NewBoardSeedStore(pgDB))
			// SetHostRoots, same reason as the repository store below:
			// sessions.workspace_dir and sessions.project_root are absolute
			// paths belonging to whichever host wrote the row. Resuming a
			// pod-written chat here without the translation reaches
			// os.MkdirAll("/data/workspaces/...") and fails the turn.
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
			// And again for workspace_indexes.root_path, whose only use after a
			// read is the skeleton walk in indexer.Injector: everything else in
			// an index row is stored relative to that root, so re-anchoring it
			// points the walk at this host's checkout of the same repository
			// instead of failing silently on a path that cannot exist here.
			indexStore = pgstore.NewIndexStore(pgDB).
				SetHostRoots(cfg.Storage.Sessions.WorkspaceRoot, cfg.Indexer.AllowedRoots)
			embedMapStore = pgstore.NewEmbeddingMapStore(pgDB)
			// SetHostRoots: repositories.root_path is an absolute path written
			// by whichever host imported the repo, and one database is
			// now served by two (the cloud pod's PVC at /data and the user's
			// Mac behind a reverse tunnel). The store re-anchors a foreign
			// path onto this host's workspace root on read; without it a board
			// run here dies in git clone with "mkdir /data: read-only file
			// system".
			repositoryStore = wireRepositoryStore(pgDB, cfg, e.secretsCipher, e.secretsCipherErr)
			boardTaskStore = pgstore.NewBoardTaskStore(pgDB)
			criterionStore = pgstore.NewAcceptanceCriterionStore(pgDB)
			testCaseStore = pgstore.NewTaskTestCaseStore(pgDB)
			relationStore = pgstore.NewTaskRelationStore(pgDB)
			documentStore = pgstore.NewTaskDocumentStore(pgDB)
			initiativeStore = pgstore.NewInitiativeProjectStore(pgDB)
			boardConfigStore = pgstore.NewBoardConfigStore(pgDB)
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
			// The boot-time cipher, not a lazy one: by the time this store is
			// first used the environment no longer carries MCP_SECRETS_KEY.
			pgSettings.SetCipher(e.secretsCipher, e.secretsCipherErr)
			settingsStore = pgSettings
			githubTokens = pgSettings
			vercelCreds = pgSettings
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
		// The reload hook is a SIBLING of the LLM credential leak: mcp.Service
		// calls it after every create/update/delete — a request path — and it
		// rebuilds the process-wide manager from the stored servers, registering
		// their tools into the one process-wide registry with their secrets in the
		// child environment.
		//
		// Wired unconditionally, because the refusal now lives in reloadMCP
		// itself. It used to live here, and that was the mistake: the other
		// caller (engine.reload, via POST /admin/reload) walked straight past a
		// guard that only ever covered this one. One guard, on the resource.
		mcpService = mcpsvc.NewService(mcpStore, e.secretsCipher, e.reloadMCP)
		// The default catalog of stdio/http MCP servers, seeded as a boot step
		// so it runs after the board seed, once per process.
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

	// Embedding model adı config'ten okunmuyor: UI'dan (LLM ayarları) seçilen
	// model DB'den MultiProviderClient.SetEmbeddingModel ile pinlenir ve boş
	// parametreyi ezer. Buradaki boş ad yalnız hiç ayar yapılmamış kurulumun
	// son çare fallback'idir (sağlayıcının yüklü/varsayılan modeli).
	embeddingModel := ""

	mapperSvc := mapper.NewService(cfg.Mapping)
	chunkers := chunker.DefaultRegistry()

	llmClient := e.llmClient
	if e.multiLLM != nil {
		llmClient = e.multiLLM
		// Pacing lives on the shared client so the indexer, RAG uploads and
		// query rewriting draw on one embedding quota.
		e.multiLLM.SetEmbeddingLimits(cfg.Embedding)
	}
	// Token kullanım kaydı: tüm chat çağrıları (agent loop, planner, sentez)
	// bu sarmalayıcıdan geçer.
	if usageStore != nil {
		llmClient = usageapp.NewRecordingClient(llmClient, usageStore)
	}
	// Embedding query-vektör önbelleği RecordingClient'ın DIŞINA sarılır: bir
	// önbellek isabetinde alttaki (kaydeden) client hiç çağrılmaz, dolayısıyla
	// harcanmayan bir çağrı için kullanım da kaydedilmez. codebase_search,
	// inject.go'nun sorgu-zamanı embed'i, catalog.SearchSkills, memory ve rag
	// hepsi bu tek sarmalayıcıdan geçer; ayrı ayrı değişiklik gerekmez.
	llmClient = usageapp.NewCachingEmbedder(llmClient, cfg.Embedding.QueryCacheEntries)

	if indexStore != nil {
		codeKit := code.NewToolKit(indexStore, llmClient, mapperSvc, cfg.Indexer, cfg.Graph, embeddingModel)
		for _, tool := range code.NewExecutors(codeKit) {
			e.reg.Register(tool)
		}
	}

	// Ledger recording wraps the audited registry, so every path that runs a
	// tool (chat, orchestrated subtask, board run) leaves a durable record of
	// the board entities it touched — the agent loop's own trace does not
	// outlive the loop.
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
	// Built here, wired with an executor much further down (the CLI's existence
	// is not known until the board block probes for it) and handed to every
	// agentic consumer in between. That order is why the executor arrives
	// through a setter on a live object rather than through the constructor.
	e.agentRouter = agent.NewRouter(e.agentLoop)

	var ragSvc *rag.Service
	if fileStore != nil && cfg.RAG.Enabled {
		ragSvc = rag.NewService(fileStore, llmClient, cfg.RAG)
	}

	// Binary attachments (images/documents) for tasks and chat. Postgres-backed
	// bytes — the pod disk the RAG file pipeline writes to is ephemeral.
	var attachmentSvc *attachmentapp.Service
	if attachmentStore != nil {
		attachmentSvc = attachmentapp.NewService(attachmentStore, boardTaskStore)
		// Screenshots a tool took go into the same store the UI already fetches
		// attachments from, so the activity feed can show the human the picture
		// the model was handed instead of a sentence describing it.
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
	// Defaulted BEFORE the loop is given the budget, not after. Budget is a
	// value, so a field filled in later never reached the loop's copy — and the
	// loop's summarizing trim reads KeepRecentMessages to decide how much of the
	// recent conversation it must keep verbatim. A zero there means "keep
	// nothing recent", which is the opposite of what an unset config wants.
	if budget.KeepRecentMessages <= 0 {
		budget.KeepRecentMessages = 10
	}
	summarizer := appcontext.NewLLMSummarizer(llmClient)
	// The agent loop trims with the same budget its callers do. Without this the
	// budget was applied once to the history handed in and never again, and the
	// loop's own tool results grew the request back past it many times over.
	e.agentLoop.SetHistoryBudget(budget)
	// ...and it condenses what it drops into one block at a fixed position
	// instead of deleting messages out of the middle, so the request's prefix
	// stays byte-identical between trims and the provider's cache survives a
	// long run. See Loop.SetSummarizer.
	e.agentLoop.SetSummarizer(summarizer)
	// Mid-run token circuit breaker: billing.Service.Allow (below) only gates
	// BEFORE a run starts, and MaxIterations/TaskMaxIterations count turns, not
	// tokens — neither catches a run that blows past its token budget DURING
	// execution. 0 (unset) disables it. See Loop.SetRunTokenCap.
	e.agentLoop.SetRunTokenCap(cfg.LLM.RunMaxTotalTokens)

	var indexSvc *indexer.Service
	var indexInjector *indexer.Injector
	var repositorySvc *repository.Service
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
		for _, tool := range skilltools.NewExecutors(&skilltools.ToolKit{
			Catalog: catalogStore,
			Creator: catalogSvc,
			Events:  evolutionStore,
			Perf:    perfStore,

			MaxSkills: cfg.Evolution.MaxSkillsPerAgent,
		}) {
			e.reg.Register(tool)
		}
		// Orchestration is always on, and the role agents it dispatches to are
		// seeded as a boot step: background work, because a dozen writes must
		// not hold up anything.
		e.bootSeed.AddStep("role_agents", func(stepCtx context.Context) error {
			err := catalogSvc.EnsureRoleAgents(stepCtx)
			// The seed stores skills without vectors so it never waits on the
			// embedder. The backfill outlives the boot step's deadline on
			// purpose: a first launch is still downloading the model.
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
			return err
		})
		orchSvc = orchestrator.NewService(llmClient, catalogStore, nil, e.agentRouter, cfg.Orchestration, contextBuilder)
		// Intake and the planner run without tools; the snapshot is what keeps
		// them from asking the stakeholder about repositories the system knows.
		if initiativeStore != nil && repositoryStore != nil {
			orchSvc.SetWorkspace(workspaceLister{projects: initiativeStore, repos: repositoryStore})
		}
		// Subtasks in one wave run concurrently; a dependent subtask must read
		// the ledger fresh to see what its dependency just put on the board.
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
	}

	var boardRunner *boardapp.Runner
	// hostChatExecutor is declared here, beside boardRunner, because it is set
	// once mux is built, far below, and consumed by the session service
	// further below still. It is the SAME mux the board's TaskExecutor uses,
	// not a second instance: the concurrency cap, the slot queue and the
	// subscription behind each provider's executor are per-executor, so a
	// second one would let chat and the board each believe they had the whole
	// budget.
	//
	// Nil when no host-executed provider has an executor at all, which every
	// caller of a provider on this seam treats as "this agent cannot run
	// here" rather than as a degraded mode.
	var hostChatExecutor port.ChatExecutor
	if boardConfigStore != nil {
		workspaceSvc = workspace.NewService(boardConfigStore)
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
	// Left nil on a build with no board runner, so the interface field on the
	// HTTP config stays nil too and the stop/re-run routes answer 503 instead
	// of dereferencing a runner that was never built.
	var boardRunControl httpadapter.BoardRunControl
	// Left nil the same way when there is no board: the task-chat route then
	// answers 503 instead of dereferencing an opener that was never built, and a
	// session with no task resolver keeps its old mirror-clone workspace.
	var boardTaskChat httpadapter.TaskChatControl
	var taskChatWorkspaces session.TaskWorkspaceResolver

	// The local agent CLI connect flow. Both stores are required and neither is
	// optional-with-a-fallback: without the catalog there is nothing to write
	// out, and without the connection row there is nowhere to record what was
	// verified, so a build missing either answers 503 on the routes instead of
	// pretending to connect.
	//
	// The probe follows the SESSIONS, on the same condition the executor choice
	// below follows: with a control plane in front of this process the `claude`
	// binary and the subscription are on the member's Mac, so connect reads the
	// environment report that Mac pushed (claudecode.Preflight) instead of
	// running anything here. Running the local probe there asked a Linux pod
	// whether it had Claude Code, and told every cloud user to install it on a
	// machine they cannot see.
	//
	// Without one, unchanged: the local probe is given the executor's OWN
	// binary name and setting sources, so what connect verifies is the same
	// program, configured the same way, that a board run will start. See
	// agentcli.DefaultProbe.
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

	// Hoisted above the block that creates it: the criteria-loop guard is
	// wired onto it later, once repositorySvc exists (it is both the task
	// commenter and the acceptance-criteria reader), and that wiring lives
	// next to ReviewLoopGuard/PipelineBounceGuard's in a sibling block below.
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
		boardDispatcher = boardapp.NewDispatcher(boardConfigStore, boardEventStore, taskAgentRunStore, boardRunner, cfg.Board.DispatchEnabled)
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
		// The MCP tool endpoint is created with the local executor and only
		// with it: it exists to serve CLI sessions, so on a host with no CLI
		// it would be a route nothing can authenticate against. That is also
		// why it needs no config knob — "enabled" and "the executor is
		// registered" are the same fact. The endpoint is the one Run already
		// published the bound address on, so a board run dispatched by the
		// activation at the end of this function has a reachable URL from
		// its first millisecond. Allocated here only for the callers that
		// build a handler without going through Run.
		if e.mcpEndpoint == nil {
			e.mcpEndpoint = &mcpEndpoint{}
		}
		claudeMCP := &claudeCodeMCP{endpoint: e.mcpEndpoint, tokens: mcpserver.NewRunTokenRegistry(), registry: toolReg}
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
			log.Info().Err(ccErr).Msg("claude code executor not registered; agents on the claude_code provider cannot run on this host")
		} else {
			claudeExecutor = executor
			// toolReg, not the bare registry: the audit and action-ledger
			// decorators are what make a CLI session's tool call show up in the
			// same places a loop run's does.
			e.mcpServer = mcpserver.New(toolReg, claudeMCP.tokens)
			// This route sits outside the prefixes the auth middlewares gate, so
			// the per-run token would otherwise be the only thing in front of
			// this install's board tools. The only legitimate client is a claude
			// child on this host.
			e.mcpServer.SetLoopbackOnly(true)
			log.Info().
				Str("mcp_endpoint", e.mcpEndpoint.get()).
				Msg("claude code executor enabled, tasktrooper tools served over mcp")
		}

		if executor, agErr := antigravity.New(antigravity.Config{
			Binary:     cfg.Antigravity.Binary,
			RunTimeout: cfg.Antigravity.RunTimeout,
		}); agErr != nil {
			log.Info().Err(agErr).Msg("antigravity executor not registered; agents on the antigravity provider cannot run on this host")
		} else {
			antigravityExecutor = executor
			log.Info().Msg("antigravity executor enabled")
		}

		if executor, curErr := cursor.New(cursor.Config{
			Binary:     cfg.CursorAgent.Binary,
			RunTimeout: cfg.CursorAgent.RunTimeout,
		}); curErr != nil {
			log.Info().Err(curErr).Msg("cursor executor not registered; agents on the cursor_agent provider cannot run on this host")
		} else {
			cursorExecutor = executor
			log.Info().Msg("cursor executor enabled")
		}

		if executor, ocErr := opencode.New(opencode.Config{
			Binary:     cfg.Opencode.Binary,
			RunTimeout: cfg.Opencode.RunTimeout,
		}); ocErr != nil {
			log.Info().Err(ocErr).Msg("opencode executor not registered; agents on the opencode provider cannot run on this host")
		} else {
			opencodeExecutor = executor
			log.Info().Msg("opencode executor enabled")
		}

		// Wire the mux executor to the board runner and router
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
			// No live-run checker any more. It asked THIS process whether a run
			// was executing, which on a shared deployment reports every other
			// replica's live run as abandoned; the run row's own heartbeat is
			// the answer every process can see. See Reconciler.run.
			//
			// A killed process leaves its orchestration plan and subtasks at
			// "running"; recovering the run row alone still left the card live.
			reconciler.SetPlanSettler(catalogStore)
			// Reconciler.Start sweeps immediately, dispatching stale and
			// never-started tasks — same reason as the runner above.
			activateBoard = append(activateBoard, func() {
				reconciler.Start(ctx, cfg.Board.ReconcileInterval)
			})
		}
	}

	var scoreTracker *boardapp.ScoreTracker
	if repositoryStore != nil && boardTaskStore != nil {
		repositorySvc = repository.NewService(repositoryStore, boardTaskStore, criterionStore, relationStore, documentStore, commentStore, workspaceSvc, boardDispatcher, indexSvc, cfg.Indexer.AllowedRoots)
		repositorySvc.SetGit(gitClient, cfg.Storage.Sessions.WorkspaceRoot)
		repositorySvc.SetPublicBaseURL(cfg.Server.PublicBaseURL)
		repositorySvc.SetTestCaseStore(testCaseStore)
		// The index mirror's restorer. Wired here rather than into the indexer's
		// constructor because the dependency runs backwards — the repository
		// service already holds the indexer — so a constructor argument would be
		// a cycle.
		//
		// It is what stops an index pass from walking a directory that is not
		// there. The mirror is a CACHE of the git remote on a pod disk that
		// several replicas do not share and no restart preserves, so "the
		// checkout is missing" is an ordinary Tuesday rather than an anomaly,
		// and a pass that indexed nothing and reported completed would show a
		// green 100% over an index that answers no query.
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
			repositorySvc.SetScorer(scoreTracker)
		}
		if taskSpanStore != nil {
			repositorySvc.SetCompletionStamper(boardapp.NewCompletionStamper(boardTaskStore, taskSpanStore))
			// The span ledger is also where the review-chain gate reads its
			// evidence: which stages a task actually passed through.
			repositorySvc.SetSpanStore(taskSpanStore)
			if scoreTracker != nil {
				repositorySvc.SetReviewGate(boardapp.NewReviewGate(taskSpanStore, scoreTracker))
			}
		}
		if boardRunner != nil {
			boardRunner.SetRepositories(repositorySvc)
			boardRunner.SetTaskUpdater(repositorySvc)
		}
		repositorySvc.SetRequireCriteriaComplete(cfg.Board.RequireCriteriaComplete)

		// Work order: the `blocks` relation, enforced. The gate lives in the
		// dispatcher because that is the only place a run starts, and the
		// sweeper is what lets a park end without anyone touching the card.
		// Both are registered on the relation store being present — without it
		// there is no order to read and the board dispatches exactly as before.
		if relationStore != nil && boardDispatcher != nil {
			workOrder := boardapp.NewWorkOrder(relationStore, boardTaskStore)
			// The commenter is the SERVICE, not the comment store: a comment
			// created through it lands on the card the same way every other
			// system comment does, including the board event that makes it
			// visible in task history.
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

		// Review-cycle cap: a task that keeps arriving in need_revision with no
		// human weighing in stops being dispatched and waits for a person. It
		// hangs off the event store because the streak IS board history —
		// nothing else survives the restarts and the multi-pod delivery the
		// loop it brakes ran across.
		if boardDispatcher != nil && boardEventStore != nil {
			reviewLoop := boardapp.NewReviewLoopGuard(boardEventStore, boardTaskStore)
			// The service, not the comment store, for the same reason the work
			// order's commenter is: a comment made through it lands on the card
			// with the board event that makes it visible in task history.
			reviewLoop.SetCommenter(repositorySvc)
			// The park is a move out of need_revision, and nothing else writes
			// it: Dispatch's own event was for the move INTO that column. Without
			// the journal the card jumps to blocked unexplained and the
			// need_revision column span never closes.
			reviewLoop.SetParkJournal(boardapp.NewParkJournal(boardEventStore, taskSpanStore))
			boardDispatcher.SetReviewLoopGuard(reviewLoop)
		}

		// Criteria-loop cap: the third machine-talking-to-itself shape,
		// closed the same way as the two above — a task whose runs keep
		// exhausting the criteria sweep with the SAME criteria left open,
		// unattended, stops being retried and waits for a person. Unlike the
		// other two this is not detected on an incoming board event, so it
		// hangs off the reconciler rather than the dispatcher.
		if reconciler != nil && boardEventStore != nil && boardTaskStore != nil {
			criteriaLoop := boardapp.NewCriteriaLoopGuard(boardEventStore, boardTaskStore, repositorySvc)
			criteriaLoop.SetCommenter(repositorySvc)
			criteriaLoop.SetParkJournal(boardapp.NewParkJournal(boardEventStore, taskSpanStore))
			reconciler.SetCriteriaLoopGuard(criteriaLoop)
		}

		// Project profile: agent-maintained per-repository brief. Built by the
		// shared agent loop on the system-architect's model; the tool is
		// registered globally so board-run developers can update the profile
		// mid-run, and the repository service gets the refresher for its
		// import/push/manual triggers.
		// The sectioned profile store is Postgres-only: the derived/agent split,
		// per-section staleness and the settings proposals all live in the two
		// tables migration 086 adds. Without a pool the service degrades to the
		// single rendered blob on the repositories row rather than failing.
		var profileSectionStore port.RepositoryProfileStore
		if e.pgDB != nil {
			profileSectionStore = pgstore.NewRepositoryProfileStore(e.pgDB)
		}
		profileSvc := repoprofileapp.NewService(repositoryStore, profileSectionStore, e.agentRouter)
		if catalogStore != nil {
			profileSvc.SetAgentLister(catalogStore.ListAgents)
		}
		if e.pgDB != nil {
			// Pipeline slots are one of the settings a profiling pass can fill
			// in from the deploy workflows it just parsed.
			profileSvc.SetPipelineJobs(pgstore.NewRepositoryPipelineJobStore(e.pgDB))
		}
		for _, tool := range repoprofiletools.NewExecutors(&repoprofiletools.ToolKit{Profiles: profileSvc}) {
			e.reg.Register(tool)
		}
		repositorySvc.SetProfileRefresher(profileSvc)

		if attachmentStore != nil {
			// Task detail responses carry attachment metadata alongside documents.
			repositorySvc.SetAttachmentStore(attachmentStore)
		}
		boardKit := &board.ToolKit{Tasks: repositorySvc}
		if initiativeStore != nil {
			boardKit.Workspace = workspaceLister{projects: initiativeStore, repos: repositoryStore}
		}
		if catalogStore != nil {
			boardKit.Team = catalogStore
		}
		if attachmentSvc != nil {
			boardKit.Attachments = attachmentSvc
		}
		// The task<->pull-request use case: read the PR a task is reviewed in,
		// push a change into it, answer a reviewer on it. It needs GitHub for
		// everything but the commit, so the tools are registered only when a token
		// store exists — a build without one would otherwise offer three tools
		// that can only report "GitHub is not connected".
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
				// The lifecycle gates the merge re-asks before it lands
				// anything: the repository's require_review_chain check and the
				// task's last pipeline verdict. Same service that guards the
				// move into done, so there is one definition of each.
				Gates:         repositorySvc,
				WorkspaceRoot: cfg.Storage.Sessions.WorkspaceRoot,
			})
			boardKit.PullRequests = taskPRSvc
			// The same reader the tools use, so a revision run is handed the
			// reviewer's PR comments without having to call a tool for them.
			if boardRunner != nil {
				boardRunner.SetPullRequestReader(taskPRSvc)
			}
		}
		for _, tool := range board.NewExecutors(boardKit) {
			e.reg.Register(tool)
		}
		// The chat a human opens about one task, and the branch checkout its turns
		// run in. Both hang off the same three stores; building them here keeps
		// them beside the tools the chat's agent calls.
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
			// QA is left as a nil interface (not a typed-nil *Dispatcher) when
			// boardDispatcher is nil, so PipelineRunner's `p.qa != nil` check
			// behaves correctly instead of tripping the typed-nil interface trap.
			var qaDispatcher boardapp.QADispatcher
			if boardDispatcher != nil {
				qaDispatcher = boardDispatcher
			}
			var tokenSource boardapp.TokenSource
			if githubTokens != nil {
				tokenSource = githubTokens.GitHubToken
			}
			// Built here rather than beside deploySvc below: the pipeline
			// runner needs it to tell a store prod deploy from a server one.
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
			if boardDispatcher != nil {
				boardDispatcher.SetPipelineGate(true)
				// …and the per-repository off switch for it. Without this the
				// gate is unconditional, which is a deadlock on any repository
				// whose CI cannot answer.
				boardDispatcher.SetPipelineGatePolicy(repositorySvc)
			}
			pipelineRunner.SetStageVerifier(repositorySvc)
			// Lets a finished prod deploy re-read the task and post its
			// after_deploy steps against the current text, not the snapshot the
			// pipeline was queued with.
			pipelineRunner.SetTaskReader(repositorySvc)
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
			// Delivery dedupe that outlives the process. GitHub retries to
			// whichever replica the load balancer picks, so an in-memory map
			// answers only for the pod that happened to get the first attempt.
			repositorySvc.SetDeliveryLedger(pgstore.NewWebhookDeliveryStore(e.pgDB))
			repositorySvc.SetDeployPackages(pgstore.NewDeployPackageStore(e.pgDB))
			repositorySvc.SetPipelineJobStore(pipelineJobStore)
			if githubTokens != nil {
				repositorySvc.SetGitHubTokenSource(githubTokens.GitHubToken)
			}
			// Two boot-time convergence passes:
			//
			//   webhook_reconcile — repos registered before webhook support get
			//     one installed, and repos whose hook predates the Actions
			//     events get that hook widened in place. Every existing hook out
			//     there is push-only, which is why the code-review gate had no
			//     signal to open on.
			//   index_freshness — catch-up for pushes that landed while this pod
			//     was down. GitHub does not redeliver them, so without this the
			//     clone (and the index built from it) stays at the pre-downtime
			//     commit until a human opens the repository settings page.
			//
			// Backgrounded together, on one goroutine: both are GitHub round
			// trips per repository and neither is on any request path. The
			// budget is the whole pass's; one that cannot converge in fifteen
			// minutes has a GitHub problem, and the next start tries again.
			go func() {
				bootCtx, cancel := context.WithTimeout(ctx, bootConvergeTimeout)
				defer cancel()
				repositorySvc.ReconcileWebhooks(bootCtx)
				repositorySvc.SweepIndexFreshness(bootCtx)
				repositorySvc.ResumeUnfinishedIndexes(bootCtx)
			}()
			// GitHub will not deliver webhooks to a loopback or private address,
			// and a desktop install listens on 127.0.0.1. Poll instead: the
			// sweep pulls each clone's default branch, reindexes when it moved
			// and refreshes the profile sections the new commits touched.
			// freshnessCheckInterval still throttles each repository.
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

			// The belt to the in-process poll's braces. The poll dies with its
			// pod and its queue is in memory; this reads the rows, asks GitHub,
			// and — when nothing can answer — opens the code-review gate with a
			// reason rather than leaving the card wedged behind a signal that
			// is never coming.
			boardapp.NewPipelineGateSweeper(pipelineStore, pipelineRunner, cfg.Board.PipelineGateTimeout).
				Start(ctx, cfg.Board.PipelineGateInterval)

			// Deploy definitions and production incidents. Both hang off the
			// board: a deploy target is authored by a task, and an incident
			// feeds a task back. Wiring them after the pipeline runner means
			// the runner can report failed deploys as incidents.
			deploySvc := deploy.NewService(deployTargetStore, repositoryStore)
			deploySvc.SetTaskCreator(repositorySvc)
			repositorySvc.SetDeployTargets(deployTargetStore)
			if catalogStore != nil {
				deploySvc.SetAgentLister(catalogStore.ListAgents)
			}
			e.deploySvc = deploySvc

			// Reference docs (coding standards / test standards /
			// architecture / local run): the same "a human names where it
			// lives, an agent can be asked to write it" idea as deploySvc
			// above, so it shares its TaskCreator/AgentLister wiring.
			repoDocsSvc := repodocs.NewService(repositorySvc)
			repoDocsSvc.SetTaskCreator(repositorySvc)
			if catalogStore != nil {
				repoDocsSvc.SetAgentLister(catalogStore.ListAgents)
			}
			// The docs bundle lands as one pull request, and the screen that
			// asked for it offers to merge that PR — through the same use case
			// (and the same refusals) every other task's merge goes through.
			// nil without GitHub, where the merge could only ever refuse.
			if taskPRSvc != nil {
				repoDocsSvc.SetTaskPRMerger(taskPRSvc)
			}
			e.repoDocsSvc = repoDocsSvc

			// Hosting links: where each frontend/backend actually lives, as a
			// Vercel project. Wired beside the deploy service because a
			// root-area link fills the prod deploy target (address, health
			// URL, recipe vars) — the same store the runner and monitor read.
			hostingSvc := hostingapp.NewService(pgstore.NewHostingLinkStore(e.pgDB), repositoryStore, vercelCreds, vercelapi.New())
			hostingSvc.SetDeployTargets(deployTargetStore)
			e.hostingSvc = hostingSvc

			// Repo/database dependency edges feed the project overview's
			// architecture view. SetCipher before MCP_SECRETS_KEY is
			// scrubbed, same reason as the repository and mobile-device
			// stores above.
			repoDependencyStore := pgstore.NewRepoDependencyStore(e.pgDB)
			repoDependencyStore.SetCipher(e.secretsCipher, e.secretsCipherErr)
			e.repoDependencySvc = repodependency.NewService(repoDependencyStore, repositoryStore)

			// One *vercel.Client for both roles. hostingSvc reads the account to
			// fill a deploy target; vercelOpsSvc binds one scope of a repository
			// to one project and reads that project's deployments. Same token,
			// same rate limit — two clients would only split the budget.
			vercelClient := vercelapi.New()
			e.vercelOpsSvc = vercelops.NewService(vercelops.Deps{
				Links:       pgstore.NewVercelProjectLinkStore(e.pgDB),
				Creds:       vercelCreds,
				API:         vercelClient,
				Deployments: vercelClient,
				Repos:       repositoryStore,
			})

			// The user's own Google Cloud account, read-only: Cloud Run
			// services and GKE clusters behind a service account they saved.
			// Repos is handed over so a bind can check the sub-project path it
			// is given actually exists; nil would accept any path silently.
			e.gcloudOpsSvc = gcloudops.NewService(gcloudops.Deps{
				Credentials: pgstore.NewGCloudCredentialStore(e.pgDB),
				Bindings:    pgstore.NewGCloudResourceStore(e.pgDB),
				Cipher:      e.secretsCipher,
				NewClient:   func(c domain.GCloudCredential) (port.GCloudClient, error) { return gcloud.New(c) },
				Repos:       repositoryStore,
			})

			// Real-device tools. Registered here rather than beside the browser
			// tools because their guard IS the deploy target store: the only
			// app mobile_launch_app may open is the package a human recorded on
			// a target, and that store does not exist until this point.
			//
			// No enable flag — an installation with no hub URL and no device
			// registers no tools at all. Handing agents a mobile_tap that can
			// only ever answer "no device configured" spends a tool call and a
			// turn to say what the tool list could have said by being absent.
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
				// nil when MOBILE_BRIDGE_URL is unset, which is the normal case
				// on a host running local simulators and emulators: there is no
				// cluster sidecar to pair a phone through. Its absence must not
				// disable anything else — the bridge serves remote_adb only,
				// and tool registration below is driven by the effective
				// devices whatever kind they are.
				deviceagent.New(cfg.Tools.Mobile.BridgeURL, cfg.Tools.Mobile.BridgeToken),
				envDevice,
			)
			// The other half of that: what this machine itself can drive.
			// Detected rather than configured, because it is a fact about the
			// host. New() never fails; a host with no Xcode and no Android SDK
			// simply reports none.
			mobileDeviceSvc.SetLocalHost(localdevice.New(localdevice.Config{}))
			e.mobileDeviceSvc = mobileDeviceSvc

			// The pool exists whether or not a phone is attached yet, so a
			// device registered later in the settings UI can be swapped in
			// without a restart. What is gated is the TOOLS: an agent shown
			// mobile_tap on an installation with no phone spends a call and a
			// turn to learn what an absent tool would have said for free.
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

			// Applying registrations is the whole reason this is a callback and
			// not a config read: the tools hold the pool for the life of the
			// process, so attaching or removing a phone has to retarget it in
			// place. Registration is one-way on purpose — unregistering every
			// device leaves the tools present and explicitly failing, which is
			// a truthful "the phone is gone", where silently removing them
			// would look to the agent like the capability never existed.
			mobileDeviceSvc.SetReloader(func(_ context.Context, devices []domain.MobileDevice) error {
				e.mobilePool.Reconfigure(mobileConfigsOf(devices))
				if len(devices) > 0 {
					registerMobileTools()
				}
				return nil
			})

			// Whatever is registered (or, failing that, in the environment)
			// is what the agents drive from the first run — read as a boot
			// step, after the board seed.
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

			// The release half of the device lease: a task parked because the
			// phone was taken has nothing to wake it — no answer arrives, no
			// human drags it — so the sweep is the only way back. Started
			// unconditionally: a device attached later must not also need a
			// restart to get its sweeper, and a sweep with nothing parked is a
			// probe against an unconfigured session, which returns immediately.
			if boardTaskStore != nil && boardDispatcher != nil {
				sweeper := boardapp.NewDeviceSweeper(boardTaskStore, e.mobilePool, boardDispatcher)
				activateBoard = append(activateBoard, func() {
					sweeper.Start(ctx, boardapp.DeviceSweeperInterval)
				})
			}

			// Task checkouts are a few hundred megabytes each and nothing ever
			// removed one, so the volume filled and every build on it began
			// failing with ENOSPC — in tasks unrelated to the ones holding the
			// space, and now belonging to unrelated customers. Deleting a task
			// takes its directory with it; this collects the rest, once they
			// have been finished long enough that nobody is about to drag the
			// card back.
			if boardTaskStore != nil && cfg.Storage.Sessions.WorkspaceRoot != "" {
				// The run STORE is the "is this checkout in use" probe, not the
				// local runner: a checkout being written by a run on another
				// replica looked idle to this one's map, and the reaper would
				// have deleted the directory mid-commit.
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
			if catalogStore != nil {
				prodOpsDeps.Agents = catalogStore.ListAgents
			}
			prodOpsSvc := prodops.NewService(prodOpsDeps)
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

			// Store console credential vault + mobile app registry. Onboarding
			// bridges into deploySvc.SaveTarget (SetStoreOnboarder) and the
			// registry gates stage/prod deploys (SetMobileStoreApps) — both
			// wired unconditionally so a store deploy target always reads the
			// real onboarding state instead of silently no-op'ing.
			storeCredentialStore := pgstore.NewStoreCredentialStore(e.pgDB)
			storeAppStore := pgstore.NewMobileStoreAppStore(e.pgDB)
			storeSigningStore := pgstore.NewSigningAssetStore(e.pgDB)

			// A cipher failure (missing MCP_SECRETS_KEY/SERVER_API_KEY) must not
			// stop the server from booting — same graceful-degrade idiom as the
			// mcp/llmprovider secret ciphers above. storeOpsSvc is still built
			// and wired with a nil cipher; only the credential-vault paths that
			// actually need to encrypt/decrypt fail until the key is set.
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
				// PushSecret resolves the repo's owner/name and GitHub token the
				// same way the pipeline's DispatchWorkflow callers do (see
				// application/board/pipeline.go), adapted from a task workspace
				// to a bare repository ID: the repo's own RootPath stands in for
				// the task workspace dir when resolving the origin remote.
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
			// Task 9: wire the ops_audit_log sink so every store release
			// action (submit, release, promote, rollout, halt, resume) is
			// recorded, success or failure. Unconditional, same as the rest
			// of storeOpsSvc's wiring above — writing an audit row never
			// depends on the cipher or any store credential being present.
			storeOpsSvc.SetAuditor(pgstore.NewOpsAuditStore(e.pgDB))

			// Release engine selection (storeops/engine.go). The probes are
			// closures rather than port interfaces because they only exist to
			// let the application layer ask an adapter a yes/no question it
			// must not import to ask.
			//
			// The Actions probe reads the repository's own workflow list and
			// looks for the EXACT file the dispatch will ask for. Prefix
			// matching was wrong in the case it mattered: a monorepo renders
			// mobile-release-<sub-project>.yml, and a repository still carrying
			// an older mobile-release.yml answered "yes" with a file GitHub
			// then 404s on. GitHub's refusal to answer at all (402 Payment
			// Required on an unpaid org, a permanent 403) travels up as the API
			// error whose text storeops classifies. Nothing here decides — it
			// reports.
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
					// The host IS this machine. SupportsIOSSimulators is the Mac
					// test: it reads the host's own reported capability, and a
					// machine that is not a Mac can never report it.
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

				// The files land BEFORE anything is started, and for both
				// engines. They are the release procedure itself: Actions
				// dispatches the workflow by name out of this branch, and a
				// local run executes the script out of a checkout of it, so
				// generating them and not writing them left both engines
				// running whatever the tree already held — the previous
				// binding's release, or nothing at all.
				//
				// Committed to the repository rather than to the mirror
				// checkout: git.Client.CommitAndPush stages the whole tree
				// (`git add -A`), and the mirror is a tree SyncDefaultBranch
				// treats as disposable, so anything stray sitting in it would
				// ride along into the user's default branch.
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
					// runner.Mobile.Release is the call this wants, and two
					// things it needs are not resolvable here: the checkout as
					// a path relative to the Mac's own workspace root (a
					// workspace.prepare round-trip), and the member whose
					// machine runs it. Refusing is the honest answer until the
					// board drives that — a starter that returned nil would
					// report a release nobody started. ErrNoReleaseEngine is
					// what makes StartBuild park the card instead of handing
					// the operator a 500 they cannot act on.
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
			// Onboard returns the (possibly already test_ready) app row;
			// SetStoreOnboarder's hook only needs to know whether onboarding
			// itself succeeded, so the row is dropped here.
			deploySvc.SetStoreOnboarder(func(ctx context.Context, repositoryID uuid.UUID, provider, identifier, appName string) error {
				_, err := storeOpsSvc.Onboard(ctx, repositoryID, provider, identifier, appName)
				return err
			})
			// Runs BEFORE the target is written, unlike the onboarder above
			// whose failures SaveTarget tolerates: a re-pointed live app must
			// fail the save outright, not be logged and forgotten.
			deploySvc.SetStoreIdentifierGuard(storeOpsSvc.EnsureIdentifierAllowed)
			repositorySvc.SetMobileStoreApps(storeAppStore)
			// A successful store prod deploy IS the submit for review — hand
			// it to storeops so the monitor starts polling the verdict.
			pipelineRunner.SetStoreSubmitter(storeOpsSvc)

			// Gated on the cipher, mirroring how the prodops monitor above is
			// gated on cfg.ProdOps.MonitorEnabled: without a working cipher the
			// credential vault can't decrypt anything the sweep would need
			// (ASC/Play client construction), so starting it would just log a
			// warning every interval for no benefit.
			if e.secretsCipher != nil {
				storeMonitor := storeops.NewMonitor(storeOpsSvc, storeAppStore, prodOpsSvc)
				storeMonitor.Start(ctx, cfg.Storeops.PollInterval)
				e.storeMonitor = storeMonitor
			}

			// Deploy operations console: mirrors GitHub Actions deploy runs
			// locally, attributes console-triggered dispatches back to
			// whoever triggered them, and turns a failed deploy into an
			// incident.
			//
			// Gated on the token STORE, not on a token: NewActionsAPIFor
			// resolves the stored token per call, so an unconnected install gets
			// GitHub's own answer rather than a capability that silently does
			// not exist.
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
				// Carried-over Task 4 defect fix: domain.Repository carries
				// no GitHub owner/repo — resolve them from the checkout's
				// git origin, the same source board/pipeline.go and the
				// PushSecret closure above use, via repo.RootPath.
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

				// Break-glass recorder. Registered here rather than with
				// the rest of the ops tools above because the deploy
				// console service only exists once a GitHub token
				// resolved — an agent that deploys from the machine while
				// Actions is down reports the run through this tool, so
				// the matrix keeps answering "what is live where".
				for _, tool := range opstools.NewLocalDeployExecutors(deployOpsSvc) {
					e.reg.Register(tool)
				}

				if cfg.DeployOps.MonitorEnabled {
					deployMonitor := deployops.NewMonitor(deployOpsSvc, prodOpsSvc)
					deployMonitor.Start(ctx, cfg.DeployOps.PollInterval)
					e.deployMonitor = deployMonitor
				}

				// The deploy watch: what happened in production to the
				// commit a task's merge produced, and the rollback when the
				// answer is "nothing good".
				//
				// Wired here rather than beside the board tools because it
				// needs the deployment-run store and the resolved GitHub
				// token, neither of which exists at registration time. The
				// board ToolKit is a pointer and its DeployWatch field is
				// read at tool-call time, so assigning it now is what turns
				// the three already-registered tools on.
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

				// The park's release mechanism. Without it a task that
				// parked on a running deploy stays in blocked forever: the
				// tool that parked it is the only thing that could unpark
				// it, and it is not running.
				if boardDispatcher != nil {
					deploySweeper := boardapp.NewDeploySweeper(boardTaskStore, deployWatchSvc, boardDispatcher)
					activateBoard = append(activateBoard, func() {
						deploySweeper.Start(ctx, boardapp.DeploySweeperInterval)
					})

					// auto_rollback stops being decorative here: an
					// incident inside a release's health window is
					// attributed to the task that released, and the task's
					// owner is woken to roll it back (or to write the
					// proposal up when the flag is off).
					prodOpsSvc.SetReleaseAttributor(deployWatchSvc)
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
		// save_memory gets first refusal on reusable know-how: the evolution
		// service classifies it and writes a skill instead of a memory.
		if memoryToolKit != nil {
			memoryToolKit.SetPromoter(e.evolutionSvc)
		}
		e.evolutionSvc.Start(ctx)
	}
	if initiativeStore != nil {
		initiativeSvc = initiative.NewService(initiativeStore)
	}

	var settingsSvc *settings.Service
	if settingsStore != nil {
		settingsSvc = settings.NewService(settingsStore)
		if catalogSvc != nil {
			settingsSvc.SetAgentCatalog(catalogSvc)
		}
		if repositorySvc != nil {
			repositorySvc.SetAnalizAssignmentSource(settingsSvc)
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
		// The provider cache, and the resolver the LLM client asks on every
		// call. Constructed before the service so the service can be given its
		// invalidation hook, and installed on the client immediately after —
		// from that moment the client stops using the config.yml fallback and
		// starts answering from the stored settings.
		providers := newProviderCache(func(rctx context.Context) (llmprovider.Resolved, error) {
			return llmProviderSvc.Resolve(rctx)
		}, llmTimeout)
		llmProviderSvc = llmprovider.NewService(llmProviderStore, llmEndpointStore, e.secretsCipher, llmTimeout, providers.Invalidate)
		if e.multiLLM != nil {
			e.multiLLM.SetResolver(providers)
		}
		// The operator and the user are the same person here, so
		// config.yml's llm.base_url and ANTHROPIC_API_KEY/OPENAI_API_KEY/
		// GOOGLE_API_KEY out of the environment are that person's own
		// credentials and seeding them saves a trip to the settings page. A boot
		// step, so it runs after the board seed has written the provider list.
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
		// against. One call rather than two: the indexer forwards the resolver
		// to its store, so wiring the search guard separately is how one half
		// gets wired and the other does not — which looks exactly like the
		// guard working, right up to the moment a stale index is searched.
		if indexSvc != nil {
			indexSvc.SetEmbeddingResolver(llmProviderSvc)
		}

	}

	var sessionSvc *session.Service
	if sessionStore != nil {
		// @-mention resolution needs the workspace roster; both stores present or
		// mentions fall back to agents only.
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
			ContextCfg:         cfg.Context,
			IndexerCfg:         cfg.Indexer,
			MappingCfg:         cfg.Mapping,
			Memories:           memoryStore,
			KPIs:               kpiStore,
			Actions:            sessionActionStore,
			Workspace:          sessionWorkspace,
		})
		// Human-in-the-loop loop closer: an agent that asks a question parks its
		// task on the clarification chat; answering there re-dispatches the task.
		if boardTaskStore != nil && boardDispatcher != nil {
			// repositorySvc records the answer on the task itself, so later runs
			// read what was settled instead of asking the human again.
			sessionSvc.SetAnswerResumer(boardapp.NewAnswerResumer(boardTaskStore, boardDispatcher, repositorySvc))
		}
		if attachmentSvc != nil {
			// Links attachment ids onto persisted user messages and enriches
			// history reads; guarded so a nil *Service never lands in the
			// interface field as a typed non-nil.
			sessionSvc.SetAttachments(attachmentSvc)
		}
		if taskChatWorkspaces != nil {
			// A chat bound to a board task works in that task's own branch
			// checkout instead of the shared mirror clone, so what the agent
			// changes can actually reach the task's pull request.
			sessionSvc.SetTaskWorkspaces(taskChatWorkspaces)
		}
		if hostChatExecutor != nil {
			// Chat with an agent on a host-executed provider goes to the local
			// CLI, exactly as its board tasks already do. Without this the turn
			// fell through to the HTTP client, which has no base URL for a
			// provider that is a binary — the "unsupported protocol scheme"
			// failure this wiring exists to end.
			//
			// Guarded because a nil *claudecode.Executor in a non-nil interface
			// would report Supports() == false but still be non-nil, and the
			// session service's own nil check would then be the only thing
			// standing between a cloud pod and a confusing error.
			sessionSvc.SetChatExecutor(hostChatExecutor)
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

	// The embedding map reads the same chunk tables the indexer writes; it needs
	// no LLM and no indexer config, only Postgres.
	var embedMapSvc *embedmap.Service
	if embedMapStore != nil {
		embedMapSvc = embedmap.New(embedMapStore)
		// So the map can LABEL a source built with a different embedding model
		// rather than draw it as though it were comparable. Deliberately a
		// label and not a refusal, unlike code search: PCA drops rows whose
		// length does not match the modal one, so a model change would quietly
		// halve the cloud and re-derive its axes from the survivors — a picture
		// that looks like a finding. A person reading a map can act on a
		// warning; a wrong search ranking is indistinguishable from a right one.
		if llmProviderSvc != nil {
			embedMapSvc.SetEmbeddingResolver(llmProviderSvc)
		}
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
		HostingSvc:        e.hostingSvc,
		RepoDependencySvc: e.repoDependencySvc,
		VercelOpsSvc:      e.vercelOpsSvc,
		GCloudOpsSvc:      e.gcloudOpsSvc,
		InitiativeSvc:     initiativeSvc,
		WorkspaceSvc:      workspaceSvc,
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
		// Nil unless the Claude Code executor registered, in which case no /mcp
		// route is mounted at all.
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
	// The same overrides boot applies. Skipping them let a reload overwrite the
	// values that only exist in the environment — the DSN and the API key —
	// with whatever the file said.
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
			// The boot-time cipher, not a fresh NewCipherFromEnv: reload runs from
			// a request goroutine long after MCP_SECRETS_KEY left the environment,
			// so re-deriving here would fail and silently drop every stored MCP
			// secret from the reloaded configs.
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

// deployAppResolver is the app guard for the device tools: it answers "which
// package is this repository's build for this environment, and where is the
// artifact" from the deploy target a human configured.
//
// It is read-only by construction. The tools can ask what they are allowed to
// open; nothing on this path can change the answer, which is the whole point —
// a guard an agent could write to would be a formality.
// mobileConfigsOf turns registrations into what the session pool drives.
//
// Devices with no UDID are dropped, and that rule holds across all three kinds
// for the same reason: the UDID is allocated by whoever attaches the device —
// the bridge's loopback port for a phone, the console port for an emulator —
// so a registration without one has never been reached. A session pointed at an
// empty udid is one Appium would satisfy with "whatever adb lists first", which
// on a host with several devices is a different one than the operator
// registered. (A simulator is the exception that proves it: its UDID is known
// at registration, so it is never in this state.)
func mobileConfigsOf(devices []domain.MobileDevice) []mobiletools.Config {
	out := make([]mobiletools.Config, 0, len(devices))
	for _, d := range devices {
		if !d.Configured() {
			continue
		}
		out = append(out, mobiletools.Config{
			HubURL: d.HubURL,
			// The kind rides along because it is what the session's capability
			// set switches on. Without it a registered simulator would be
			// driven with UiAutomator2 capabilities and fail at session
			// creation with a message about a driver nobody chose.
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
//
// A stored executor is a port.TaskExecutor by the map's own type, and not
// every one of them also answers chat — the local opencode.Executor has no
// ExecuteChat, only its remote counterpart does. The type assertion is that
// difference made explicit rather than assumed: a provider with an executor
// that cannot chat gets the same refusal a provider with none does, instead
// of a panic.
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
