package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"
)

// The MCP configuration one run is given — how a Claude Code session on this
// Mac reaches TaskTrooper's own tools.
//
// When a task ran in the cloud, agent-server mounted its /mcp endpoint on
// loopback and handed the CLI session a per-run token. That is how an agent
// ticks an acceptance criterion, records a verdict, moves a board card. Runs
// happen on the user's Mac now, so the control plane sends the public endpoint
// and a per-run token WITH the call, and this file turns them into the only
// thing the CLI will read: a configuration file.
//
// Three properties, and every line below is one of them:
//
//  1. **The token goes in the FILE, never in argv.** argv is world-readable on
//     macOS — it is already why the prompt travels on stdin — and a bearer
//     token on a command line is a credential in every `ps` on the machine for
//     the length of the run.
//  2. **The file lives exactly as long as the run.** A per-run directory with
//     an unguessable name, mode 0700, the file inside it 0600, and a removal
//     that happens however the run ends: finished, failed, cancelled, or the
//     tunnel dropping under it. A token file that outlives its run is a
//     credential left on somebody's disk, and `sweepMCPConfigs` is what clears
//     the one exit that cannot run a deferred function — a SIGKILL.
//  3. **`--strict-mcp-config` goes on EVERY run**, including one the control
//     plane sent no `mcp` for. Without it the CLI also loads the servers the
//     user configured for themselves, and somebody's personal MCP servers
//     turning up inside a work task is both a surprise and a way for a task to
//     reach something nobody scoped it to. A run with no `mcp` therefore gets
//     a config file with no servers in it, which is not a default invented
//     here: it is "no TaskTrooper tools" written down.
//
// It is the ONE thing this program puts on disk outside the workspace root.
// The workspace is a folder the user opens in Finder and may keep on a
// cloud-synced volume — the desktop app warns about that rather than refusing
// it — and neither a sync client nor a Finder window is somewhere a bearer
// token belongs. The containment rule it appears to bend is about paths a
// CALLER names, and no caller names this one.

// mcpDirPrefix is shared by the writer and the sweeper: it is how a directory
// left behind by a killed runner is recognised as ours.
const mcpDirPrefix = "tasktrooper-mcp-"

// mcpFileName is the file inside that directory. Named for what it is, because
// it appears in argv and in the CLI's own errors.
const mcpFileName = "mcp.json"

// mcpToolPrefix is how the CLI names a tool that came from an MCP server:
// `mcp__<server_name>__<tool>`. It is here rather than in session.go because it
// is the CLI's naming convention for THIS file's servers, and because
// claudeArgs has to tell the two kinds of tool name apart — `--tools` filters
// the built-in surface and ignores these outright.
const mcpToolPrefix = "mcp__"

// mcpServerName is the grammar for `server_name`. It becomes a JSON object key
// in a file the CLI parses and the middle of every tool name the model is
// asked to call (`mcp__<server_name>__<tool>`), so it is a plain identifier and
// nothing else — no dots, no slashes, no leading dash.
var mcpServerName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// mcpToken says what a bearer token may not contain rather than what it is: no
// whitespace, no control characters, nothing outside printable ASCII. The CLI
// sends this value as an HTTP header, and a newline in a header value is a
// header injection.
var mcpToken = regexp.MustCompile(`^[\x21-\x7E]+$`)

// mcpTokenMaxLen bounds it. Signed tokens are long; nothing is this long, and
// the value ends up in a file written once per run.
const mcpTokenMaxLen = 8192

// isLoopbackHost reports whether host (already stripped of any port by
// url.URL.Hostname) is one only this machine can reach. It mirrors
// desktop/src/main/edition.ts's identical helper and the control plane's
// isLoopbackBaseURL — three call sites, one rule.
func isLoopbackHost(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// mcpParams is the optional `mcp` object on claude.run.
//
// Absent is valid and means "this run gets no TaskTrooper tools" — a run
// preparing a checkout or answering a question does not need them. It is not a
// field to fill in with a default: a token this side invented would be a token
// the cloud never issued.
type mcpParams struct {
	// URL is the MCP endpoint, absolute and https.
	URL string `json:"url"`
	// Token is the per-run bearer credential. It authorises exactly this run's
	// writes and nothing else.
	Token string `json:"token"`
	// ServerName is what the tools are named after, on both sides.
	ServerName string `json:"server_name"`
}

// mcpConfig is the validated form of mcpParams, the same way config is the
// validated form of wireConfig: it can only be built by checkMCP, so a value
// of this type has already been through the grammars and nothing that writes a
// file has to re-check them.
type mcpConfig struct {
	url        string
	token      string
	serverName string
	// host is kept for the log line, so nothing has to re-parse the URL to
	// say where a session's tools live.
	host string
}

// checkMCP validates the object, or refuses it. It is called during
// prepareRun, which is to say BEFORE the 200 — a malformed `mcp` has to be a
// status the caller sees at once, not a `done` frame it has to read a stream to
// reach, and certainly not a broken config file handed to a CLI.
//
// nil in, nil out: a call with no `mcp` is not an error.
func checkMCP(p *mcpParams) (*mcpConfig, *rpcError) {
	if p == nil {
		return nil, nil
	}

	raw := strings.TrimSpace(p.URL)
	if raw == "" {
		return nil, failure(codeBadRequest, "mcp.url is required when mcp is present")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, failure(codeBadRequest, "mcp.url is not a URL: %v", err)
	}
	// https only, and absolute — except loopback, where plain http is let
	// through for local trials: a URL that never leaves this machine cannot
	// put the bearer token on a network wire either way. A relative URL is not
	// an endpoint at all — the CLI would resolve it against something nobody
	// here chose.
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
		return nil, failure(codeBadRequest, "mcp.url %q must be an absolute https:// URL, or http:// on loopback (127.0.0.1, localhost, [::1]) for local trials (the token is a bearer credential)", raw)
	}
	if u.Host == "" {
		return nil, failure(codeBadRequest, "mcp.url %q has no host; an absolute https:// URL is required", raw)
	}
	// Credentials in the URL would be a second, unaudited place for a secret to
	// live — one that ends up in the CLI's own logs and in every error it
	// prints. The token travels as a header and nowhere else.
	if u.User != nil {
		return nil, failure(codeBadRequest, "mcp.url must not carry userinfo; the token is sent as a request header")
	}

	// Deliberately not trimmed: whitespace around a credential is not
	// whitespace this side gets to decide about. The grammar refuses it
	// outright instead, because a token with a space in it is not one and a
	// token with a newline in it is a header injection.
	if p.Token == "" {
		return nil, failure(codeBadRequest, "mcp.token is required when mcp is present")
	}
	if !mcpToken.MatchString(p.Token) {
		return nil, failure(codeBadRequest, "mcp.token must be printable ASCII with no whitespace (it becomes an HTTP header value)")
	}
	if len(p.Token) > mcpTokenMaxLen {
		return nil, failure(codeBadRequest, "mcp.token is longer than %d bytes", mcpTokenMaxLen)
	}

	name := strings.TrimSpace(p.ServerName)
	if name == "" {
		return nil, failure(codeBadRequest, "mcp.server_name is required when mcp is present (it is half of every tool name the session sees)")
	}
	if !mcpServerName.MatchString(name) {
		return nil, failure(codeBadRequest, "mcp.server_name %q is not a plain identifier", name)
	}

	return &mcpConfig{url: u.String(), token: p.Token, serverName: name, host: u.Host}, nil
}

// mcpTokenOf reads the token out of a validated config that may be nil — the
// one piece of it credentialRedactor needs, without every caller having to
// nil-check the config itself.
func mcpTokenOf(m *mcpConfig) string {
	if m == nil {
		return ""
	}
	return m.token
}

// mcpDocument is the file the CLI reads. The shape is the CLI's, not ours.
type mcpDocument struct {
	Servers map[string]mcpServer `json:"mcpServers"`
}

type mcpServer struct {
	// Type is "http": the endpoint is a streamable-HTTP MCP server in the
	// cloud, reached over the public base URL rather than over a pipe.
	Type string `json:"type"`
	URL  string `json:"url"`
	// Headers is where the token lives, and the reason this is a file at all.
	Headers map[string]string `json:"headers,omitempty"`
}

// writeMCPRun materialises one run's configuration and returns the argv that
// points the CLI at it, together with the removal that must follow it.
//
// The removal is ALWAYS non-nil, including on the error paths, so the caller
// can defer it before it looks at the error: a half-written file is still a
// file with a token in it.
//
// root is os.TempDir() in the shipped binary — the user's own per-user
// directory on macOS, mode 0700 and never synced anywhere. A test points it
// somewhere it can watch.
func writeMCPRun(root, callID string, m *mcpConfig) ([]string, func(), *rpcError) {
	noop := func() {}
	if root == "" {
		root = os.TempDir()
	}

	// crypto/rand rather than os.MkdirTemp's own randomness. The 0700 is what
	// keeps other users out; an unguessable name is what keeps a process
	// running AS this user from waiting for a predictable path to appear. It
	// also does not depend on what MkdirTemp's source of randomness happens to
	// be in a given Go release.
	var seed [16]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return nil, noop, failure(codeInternal, "could not name a directory for the MCP config: %v", err)
	}
	dir := filepath.Join(root, mcpDirPrefix+hex.EncodeToString(seed[:]))
	// Mkdir, not MkdirAll: it fails if the name is taken, so it can never
	// adopt a directory — or follow a symlink — that something else put there.
	if err := os.Mkdir(dir, 0o700); err != nil {
		return nil, noop, failure(codeInternal, "could not create the MCP config directory: %v", err)
	}

	// From here on the directory exists, so every return removes it.
	remove := func() {
		if err := os.RemoveAll(dir); err != nil {
			// Loud, because the failure is a bearer token still sitting on
			// somebody's disk with nothing left to clean it up but the sweep on
			// the next start.
			log.Error().Err(err).Str("call", callID).Str("dir", dir).Msg("could not remove the run's MCP config; a token is still on disk")
		}
	}

	doc := mcpDocument{Servers: map[string]mcpServer{}}
	if m != nil {
		doc.Servers[m.serverName] = mcpServer{
			Type:    "http",
			URL:     m.url,
			Headers: map[string]string{"Authorization": "Bearer " + m.token},
		}
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		return nil, remove, failure(codeInternal, "could not encode the MCP config: %v", err)
	}

	path := filepath.Join(dir, mcpFileName)
	// O_EXCL and 0600: the file is created here or not at all, and nothing but
	// this user can read the token in it.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, remove, failure(codeInternal, "could not create the MCP config: %v", err)
	}
	if _, err := f.Write(encoded); err != nil {
		_ = f.Close()
		return nil, remove, failure(codeInternal, "could not write the MCP config: %v", err)
	}
	if err := f.Close(); err != nil {
		return nil, remove, failure(codeInternal, "could not write the MCP config: %v", err)
	}

	if m != nil {
		// The server and its host, never the token. There is a test for that.
		log.Debug().Str("call", callID).Str("mcp_server", m.serverName).Str("mcp_host", m.host).Msg("the session was given TaskTrooper's tools")
	}

	// --strict-mcp-config on every run, with or without a server in the file:
	// it is what keeps the user's own MCP servers out of a TaskTrooper task.
	return []string{"--mcp-config", path, "--strict-mcp-config"}, remove, nil
}

// sweepMCPConfigs removes config directories an earlier run of this program
// left behind, and is called once at startup.
//
// Every ordinary ending — the session finishing, failing, being cancelled, the
// tunnel dropping — removes the directory through a deferred call in the
// handler. The one ending that cannot is the process being killed outright:
// SIGKILL, a panic, a Mac losing power. What that leaves is a bearer token in a
// file, and this is what clears it.
//
// Ownership is checked because os.TempDir() is only guaranteed to be private
// on macOS, where it is the per-user directory launchd hands out, and on
// Windows, where it is the per-user AppData\Local\Temp. On Linux it is /tmp,
// a directory whose entries belong to whoever made them, and removing another
// user's files is not this program's business. See ownedByThisUser.
//
// Safe to run at startup because the desktop app runs exactly ONE runner and
// drains it — SIGTERM, then time — before starting another; there is never a
// second live runner whose directories these could be.
func sweepMCPConfigs(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		log.Debug().Err(err).Str("dir", root).Msg("could not scan for stale MCP configs")
		return
	}
	swept := 0
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), mcpDirPrefix) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if !ownedByThisUser(info) {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			log.Warn().Err(err).Str("dir", path).Msg("could not remove a stale MCP config")
			continue
		}
		swept++
	}
	if swept > 0 {
		// Worth a line at info: it means the last run of this program did not
		// exit through any of its own paths.
		log.Info().Int("count", swept).Msg("removed MCP configs an unclean exit left behind")
	}
}
