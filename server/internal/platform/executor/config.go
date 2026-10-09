// Package executor wires the headless executor process: the config its parent
// writes on stdin, the user's providers held in memory, the local tools, and
// the loopback HTTP surface (adapter/executorapi) over application/executor.
package executor

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const defaultListen = "127.0.0.1:0"

// minTokenLength refuses a bearer short enough to guess: the listener is on
// loopback, but every other process on the machine can reach loopback.
const minTokenLength = 16

const maxConfigLine = 1 << 20

// Config is the one JSON line the parent writes on stdin. It holds the user's
// provider keys, so it is never logged, and String keeps a stray %v from
// printing them.
type Config struct {
	Listen            string `json:"listen"`
	Token             string `json:"token"`
	DataDir           string `json:"data_dir"`
	WorkspaceRoot     string `json:"workspace_root"`
	EmbeddingsBaseURL string `json:"embeddings_base_url"`
	// EmbeddingsSource names the engine behind EmbeddingsBaseURL in the index
	// provenance; empty means the desktop's int8 ONNX embedder.
	EmbeddingsSource string `json:"embeddings_source,omitempty"`
	// PostgresCacheDir holds the Postgres binaries the index store runs on;
	// the desktop's own cache spares a download. Empty means one under
	// data_dir.
	PostgresCacheDir string           `json:"postgres_cache_dir,omitempty"`
	Providers        []ProviderConfig `json:"providers"`
	// MCPServers are the member's own MCP servers on this computer. Their env
	// and headers are secrets, so they are held like the provider keys.
	MCPServers []MCPServerConfig `json:"mcp_servers,omitempty"`
	Debug      bool              `json:"debug,omitempty"`
}

// MCPServerConfig is one of the member's MCP servers: a stdio command or an
// http endpoint, never both.
type MCPServerConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (m MCPServerConfig) String() string { return m.Name }

func (m MCPServerConfig) GoString() string { return m.Name }

const maxMCPServers = 32

var (
	mcpServerName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,31}$`)
	mcpEnvName    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	mcpHeaderName = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,128}$")
	configControl = regexp.MustCompile(`[\x00-\x1f\x7f]`)
)

func (m MCPServerConfig) validate() error {
	if !mcpServerName.MatchString(m.Name) {
		return fmt.Errorf("name %q is not a server name (letters, digits and hyphens, at most 32)", m.Name)
	}
	hasCommand, hasURL := strings.TrimSpace(m.Command) != "", strings.TrimSpace(m.URL) != ""
	if hasCommand == hasURL {
		return errors.New("a server has either a command (stdio) or a url (http), not both and not neither")
	}
	if hasURL {
		if len(m.Args) > 0 || len(m.Env) > 0 {
			return errors.New("an http server takes headers, not args or env")
		}
		u, err := url.Parse(strings.TrimSpace(m.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return errors.New("url must be an http(s) address without credentials in it")
		}
		for k, v := range m.Headers {
			if !mcpHeaderName.MatchString(k) || configControl.MatchString(v) {
				return fmt.Errorf("header %q is not a header", k)
			}
		}
		return nil
	}
	if len(m.Headers) > 0 {
		return errors.New("a stdio server takes args and env, not headers")
	}
	if configControl.MatchString(m.Command) {
		return errors.New("command contains control characters")
	}
	for _, a := range m.Args {
		if strings.ContainsRune(a, 0) {
			return errors.New("an arg contains a NUL")
		}
	}
	for k, v := range m.Env {
		if !mcpEnvName.MatchString(k) || strings.ContainsRune(v, 0) {
			return fmt.Errorf("env %q is not an environment variable", k)
		}
	}
	return nil
}

func (m MCPServerConfig) domainConfig() domain.MCPServerConfig {
	cfg := domain.MCPServerConfig{ID: m.Name, Enabled: true, Access: domain.MCPAccessListed}
	if strings.TrimSpace(m.URL) != "" {
		cfg.Transport, cfg.URL, cfg.Headers = "http", strings.TrimSpace(m.URL), m.Headers
		return cfg
	}
	cfg.Transport, cfg.Command, cfg.Args, cfg.Env = "stdio", strings.TrimSpace(m.Command), m.Args, m.Env
	return cfg
}

// secrets are the values that must not leave this process in any answer.
func (m MCPServerConfig) secrets() []string {
	var out []string
	for _, v := range m.Env {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	for _, v := range m.Headers {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

type ProviderConfig struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"`
	BaseURL        string   `json:"base_url"`
	APIKey         string   `json:"api_key"`
	Models         []string `json:"models"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

func (c Config) String() string {
	ids := make([]string, 0, len(c.Providers))
	for _, p := range c.Providers {
		ids = append(ids, p.String())
	}
	servers := make([]string, 0, len(c.MCPServers))
	for _, m := range c.MCPServers {
		servers = append(servers, m.Name)
	}
	return fmt.Sprintf("executor config{listen=%s workspace_root=%s data_dir=%s embeddings=%s providers=[%s] mcp_servers=[%s]}",
		c.Listen, c.WorkspaceRoot, c.DataDir, c.EmbeddingsBaseURL, strings.Join(ids, " "), strings.Join(servers, " "))
}

func (c Config) GoString() string { return c.String() }

func (p ProviderConfig) String() string {
	return fmt.Sprintf("%s(%s)", p.ID, p.Type)
}

func (p ProviderConfig) GoString() string { return p.String() }

// ReadConfig reads the first line of the parent's stdin. A config followed by
// EOF is still a config: the process then starts and, its lifetime channel
// already closed, shuts straight down.
func ReadConfig(r *bufio.Reader) (Config, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > maxConfigLine {
			return Config{}, fmt.Errorf("the config line on stdin exceeds %d bytes", maxConfigLine)
		}
		if err == nil || errors.Is(err, io.EOF) {
			break
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return Config{}, fmt.Errorf("read the config line from stdin: %w", err)
		}
	}
	if len(strings.TrimSpace(string(line))) == 0 {
		return Config{}, errors.New("no config line on stdin: the parent writes one JSON object, then keeps stdin open")
	}
	return ParseConfig(line)
}

func ParseConfig(line []byte) (Config, error) {
	var cfg Config
	if err := json.Unmarshal(line, &cfg); err != nil {
		return Config{}, fmt.Errorf("the config line is not a JSON object: %w", err)
	}
	if err := cfg.normalize(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) normalize() error {
	if len(strings.TrimSpace(c.Token)) < minTokenLength {
		return fmt.Errorf("token must be at least %d characters", minTokenLength)
	}
	c.Token = strings.TrimSpace(c.Token)

	listen, err := loopbackListen(c.Listen)
	if err != nil {
		return err
	}
	c.Listen = listen

	if strings.TrimSpace(c.WorkspaceRoot) == "" {
		return errors.New("workspace_root is required")
	}
	if c.WorkspaceRoot, err = filepath.Abs(strings.TrimSpace(c.WorkspaceRoot)); err != nil {
		return fmt.Errorf("workspace_root: %w", err)
	}
	if strings.TrimSpace(c.DataDir) != "" {
		if c.DataDir, err = filepath.Abs(strings.TrimSpace(c.DataDir)); err != nil {
			return fmt.Errorf("data_dir: %w", err)
		}
	}
	if strings.TrimSpace(c.PostgresCacheDir) != "" {
		if c.PostgresCacheDir, err = filepath.Abs(strings.TrimSpace(c.PostgresCacheDir)); err != nil {
			return fmt.Errorf("postgres_cache_dir: %w", err)
		}
	}
	if c.EmbeddingsBaseURL, err = loopbackEmbeddings(c.EmbeddingsBaseURL); err != nil {
		return err
	}
	c.EmbeddingsSource = strings.TrimSpace(c.EmbeddingsSource)

	seen := make(map[string]bool, len(c.Providers))
	for i := range c.Providers {
		p := &c.Providers[i]
		p.ID = strings.TrimSpace(p.ID)
		if p.ID == "" {
			return fmt.Errorf("providers[%d] has no id", i)
		}
		if seen[p.ID] {
			return fmt.Errorf("provider id %q appears twice", p.ID)
		}
		seen[p.ID] = true
		typ, err := wireType(p.Type)
		if err != nil {
			return fmt.Errorf("provider %q: %w", p.ID, err)
		}
		if typ == typeOpenAICompatible && strings.TrimSpace(p.BaseURL) == "" {
			return fmt.Errorf("provider %q: an OpenAI-compatible endpoint needs a base_url", p.ID)
		}
		if p.TimeoutSeconds < 0 {
			return fmt.Errorf("provider %q: timeout_seconds cannot be negative", p.ID)
		}
	}
	if len(c.MCPServers) > maxMCPServers {
		return fmt.Errorf("mcp_servers: %d entries, at most %d", len(c.MCPServers), maxMCPServers)
	}
	names := make(map[string]bool, len(c.MCPServers))
	for i, m := range c.MCPServers {
		if err := m.validate(); err != nil {
			return fmt.Errorf("mcp_servers[%d]: %w", i, err)
		}
		if names[m.Name] {
			return fmt.Errorf("mcp_servers[%d]: name %q appears twice", i, m.Name)
		}
		names[m.Name] = true
	}
	return nil
}

// loopbackListen refuses anything but a loopback address: this surface runs
// code and spends the user's keys, and only the parent on this machine may
// reach it.
func loopbackListen(listen string) (string, error) {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return defaultListen, nil
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("listen %q is not host:port: %w", listen, err)
	}
	if strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("listen %q is not a loopback address; the executor only serves this machine", listen)
	}
	return net.JoinHostPort(ip.String(), port), nil
}

// loopbackEmbeddings refuses an embedder off this machine: every chunk of
// code the index embeds is sent to it, and code does not leave this computer.
func loopbackEmbeddings(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("embeddings_base_url %q is not an http(s) URL", raw)
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return raw, nil
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("embeddings_base_url %q is not on this computer; the code it would embed does not leave it", raw)
	}
	return raw, nil
}

const (
	typeOpenAICompatible = "openai_compatible"
)

// wireType is the client a provider's requests go through. A host-executed
// provider is a CLI the runner starts, not something this process can call.
func wireType(t string) (domain.LLMProviderType, error) {
	switch typ := strings.ToLower(strings.TrimSpace(t)); typ {
	case typeOpenAICompatible, "endpoint", "custom":
		return typeOpenAICompatible, nil
	case "":
		return "", errors.New("type is required")
	default:
		def, ok := domain.LLMProviderDefinitionFor(domain.LLMProviderType(typ))
		if !ok {
			return "", fmt.Errorf("type %q is not a provider this executor can call", t)
		}
		if def.HostExecuted {
			return "", fmt.Errorf("type %q is an agent CLI the runner starts, not an API this executor calls", t)
		}
		return def.Type, nil
	}
}
