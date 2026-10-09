package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/executorapi"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/mcp"
	gitadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/git"
	"github.com/makifbaysal/tasktrooper/server/internal/application/config"
	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	execapp "github.com/makifbaysal/tasktrooper/server/internal/application/executor"
	"github.com/makifbaysal/tasktrooper/server/internal/application/localindex"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/proctree"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/resources"
)

// ListeningPrefix starts the one line the executor writes on stdout; the
// parent reads the address from it.
const ListeningPrefix = "EXECUTOR_LISTENING "

// Version is stamped with -ldflags "-X …/platform/executor.Version=…"; a
// build without it reports its VCS revision.
var Version = ""

const (
	readHeaderTimeout   = 30 * time.Second
	idleTimeout         = 5 * time.Minute
	processGrace        = 3 * time.Second
	defaultEmbedTimeout = 60 * time.Second
)

// ConfigureLogger sends logs to stderr: stdout carries exactly the
// EXECUTOR_LISTENING line the parent parses.
func ConfigureLogger(debugLevel bool) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if debugLevel {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
	if isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd()) {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
		return
	}
	log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
}

type Server struct {
	addr       string
	http       *http.Server
	svc        *execapp.Service
	tools      localTools
	indexStore *indexStoreOpener
	done       chan struct{}
}

// Options are the seams a test replaces; production passes the zero value.
type Options struct {
	Remote port.RemoteToolConnector
}

// Start binds the listener, prints the EXECUTOR_LISTENING line on stdout and
// serves until Shutdown.
func Start(cfg Config, stdout io.Writer, opts Options) (*Server, error) {
	if err := loadPromptLibrary(); err != nil {
		return nil, err
	}
	appCfg, err := config.Parse(resources.ConfigYAML)
	if err != nil {
		return nil, fmt.Errorf("embedded config: %w", err)
	}
	if err := os.MkdirAll(cfg.WorkspaceRoot, 0o755); err != nil {
		return nil, fmt.Errorf("workspace_root: %w", err)
	}
	if cfg.DataDir != "" {
		if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
			return nil, fmt.Errorf("data_dir: %w", err)
		}
	}

	llmClient, providers := buildProviders(cfg.Providers, appCfg.LLM.Timeout)
	tools := buildLocalTools(appCfg, cfg.WorkspaceRoot)
	remote := opts.Remote
	if remote == nil {
		remote = mcp.RemoteConnector{}
	}
	maxToolOutput := appCfg.Tools.MaxToolOutputChars
	if maxToolOutput <= 0 {
		maxToolOutput = 16000
	}
	history := appcontext.Budget{
		MaxTokens:          appCfg.Context.MaxTokens,
		ReserveOutput:      appCfg.Context.ReserveOutput,
		SummarizeThreshold: appCfg.Context.SummarizeThreshold,
		KeepRecentMessages: appCfg.Context.KeepRecentMessages,
	}
	if history.KeepRecentMessages <= 0 {
		history.KeepRecentMessages = 10
	}
	index, indexStore, embedder := buildLocalIndex(cfg, appCfg)
	svc := execapp.NewService(execapp.Deps{
		LLM:            llmClient,
		Providers:      providers,
		WorkspaceRoot:  cfg.WorkspaceRoot,
		WorkspaceTools: tools.workspace,
		HostTools:      tools.host,
		Remote:         remote,
		Index:          index,
		Embeddings:     embedder,
		Git:            gitadapter.NewCheckout(),
		Limits: execapp.Limits{
			MaxIterations:      appCfg.LLM.MaxIterations,
			TaskMaxIterations:  appCfg.LLM.TaskMaxIterations,
			MaxToolOutputChars: maxToolOutput,
			RunTokenCap:        appCfg.LLM.RunMaxTotalTokens,
			History:            history,
		},
	})
	handler := executorapi.NewHandler(svc, executorapi.Options{
		Token:   cfg.Token,
		Version: version(),
		Secrets: append(providerSecrets(cfg.Providers), cfg.Token),
	})

	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		tools.close()
		return nil, fmt.Errorf("listen: %w", err)
	}
	addr := listener.Addr().String()
	if _, err := fmt.Fprintf(stdout, "%shttp://%s\n", ListeningPrefix, addr); err != nil {
		_ = listener.Close()
		tools.close()
		return nil, fmt.Errorf("announce the listener: %w", err)
	}
	if f, ok := stdout.(*os.File); ok {
		_ = f.Sync()
	}

	s := &Server{
		addr: addr,
		http: &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: readHeaderTimeout,
			IdleTimeout:       idleTimeout,
		},
		svc:        svc,
		tools:      tools,
		indexStore: indexStore,
		done:       make(chan struct{}),
	}
	go func() {
		defer close(s.done)
		if err := s.http.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("executor listener stopped")
		}
	}()
	log.Info().Str("addr", addr).Int("providers", len(cfg.Providers)).Str("workspace_root", cfg.WorkspaceRoot).
		Bool("embeddings", cfg.EmbeddingsBaseURL != "").Msg("executor listening")
	return s, nil
}

func (s *Server) Addr() string { return s.addr }

// Shutdown cancels every run — each still gets its done frame out — and waits
// for the streams to end, up to ctx; then it stops what the runs left behind.
func (s *Server) Shutdown(ctx context.Context) error {
	s.svc.Shutdown()
	err := s.http.Shutdown(ctx)
	if err != nil {
		_ = s.http.Close()
	}
	<-s.done
	proctree.Default.KillAll(processGrace)
	s.tools.close()
	if s.indexStore != nil {
		s.indexStore.Close()
	}
	return err
}

// buildLocalIndex is always there to answer, if only to say why it cannot:
// without an embedding engine (embeddings_base_url, or a later
// /exec/embeddings.set) there is nothing to embed with, and without data_dir
// no place for the store.
func buildLocalIndex(cfg Config, appCfg *domain.Config) (*localindex.Service, *indexStoreOpener, *localEmbedder) {
	timeout := appCfg.Embedding.RequestTimeout
	if timeout <= 0 {
		timeout = defaultEmbedTimeout
	}
	embedder := newLocalEmbedder(cfg.EmbeddingsBaseURL, cfg.EmbeddingsSource, timeout)
	deps := localindex.Deps{
		Embedder: embedder,
		Tools:    indexTools(appCfg),
		Config: localindex.Config{
			Indexer:   appCfg.Indexer,
			Graph:     appCfg.Graph,
			Mapping:   appCfg.Mapping,
			Embedding: appCfg.Embedding,
		},
	}
	var opener *indexStoreOpener
	if cfg.DataDir != "" {
		opener = newIndexStoreOpener(cfg.DataDir, cfg.PostgresCacheDir)
		deps.Opener = opener
	}
	return localindex.NewService(deps), opener, embedder
}

func loadPromptLibrary() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("prompt library: %v", r)
		}
	}()
	prompt.Default()
	return nil
}

func version() string {
	if Version != "" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	v := info.Main.Version
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && len(setting.Value) >= 12 {
			return "dev+" + setting.Value[:12]
		}
	}
	if v == "" || v == "(devel)" {
		return "dev"
	}
	return v
}
