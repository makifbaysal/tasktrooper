// Command runner is the piece of TaskTrooper that lives on a user's own Mac.
//
// It dials OUT to the control plane over WebSocket and keeps that connection
// open as a reverse tunnel. The control plane multiplexes calls for this
// member over it using yamux, opening one stream per call; this program is the
// yamux SERVER side (the control plane is the yamux client — it opens streams,
// this side only ever Accepts them) and answers each stream as an RPC.
//
// The control plane speaks HTTP over those streams — it proxies to this Mac
// with httputil.ReverseProxy — so this program is an http.Server whose listener
// is the yamux session. What it answers is the whole of what a Mac is for now:
//
//	POST /claude.run          run a headless Claude Code session in a workspace
//	                          and stream its output back as NDJSON. The WHOLE
//	                          task happens inside that session — clone, edit,
//	                          build, commit, push — so the call is long-lived,
//	                          incremental and cancellable.
//	POST /workspace.prepare   clone a repository, or fetch and check out a branch.
//	POST /toolchain.detect    read what a prepared checkout pins — the language
//	                          runtimes and versions it declares. The cloud
//	                          cannot answer this: it would be resolving against
//	                          a path that only exists here.
//	POST /embeddings.create   proxy an embeddings request to the embedding engine on this Mac.
//	GET  /mobile.devices      the simulators and emulators attached to this Mac.
//	POST /mobile.boot         bring one up; answer with what Appium is handed.
//	POST /mobile.shutdown     put one back down.
//	ANY  /mobile.appium/…     proxy to the Appium hub on this Mac, verbatim. A
//	                          Linux pod cannot run an iOS simulator, and this is
//	                          how a session in the cloud drives a device here.
//	GET  /preflight.report    what this Mac can and cannot do.
//	POST /cancel              stop a running session by call id.
//
// It used to proxy those streams to an agent-server running beside it on this
// machine, which held a Postgres connection to a per-tenant database. None of
// that is here any more: the backend is one shared cloud deployment, and this
// program is the only thing TaskTrooper runs on a user's computer.
//
// It never listens on a port of its own. It serves HTTP, but only over a
// listener that yields streams the control plane opened on a connection THIS
// process dialled; the only sockets it holds are that outbound WebSocket and
// the loopback connections it opens to this Mac's embedding engine and to the
// Appium hub.
//
// It is started by the TaskTrooper desktop app's supervisor and by nothing
// else. Its configuration is the first line on stdin, and stdin stays open as a
// control channel afterwards. See loadConfig and readControl.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	stdlog "log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// tunnelPath is fixed, not configuration: it is the one endpoint the control
// plane exposes for runners, and TMBaseURL only ever names the host.
const tunnelPath = "/internal/runner/tunnel"

// minBackoff is both the starting delay after the first failed attempt and
// the value reconnects fall back to once a session has proven itself stable.
const minBackoff = 1 * time.Second

// stableAfter is how long a session has to stay up before a subsequent drop
// is treated as a fresh problem rather than a continuation of the last one.
const stableAfter = 60 * time.Second

// defaultMaxBackoff is the ceiling for reconnect backoff when the config
// document does not name one.
const defaultMaxBackoff = 30 * time.Second

// configReadLimit bounds how much this program will read for one line of
// stdin. The config document is a few hundred bytes and a preflight report a
// few kilobytes; anything approaching this is a supervisor bug or a pipe
// someone else is writing to.
const configReadLimit = 1024 * 1024

// wireConfig is the JSON document this program reads as the first line of
// stdin at startup.
//
// Field names are the pairing bundle's where they overlap, so the supervisor
// can forward what the control plane gave it without a translation layer that
// could disagree with itself. The two binary paths are NOT looked up here:
// detection lives in the desktop app's services/detect.ts and nowhere else, and
// a second search on this side with slightly different rules is how a Mac ends
// up running one `claude` and reporting another.
type wireConfig struct {
	TMBaseURL   string `json:"tm_base_url"`
	RunnerToken string `json:"runner_token"`
	// TenantID is the control plane's own name for the tenant, and it is the
	// name the pairing bundle uses. It was `tenant_uid` here for one release, from
	// when a tenant was a Firebase uid; it has been a row with members since.
	TenantID string `json:"tenant_id"`
	// MemberUID is the member this Mac belongs to. A tenant may have several
	// paired Macs, one per member, and the control plane routes a task's run to
	// the Mac of the member it is assigned to.
	MemberUID string `json:"member_uid"`
	// WorkspaceDir is the root every path this program touches must be inside.
	WorkspaceDir string `json:"workspace_dir"`
	// ClaudeBin is optional: a member who works only with their own API keys
	// (the executor) or another CLI has no Claude Code, and claude.run answers
	// not_ready for them the way cursor.run does without cursor_agent_bin.
	ClaudeBin string `json:"claude_bin,omitempty"`
	GitBin    string `json:"git_bin"`
	// EmbeddingsBaseURL is the OpenAI-compatible base for this Mac's bundled
	// embedding engine. Optional: a Mac with no local embedding engine is
	// a common case rather than a misconfiguration — embeddings.create
	// answers not_ready rather than exec'ing a proxy target that was never
	// configured.
	EmbeddingsBaseURL string `json:"embeddings_base_url,omitempty"`
	// EmbeddingModel is the model the whole tenant is pinned to, when this Mac
	// has a local engine at all. A request naming a different one is refused
	// rather than served, because a vector from the wrong model is silently
	// incomparable with every other vector in the index.
	EmbeddingModel string `json:"embedding_model,omitempty"`

	// The mobile toolchain, and the four fields that are OPTIONAL on purpose.
	//
	// Every other path here is required because the app cannot do its job
	// without it. These four are different: a Mac with no Xcode has no
	// simulators, a Mac with no Android SDK has no emulators, and a Mac with no
	// Appium cannot drive either. That is a real and common state, not a
	// misconfiguration, so it is carried as an absence rather than as an empty
	// string standing in for a path — `mobile.devices` reports which half is
	// missing and why, and `mobile.boot` refuses with the same sentence instead
	// of failing on an exec of "".
	//
	// They are PASSED, not looked up, for the same reason `git_bin` is: detection lives in the desktop app's services/detect.ts and
	// nowhere else. A second search here with slightly different rules is how a
	// Mac drives one adb and reports another.
	XcrunBin    string `json:"xcrun_bin,omitempty"`
	ADBBin      string `json:"adb_bin,omitempty"`
	EmulatorBin string `json:"emulator_bin,omitempty"`
	// AppiumBaseURL is the hub `/mobile.appium` proxies to. Loopback only, for
	// the same reason embeddings_base_url is: a base URL naming another host
	// would make this program a general-purpose request forwarder aimed by
	// configuration.
	AppiumBaseURL string `json:"appium_base_url,omitempty"`
	// AppiumBin, set, makes the hub at AppiumBaseURL this runner's to start
	// when an Appium call needs it and to stop once idle (appium_hub.go).
	// Absent, whoever runs a hub there does — the member, by hand.
	AppiumBin string `json:"appium_bin,omitempty"`

	// The two other host-executed CLIs' binaries, optional for the same
	// reason the mobile ones are: a Mac without Cursor or OpenCode installed is
	// a common state, not a misconfiguration, and `models.list` reports which
	// one is missing rather than exec'ing "". Passed, not looked up, for the
	// same reason every other binary here is: detection lives in
	// services/detect.ts and nowhere else.
	//
	// Antigravity is not here: this runner has no antigravity flavor, and an
	// unknown field is refused by DisallowUnknownFields — which is the point:
	// a supervisor sending antigravity_bin fails loudly at startup instead of
	// this Mac quietly accepting a capability it does not implement.
	CursorAgentBin string `json:"cursor_agent_bin,omitempty"`
	OpencodeBin    string `json:"opencode_bin,omitempty"`

	// MaxBackoff is a Go duration string ("30s", "1m"). Optional.
	MaxBackoff string `json:"reconnect_max_backoff,omitempty"`

	// ExecutorBin is the local executor (server/cmd/executor) this runner
	// starts, restarts and forwards agent.run and llm.complete to. Optional:
	// absent, both methods answer not_ready. Passed, not looked up, like every
	// other binary.
	ExecutorBin string `json:"executor_bin,omitempty"`
	// ExecutorDataDir is the executor's own data directory, required with
	// executor_bin. Separate from the local backend's, so nothing an account
	// keeps on this machine mixes with local-mode data.
	ExecutorDataDir string `json:"executor_data_dir,omitempty"`
	// ExecutorPostgresCacheDir is where the index store's Postgres binaries
	// live. Optional: the desktop app sends the local server's own cache so the
	// executor does not download Postgres a second time, and its absence falls
	// back to the executor's default under data_dir.
	ExecutorPostgresCacheDir string `json:"executor_postgres_cache_dir,omitempty"`
	// Providers are the member's own LLM providers, API keys included. Held in
	// memory and handed to the executor on its stdin; they never cross the
	// tunnel and never reach a log line (executor.go).
	Providers []providerConfig `json:"providers,omitempty"`

	// RunnerDataDir is where this runner keeps what it writes for itself: the
	// durable runs' buffers. Optional; absent is the user's cache directory.
	RunnerDataDir string `json:"runner_data_dir,omitempty"`
	// RunBufferMaxBytes caps one run's frame buffer. Optional; 16 MiB.
	RunBufferMaxBytes int64 `json:"run_buffer_max_bytes,omitempty"`

	// Policy is what this runner refuses on behalf of the service it is paired
	// with. Optional: absent keeps every default, and the defaults are strict
	// (policy.go).
	Policy *wirePolicy `json:"policy,omitempty"`
}

// config is the validated form of wireConfig.
type config struct {
	tunnelURL         string
	runnerToken       string
	tenantID          string
	memberUID         string
	workspaceDir      string
	claudeBin         string
	gitBin            string
	embeddingsBaseURL string
	embeddingModel    string
	// The mobile half. Empty means this Mac cannot do that part, which is a
	// state `mobile.devices` reports rather than a failure it hides.
	xcrunBin      string
	adbBin        string
	emulatorBin   string
	appiumBaseURL string
	appiumBin     string
	// The two other host-executed CLIs. Empty means this Mac cannot answer
	// models.list (or, for cursor, cursor.run) for that flavor, which is a
	// state the method reports rather than a failure it hides.
	cursorAgentBin string
	opencodeBin    string
	maxBackoff     time.Duration
	policy         runnerPolicy
	// The local executor. Empty executorBin means this machine runs no
	// executor, which agent.run and llm.complete report as not_ready.
	executorBin              string
	executorDataDir          string
	executorPostgresCacheDir string
	providers                []providerConfig
	runnerDataDir            string
	runBufferBytes           int64
}

// loadConfig reads one JSON document — the FIRST LINE of stdin — and validates
// it.
//
// Not the environment, and this is a security decision rather than a stylistic
// one. On macOS a process running as the same user can read another process's
// full environment block through KERN_PROCARGS2 — `ps eww <pid>` does exactly
// that — so a bearer token passed to a child in its environment is readable by
// any app that user launches, including one they were talked into running. A
// pipe has no such interface. Not flags either, for the older reason: argv IS
// world-readable on macOS, so a token on a command line is a token in every
// `ps` on the machine.
//
// One line rather than "everything until EOF", which is what it used to be.
// stdin is a control channel now: the supervisor pushes the environment
// preflight down the same pipe whenever detection runs again (readControl),
// and the pipe closing is how it asks this program to stop (watchControl).
func loadConfig(line []byte) (config, error) {
	if len(strings.TrimSpace(string(line))) == 0 {
		return config{}, errors.New("no config on stdin (this program is started by the TaskTrooper desktop app, which writes one JSON document to its stdin)")
	}

	var wire wireConfig
	dec := json.NewDecoder(strings.NewReader(string(line)))
	// An unknown field means the supervisor and this binary disagree about the
	// contract. Silently ignoring it is how a renamed field becomes a runner
	// that dials the wrong place with a stale default.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&wire); err != nil {
		return config{}, fmt.Errorf("config on stdin is not the expected JSON document: %w", err)
	}
	if dec.More() {
		return config{}, errors.New("config on stdin has trailing data after the JSON document (expected exactly one)")
	}

	base := strings.TrimSpace(wire.TMBaseURL)
	if base == "" {
		return config{}, errors.New("tm_base_url is required (e.g. https://tasktrooper.ai)")
	}
	tunnelURL, err := toTunnelURL(base)
	if err != nil {
		return config{}, fmt.Errorf("tm_base_url: %w", err)
	}

	// Not trimmed: a token's leading or trailing whitespace, if the control
	// plane minted one that way, is part of the token.
	if wire.RunnerToken == "" {
		return config{}, errors.New("runner_token is required")
	}

	tenant := strings.TrimSpace(wire.TenantID)
	if tenant == "" {
		return config{}, errors.New("tenant_id is required")
	}

	// Required, and this is the field that changed with teams. A runner that
	// attached without one would be a tenant-wide runner: on a tenant with two
	// paired Macs, either could pick up either member's work.
	member := strings.TrimSpace(wire.MemberUID)
	if member == "" {
		return config{}, errors.New("member_uid is required (a paired machine belongs to a member of a tenant, not to the tenant)")
	}

	// Absolute, because every path this program is asked to touch is resolved
	// against it and then required to still be inside it. A relative root would
	// make that containment depend on a working directory nobody set.
	workspace := strings.TrimSpace(wire.WorkspaceDir)
	if workspace == "" {
		return config{}, errors.New("workspace_dir is required (the folder Claude Code sessions work in)")
	}
	if !filepath.IsAbs(workspace) {
		return config{}, fmt.Errorf("workspace_dir %q must be an absolute path", workspace)
	}

	claudeBin, err := optionalBin("claude_bin", wire.ClaudeBin)
	if err != nil {
		return config{}, err
	}
	gitBin := strings.TrimSpace(wire.GitBin)
	if gitBin == "" || !filepath.IsAbs(gitBin) {
		return config{}, errors.New("git_bin is required and must be an absolute path (the desktop app detects it)")
	}

	// Both optional: a Mac with no local embedding engine configured is a
	// normal case rather than a startup error — embeddings.create reports not_ready
	// for it, the way mobile.* reports not_ready for a capability this Mac
	// lacks. PRESENT and wrong is still refused: a base URL that is not
	// loopback would make the proxy a general-purpose request forwarder, same
	// as ever.
	embeddingsBase := strings.TrimSpace(wire.EmbeddingsBaseURL)
	if embeddingsBase != "" {
		if err := checkLoopbackURL(embeddingsBase); err != nil {
			return config{}, fmt.Errorf("embeddings_base_url: %w", err)
		}
		embeddingsBase = strings.TrimRight(embeddingsBase, "/")
	}
	model := strings.TrimSpace(wire.EmbeddingModel)

	// The mobile toolchain. Absent is valid and means "this Mac cannot do that
	// half"; PRESENT and wrong is not, because a relative path here would be
	// resolved against a working directory nobody set — which is the same rule
	// git_bin is held to, applied to a field that may be omitted.
	xcrun, err := optionalBin("xcrun_bin", wire.XcrunBin)
	if err != nil {
		return config{}, err
	}
	adb, err := optionalBin("adb_bin", wire.ADBBin)
	if err != nil {
		return config{}, err
	}
	emulator, err := optionalBin("emulator_bin", wire.EmulatorBin)
	if err != nil {
		return config{}, err
	}
	appium := strings.TrimSpace(wire.AppiumBaseURL)
	if appium != "" {
		if err := checkLoopbackURL(appium); err != nil {
			return config{}, fmt.Errorf("appium_base_url: %w", err)
		}
		appium = strings.TrimRight(appium, "/")
	}
	appiumBin, err := optionalBin("appium_bin", wire.AppiumBin)
	if err != nil {
		return config{}, err
	}
	if appiumBin != "" {
		if appium == "" {
			return config{}, errors.New("appium_bin needs appium_base_url: it is the address this runner starts the hub on")
		}
		if _, err := appiumHubArgs(appium); err != nil {
			return config{}, fmt.Errorf("appium_base_url: %w", err)
		}
	}

	cursorAgent, err := optionalBin("cursor_agent_bin", wire.CursorAgentBin)
	if err != nil {
		return config{}, err
	}
	opencodeBin, err := optionalBin("opencode_bin", wire.OpencodeBin)
	if err != nil {
		return config{}, err
	}

	maxBackoff := defaultMaxBackoff
	if raw := strings.TrimSpace(wire.MaxBackoff); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return config{}, fmt.Errorf("reconnect_max_backoff: %w", err)
		}
		if d < minBackoff {
			return config{}, fmt.Errorf("reconnect_max_backoff %s is below the %s floor", d, minBackoff)
		}
		maxBackoff = d
	}

	policy, err := loadPolicy(wire.Policy)
	if err != nil {
		return config{}, err
	}

	executorBin, err := optionalBin("executor_bin", wire.ExecutorBin)
	if err != nil {
		return config{}, err
	}
	executorData := strings.TrimSpace(wire.ExecutorDataDir)
	if executorData != "" {
		if !filepath.IsAbs(executorData) {
			return config{}, fmt.Errorf("executor_data_dir %q must be an absolute path", executorData)
		}
		executorData = filepath.Clean(executorData)
	}
	if executorBin != "" && executorData == "" {
		return config{}, errors.New("executor_data_dir is required with executor_bin")
	}
	executorPostgres := strings.TrimSpace(wire.ExecutorPostgresCacheDir)
	if executorPostgres != "" {
		if !filepath.IsAbs(executorPostgres) {
			return config{}, fmt.Errorf("executor_postgres_cache_dir %q must be an absolute path", executorPostgres)
		}
		executorPostgres = filepath.Clean(executorPostgres)
	}
	providers, err := checkProviders(wire.Providers)
	if err != nil {
		return config{}, err
	}
	runnerData := strings.TrimSpace(wire.RunnerDataDir)
	if runnerData != "" {
		if !filepath.IsAbs(runnerData) {
			return config{}, fmt.Errorf("runner_data_dir %q must be an absolute path", runnerData)
		}
		runnerData = filepath.Clean(runnerData)
	}
	runBuffer := int64(defaultRunBufferBytes)
	if wire.RunBufferMaxBytes != 0 {
		if wire.RunBufferMaxBytes < minRunBufferBytes || wire.RunBufferMaxBytes > maxRunBufferBytes {
			return config{}, fmt.Errorf("run_buffer_max_bytes %d is outside %d..%d", wire.RunBufferMaxBytes, minRunBufferBytes, maxRunBufferBytes)
		}
		runBuffer = wire.RunBufferMaxBytes
	}

	return config{
		tunnelURL:                tunnelURL,
		runnerToken:              wire.RunnerToken,
		tenantID:                 tenant,
		memberUID:                member,
		workspaceDir:             filepath.Clean(workspace),
		claudeBin:                claudeBin,
		gitBin:                   gitBin,
		embeddingsBaseURL:        embeddingsBase,
		embeddingModel:           model,
		xcrunBin:                 xcrun,
		adbBin:                   adb,
		emulatorBin:              emulator,
		appiumBaseURL:            appium,
		appiumBin:                appiumBin,
		cursorAgentBin:           cursorAgent,
		opencodeBin:              opencodeBin,
		maxBackoff:               maxBackoff,
		policy:                   policy,
		executorBin:              executorBin,
		executorDataDir:          executorData,
		executorPostgresCacheDir: executorPostgres,
		providers:                providers,
		runnerDataDir:            runnerData,
		runBufferBytes:           runBuffer,
	}, nil
}

// optionalBin validates a path that may legitimately be absent.
//
// Absent means the capability that needs it is absent, which is a fact this
// program reports. An empty string standing in for a path is not: it would exec
// "" minutes later, inside a call, with an error naming nothing.
func optionalBin(field, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if !filepath.IsAbs(value) {
		return "", fmt.Errorf("%s %q must be an absolute path (the desktop app detects it; omit the field when this machine has none)", field, value)
	}
	return value, nil
}

// checkLoopbackURL refuses anything that is not a plain loopback HTTP address.
//
// Both things this program proxies to — this Mac's embedding engine and
// Appium — run on the user's own machine, and the whole point of proxying to
// them here is that the request never leaves this Mac. A base URL naming
// another host would turn either method into a general-purpose HTTP client
// that the control plane's configuration could point anywhere, which is a
// request forwarder rather than a proxy to a local process.
func checkLoopbackURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("not a valid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %q (want http:// or https://)", u.Scheme)
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "::1" && host != "localhost" {
		return fmt.Errorf("host %q is not loopback; the program this proxies to runs on this machine and nowhere else", host)
	}
	return nil
}

// toTunnelURL turns the base URL into the WebSocket URL this runner dials:
// http(s) becomes ws(s), and the fixed tunnel path replaces whatever path (if
// any) the base URL had.
func toTunnelURL(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("not a valid URL: %w", err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
		// Already a websocket URL; leave it alone.
	default:
		return "", fmt.Errorf("unsupported scheme %q (want http:// or https://)", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("missing host in %q", base)
	}
	u.Path = tunnelPath
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func main() {
	configureLogger()

	stdin := bufio.NewReaderSize(os.Stdin, configReadLimit)
	line, err := readLine(stdin)
	if err == nil {
		var cfg config
		cfg, err = loadConfig(line)
		if err == nil {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			// The supervisor reading this process's stdout is also what dies
			// when the app does, and Go's default for a write to a broken
			// stdout is to die of SIGPIPE — in the middle of the drain below,
			// which is the one moment the process groups it is reaping need
			// this process alive. The log lines are lost either way.
			signal.Ignore(syscall.SIGPIPE)

			// Anything a previous, unclean exit left behind. Every ordinary
			// ending removes a run's MCP config through a deferred call; a
			// SIGKILL or a lost power cable cannot, and what it leaves is a
			// bearer token in a file. See mcp.go.
			sweepMCPConfigs(os.TempDir())
			// The same ending leaves worse behind for a release: a file of
			// signing material, and on macOS a keychain in the user's search
			// list holding a distribution identity. See mobile_release.go.
			sweepReleaseSecrets(os.TempDir())
			sweepReleaseKeychains(securityBin)
			// And the one thing a release leaves inside the WORKSPACE: its
			// generated wrapper, which the next `git add -A` in that checkout
			// would commit.
			sweepReleaseWrappers(cfg.workspaceDir)

			state := newState()
			state.runs = newRunRegistry(runsDir(cfg), cfg.runBufferBytes, runRetention)
			state.hub = newAppiumHub(cfg, defaultHubTimings)
			state.hub.resume()
			state.seedEmbeddingsBaseURLIfUnset(cfg.embeddingsBaseURL)
			state.executor = newExecutorSupervisor(cfg, state.getEmbeddingsBaseURL, defaultExecutorTimings)
			state.executor.start()
			// The rest of stdin, for as long as this process lives, and its
			// closing is a shutdown request — see watchControl.
			ctx = watchControl(ctx, stdin, state)
			// The hub goes down beside the drain rather than after it: the
			// drain cancels every proxied call anyway, and the supervisor's
			// grace (child.ts) covers the drain, not the drain plus a hub.
			context.AfterFunc(ctx, state.hub.close)
			// The executor too, and for the same reason: its runs are already
			// cancelled by the drain, and its own stop fits inside the
			// supervisor's grace only when it runs beside the drain.
			context.AfterFunc(ctx, state.executor.close)
			// Durable runs outlive a tunnel session, not the process: they are
			// cancelled beside the drain, and reaped before this returns.
			context.AfterFunc(ctx, state.runs.shutdown)

			runErr := run(ctx, cfg, state, defaultTimings)
			state.runs.close()
			state.hub.close()
			state.executor.close()
			if runErr != nil {
				log.Fatal().Err(runErr).Msg("runner exiting")
			}
			return
		}
	}
	// stderr, not the JSON logger: a configuration error is addressed to
	// whoever is looking at why the process would not start, and the supervisor
	// surfaces this line verbatim.
	fmt.Fprintln(os.Stderr, "runner: "+err.Error())
	os.Exit(1)
}

// errLineTooLong is a line on stdin longer than configReadLimit.
var errLineTooLong = fmt.Errorf("a line on stdin exceeds %d bytes", configReadLimit)

// readLine reads one newline-terminated line, refusing one longer than
// configReadLimit rather than growing a buffer another process controls.
func readLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		return nil, errLineTooLong
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("reading from stdin: %w", err)
	}
	if len(line) == 0 && errors.Is(err, io.EOF) {
		return nil, errors.New("stdin closed before the config document arrived")
	}
	// ReadSlice returns a slice into the reader's own buffer, which the next
	// read overwrites.
	return append([]byte(nil), line...), nil
}

// controlMessage is a line on stdin after the first one.
//
// Tagged, because the same descriptor already carried the configuration and
// this program has to be able to tell one from the other. There are two types
// today and the envelope exists so a third does not need a new pipe.
type controlMessage struct {
	Type string `json:"type"`
	// Report is the desktop app's environment preflight, passed through
	// verbatim. This program does not probe: detection lives in the app's
	// services/detect.ts, which is the one place that knows how this Mac's
	// tools are found, and a second implementation here would drift into
	// reporting a different machine than the one the user is looking at.
	Report json.RawMessage `json:"report"`
	// EmbeddingsBaseURL updates where embeddings.create proxies to.
	//
	// The embedder binds an OS-assigned port and is never re-spawned with a
	// fresh stdin document once the runner is already attached, so a restart —
	// a crash, or the embedder losing a race with the runner at startup — gives
	// it a different port than the one the runner was configured with. Without
	// this, embeddings would stay broken until the whole session reconnected.
	EmbeddingsBaseURL string `json:"embeddings_base_url,omitempty"`
}

// watchControl reads the control channel for the life of the process and
// returns a context that ends when the channel does.
//
// The channel closing IS a shutdown request, on every platform, and it gets
// exactly the drain SIGTERM gets. Two reasons, both about the same failure — a
// runner left alive with nobody watching it:
//
//   - On Windows there is no SIGTERM to send. A force-kill there is
//     TerminateProcess, which runs no drain at all, so the supervisor asks
//     for a stop by ending stdin.
//   - On every OS, the desktop app dying closes the pipe, and that is the only
//     thing that tells this process it is now an orphan whose sessions nobody
//     can see or stop.
func watchControl(parent context.Context, r *bufio.Reader, state *state) context.Context {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		defer cancel()
		readControl(ctx, r, state)
		if parent.Err() == nil {
			log.Info().Msg("control channel closed; shutting down")
		}
	}()
	return ctx
}

// readControl consumes stdin until it closes or ctx ends.
//
// A malformed line is logged and skipped rather than fatal: the tunnel is the
// valuable thing this process holds, and tearing it down because the supervisor
// sent a line this build does not understand would make every future control
// message a compatibility hazard. An over-long line is skipped for the same
// reason — and read past rather than left in the pipe, so the closing that
// comes after it is still seen.
func readControl(ctx context.Context, r *bufio.Reader, state *state) {
	for {
		if ctx.Err() != nil {
			return
		}
		line, err := readLine(r)
		if errors.Is(err, errLineTooLong) {
			log.Warn().Int("limit", configReadLimit).Msg("ignoring a control line longer than the limit")
			if err := discardLine(r); err != nil {
				log.Debug().Err(err).Msg("control channel closed")
				return
			}
			continue
		}
		if err != nil {
			log.Debug().Err(err).Msg("control channel closed")
			return
		}
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var msg controlMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			log.Warn().Err(err).Msg("ignoring a control line that is not JSON")
			continue
		}
		switch msg.Type {
		case "preflight":
			state.setPreflight(msg.Report)
			log.Debug().Msg("preflight report updated")
		case "embeddings-base-url":
			trimmed := strings.TrimRight(msg.EmbeddingsBaseURL, "/")
			if err := checkLoopbackURL(trimmed); err != nil {
				log.Warn().Err(err).Msg("ignoring an embeddings-base-url update that is not loopback")
				continue
			}
			state.setEmbeddingsBaseURL(trimmed)
			if ex := state.executorSupervisor(); ex != nil {
				ex.setEmbeddingsBaseURL(trimmed)
			}
			log.Debug().Str("url", trimmed).Msg("embeddings base url updated")
		default:
			log.Warn().Str("type", msg.Type).Msg("ignoring an unknown control message")
		}
	}
}

// discardLine reads to the end of the current line and drops it.
func discardLine(r *bufio.Reader) error {
	for {
		_, err := r.ReadSlice('\n')
		if !errors.Is(err, bufio.ErrBufferFull) {
			return err
		}
	}
}

// state is what outlives any one tunnel session.
//
// The preflight report arrives on stdin at times unrelated to the tunnel, and a
// reconnect must not lose it: a Mac that dropped its connection for thirty
// seconds should not come back claiming it does not know what it has installed.
//
// embeddingsBaseURL is here rather than frozen into config for the same
// reason: the embedder binds an OS-assigned port and is never re-spawned with
// a fresh stdin document once the runner is already attached, so its address
// can change under a live session (see the "embeddings-base-url" control
// message above). newRunnerServer seeds it from config once, at startup; every
// call after that reads it from here.
//
// hub is here because an Appium session the cloud holds must survive a
// thirty-second reconnect, which a hub owned by one tunnel session would not.
// It is set once, before anything reads it, and nil when this runner starts no
// hub.
type state struct {
	mu                sync.Mutex
	preflight         json.RawMessage
	embeddingsBaseURL string
	hub               *appiumHub
	// executor is set once at startup, like hub, and nil when the desktop app
	// sent no executor_bin.
	executor *executorSupervisor
	// runs are the durable runs, which outlive any one tunnel session. A
	// fresh state keeps their buffers in memory; main gives it the disk.
	runs *runRegistry
}

func newState() *state { return &state{runs: newRunRegistry("", defaultRunBufferBytes, runRetention)} }

func (s *state) appiumHub() *appiumHub {
	if s == nil {
		return nil
	}
	return s.hub
}

func (s *state) executorSupervisor() *executorSupervisor {
	if s == nil {
		return nil
	}
	return s.executor
}

func (s *state) setPreflight(report json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.preflight = append(json.RawMessage(nil), report...)
}

func (s *state) getPreflight() json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.preflight
}

func (s *state) setEmbeddingsBaseURL(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.embeddingsBaseURL = url
}

// seedEmbeddingsBaseURLIfUnset sets the initial value once, at startup, and
// is a no-op on every reconnect after — a control message may already have
// moved this on, and a fresh connectAndServe re-seeding it from the (stale)
// startup config would silently undo a live update.
func (s *state) seedEmbeddingsBaseURLIfUnset(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.embeddingsBaseURL == "" {
		s.embeddingsBaseURL = url
	}
}

func (s *state) getEmbeddingsBaseURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.embeddingsBaseURL
}

// configureLogger sets up JSON logging on stdout.
//
// Not switchable, and there is no ConsoleWriter: the only consumer of this
// program's output is the desktop app's supervisor, which parses these lines
// into state ("tunnel attached", "tunnel detached", the reconnect warning) and
// renders them itself. A human-readable encoding here would be a format nobody
// reads that the supervisor then has to scrape.
func configureLogger() {
	zerolog.TimeFieldFormat = time.RFC3339
	log.Logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
}

// yamuxLogger is the logger yamux writes through, so the mux's own lines
// arrive as records like every other line this program emits instead of as raw
// text on stderr. The flags are zero deliberately: zerolog stamps each record
// with the time, and the stdlib logger's own timestamp would be a second one
// inside the message.
func yamuxLogger(ctx context.Context) *stdlog.Logger {
	return stdlog.New(yamuxLogWriter{ctx: ctx}, "", 0)
}

// yamuxLogWriter turns one line from yamux into one record from this program.
//
// yamux writes from its own read, write and keepalive goroutines, so this must
// be safe for concurrent use: it keeps no mutable state, and zerolog's logger
// is safe to share. It must also never block and never panic — a logger that
// stalls inside the mux stalls the tunnel, and one that panics takes the
// process down over a log line — so it does exactly one unbuffered write per
// call, takes no lock of its own, recovers anything the sink throws, and
// always reports a complete write so the stdlib logger never retries or
// reports a logging failure back into yamux.
type yamuxLogWriter struct{ ctx context.Context }

func (w yamuxLogWriter) Write(p []byte) (int, error) {
	defer func() { _ = recover() }()
	if level, msg, ok := yamuxRecord(string(p), w.ctx.Err() != nil); ok {
		log.WithLevel(level).Msg(msg)
	}
	return len(p), nil
}

// yamuxRecord maps one yamux log line onto a level and a message, and reports
// whether there is anything to log at all.
//
// yamux tags its lines [ERR] and [WARN], and takes a dimmer view of a closing
// connection than this program does. Tearing the session down IS how this
// program shuts down — Session.Close on a cancelled context — and the mux then
// finding the connection it was reading from gone is the expected consequence
// of that, not a fault. Logging "[ERR] yamux: Failed to read header: failed to
// get reader: context canceled" every time a user quits the app teaches them
// that errors in this log mean nothing, and this is the log they will be
// reading when something is genuinely wrong. So once shutdown has been
// initiated, yamux's complaints are debug.
//
// While the session is live they are worth exactly what they say: a keepalive
// that failed or a frame that could not be sent is the reason the tunnel is
// about to drop, and it is the line someone will want when they ask why this
// Mac stopped running their tasks. Those keep their level.
//
// An untagged line is yamux narrating itself; debug is generous enough. The
// message keeps yamux's own "yamux: " prefix, so a record needs no field to
// say where it came from and the supervisor needs no case for it.
func yamuxRecord(line string, shuttingDown bool) (zerolog.Level, string, bool) {
	msg := strings.TrimSpace(line)
	if msg == "" {
		return zerolog.NoLevel, "", false
	}

	level := zerolog.DebugLevel
	switch {
	case strings.HasPrefix(msg, "[ERR]"):
		msg = strings.TrimSpace(strings.TrimPrefix(msg, "[ERR]"))
		if !shuttingDown {
			level = zerolog.ErrorLevel
		}
	case strings.HasPrefix(msg, "[WARN]"):
		msg = strings.TrimSpace(strings.TrimPrefix(msg, "[WARN]"))
		if !shuttingDown {
			level = zerolog.WarnLevel
		}
	}
	return level, msg, true
}

// authError marks a dial rejected on credentials rather than connectivity.
// Retrying it changes nothing until the Mac is re-paired, so run treats it as
// fatal instead of feeding it back into the backoff loop.
type authError struct{ status int }

func (e *authError) Error() string {
	return fmt.Sprintf("control plane rejected the connection (HTTP %d) — this machine's runner token is wrong or revoked; re-pair it, retrying will not help", e.status)
}

// timings is the reconnect loop's clock. run takes it as an argument rather
// than reading the two constants directly so that the reconnect policy — the
// part of this program whose failure mode is a Mac that quietly stops running
// its member's tasks — can be exercised without a minute of wall clock per
// case. main passes defaultTimings; nothing in the program varies them.
type timings struct {
	minBackoff  time.Duration
	stableAfter time.Duration
}

var defaultTimings = timings{minBackoff: minBackoff, stableAfter: stableAfter}

// run dials the tunnel and serves it, reconnecting with exponential backoff
// until ctx is cancelled. It returns nil only on a clean shutdown; any other
// return (in practice, only *authError) is fatal and the caller exits 1.
func run(ctx context.Context, cfg config, state *state, t timings) error {
	backoff := t.minBackoff
	for {
		if ctx.Err() != nil {
			return nil
		}

		up, err := connectAndServe(ctx, cfg, state)

		if ctx.Err() != nil {
			log.Info().Msg("shut down cleanly")
			return nil
		}

		var authErr *authError
		if errors.As(err, &authErr) {
			return authErr
		}

		// A session that stood for a while was working; whatever just broke
		// it is a new problem, not a continuation of the last one, so give
		// it a fresh start rather than the tail of a backoff run from
		// before it connected. up is how long the tunnel was ATTACHED, not
		// how long the attempt took: a dial that hangs for a minute and then
		// fails has proven nothing, and counting it as a stable session would
		// pin the delay at the floor for exactly the failure that most needs
		// it to grow.
		if up >= t.stableAfter {
			backoff = t.minBackoff
		}

		log.Warn().Err(err).Dur("uptime", up).Dur("retry_in", backoff).Msg("tunnel session ended, reconnecting")

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil
		}
		backoff = t.nextBackoff(backoff, cfg.maxBackoff)
	}
}

// nextBackoff doubles cur, capped at max and floored at t.minBackoff so a
// caller can never pass in a value that shrinks the delay.
func (t timings) nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		next = max
	}
	if next < t.minBackoff {
		next = t.minBackoff
	}
	return next
}

// connectAndServe dials the control plane once, attaches yamux to the
// resulting connection, and answers streams from it until the session ends.
//
// It returns how long the tunnel was attached — zero if it never attached, so
// a dial that fails, however slowly, is never mistaken for a session that
// worked.
func connectAndServe(ctx context.Context, cfg config, state *state) (time.Duration, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+cfg.runnerToken)
	header.Set("X-Runner-Tenant", cfg.tenantID)
	// The member, alongside the tenant, because that is what the control plane
	// routes on: a task's runs go to the Mac of the member it is assigned to.
	header.Set("X-Runner-Member", cfg.memberUID)

	log.Debug().Str("url", cfg.tunnelURL).Msg("dialing control plane")
	wsConn, resp, err := websocket.Dial(ctx, cfg.tunnelURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return 0, &authError{status: resp.StatusCode}
		}
		return 0, fmt.Errorf("dial: %w", err)
	}

	// yamux frames routinely exceed coder/websocket's default 32KiB
	// per-message read limit; disabling it lets yamux's own framing, not
	// this library's, decide message boundaries. NetConn below sets this too,
	// but it is set explicitly here so the reason is on record.
	wsConn.SetReadLimit(-1)

	netConn := websocket.NetConn(ctx, wsConn, websocket.MessageBinary)

	// Everything else about yamux stays default; only where it logs changes.
	// Its default is raw text on stderr, which the supervisor can do nothing
	// with but pass through verbatim into the log the user reads.
	yamuxCfg := yamux.DefaultConfig()
	yamuxCfg.LogOutput = nil // one of Logger and LogOutput may be set, never both
	yamuxCfg.Logger = yamuxLogger(ctx)

	session, err := yamux.Server(netConn, yamuxCfg)
	if err != nil {
		wsConn.Close(websocket.StatusInternalError, "yamux setup failed")
		return 0, fmt.Errorf("yamux server: %w", err)
	}

	// Tear the session (and with it, the websocket) down the moment ctx is
	// cancelled, whether or not a stream is currently blocked in Accept.
	// Session.Close closes the underlying NetConn with a normal-closure
	// frame, which is what "close cleanly" means here.
	stopOnShutdown := context.AfterFunc(ctx, func() { _ = session.Close() })
	defer stopOnShutdown()

	// Requests belong to this session and end with it. A durable run does
	// not: once started it is the registry's (runs.go), and only its stream
	// to this session's caller ends here.
	sessionCtx, endSession := context.WithCancel(ctx)
	defer endSession()

	// The control plane reaches this Mac with httputil.ReverseProxy over an
	// http.Transport, so what arrives on a stream is genuine HTTP/1.1 framing.
	// yamux.Session already satisfies net.Listener, which makes "serve the
	// tunnel" exactly one http.Server whose listener is the session.
	//
	// Nothing about the security posture moves. This binds no socket: the
	// listener yields streams the control plane opened over a connection THIS
	// process dialled, and rules_test.go still forbids every net.Listen.
	runner := newRunnerServer(cfg, state)
	srv := &http.Server{
		Handler:           runner.handler(),
		ReadHeaderTimeout: requestHeaderTimeout,
		IdleTimeout:       idleTimeout,
		// Every request context descends from this one, so a session ending
		// ends its requests: the queued calls, the non-durable ones, and the
		// streams of durable runs, which go on without them.
		BaseContext: func(net.Listener) context.Context { return sessionCtx },
		// net/http's own complaints, routed into this program's JSON log. Its
		// default is the stdlib logger writing raw text to stderr, which the
		// supervisor can only pass through unparsed — the same problem
		// yamuxLogger exists to solve.
		ErrorLog: httpErrorLog(),
	}

	var streams atomic.Int64
	srv.ConnState = func(_ net.Conn, cs http.ConnState) {
		if cs == http.StateNew {
			log.Debug().Int64("stream", streams.Add(1)).Msg("stream accepted")
		}
	}

	log.Info().Msg("tunnel attached")
	attachedAt := time.Now()
	defer func() {
		// Cancel first, then wait, then close. Cancelling gives every request
		// still in flight — a non-durable run among them — the chance to kill
		// its process group and be reaped; closing first would tear the
		// connections down and leave those children behind. Durable runs are
		// not waited for: they go on, and a later session attaches to them.
		endSession()
		runner.drain()
		_ = srv.Close()
		log.Info().Int64("streams", streams.Load()).Msg("tunnel detached")
	}()

	// Serve returns when Accept fails, which is what session.Close() on a
	// cancelled context produces.
	err = srv.Serve(session)
	if ctx.Err() != nil {
		return time.Since(attachedAt), nil
	}
	return time.Since(attachedAt), fmt.Errorf("serving the tunnel: %w", err)
}

// httpErrorLog routes net/http's own log lines into this program's records.
//
// They are debug: "superfluous response.WriteHeader", a request whose
// connection died mid-header. None of them is a fault a user acts on, and the
// failure that IS worth seeing — a session that ended — arrives as the Serve
// error above.
func httpErrorLog() *stdlog.Logger {
	return stdlog.New(httpLogWriter{}, "", 0)
}

type httpLogWriter struct{}

func (httpLogWriter) Write(p []byte) (int, error) {
	// Same contract as yamuxLogWriter: never block, never panic, always report
	// a complete write, because a logger that misbehaves inside net/http
	// misbehaves inside the tunnel.
	defer func() { _ = recover() }()
	if msg := strings.TrimSpace(string(p)); msg != "" {
		log.Debug().Msg("http: " + msg)
	}
	return len(p), nil
}
