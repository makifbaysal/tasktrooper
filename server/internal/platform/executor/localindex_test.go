package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/executorapi"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/mcpserver"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const fakeEmbedDims = 768

// fakeEmbedder is an OpenAI-compatible embeddings endpoint with deterministic
// vectors: words are hashed into buckets, so texts that share words point the
// same way and a search ranks the way a person would expect.
type fakeEmbedder struct {
	mu     sync.Mutex
	inputs []string
}

func (f *fakeEmbedder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch strings.TrimPrefix(r.URL.Path, "/v1") {
	case "/models":
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": domain.PinnedLocalEmbeddingModel}}})
	case "/embeddings":
		var body struct {
			Input string `json:"input"`
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model != domain.PinnedLocalEmbeddingModel {
			http.Error(w, `{"error":"unknown model"}`, http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.inputs = append(f.inputs, body.Input)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"embedding": wordVector(body.Input)}}})
	default:
		http.NotFound(w, r)
	}
}

// chunkInputs are the chunks embedded so far, leaving out search queries: a
// chunk's input starts with its file path.
func (f *fakeEmbedder) chunkInputs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, in := range f.inputs {
		if first, _, _ := strings.Cut(in, " "); strings.Contains(first, ".") {
			out = append(out, in)
		}
	}
	return out
}

func words(text string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 1 {
			out = append(out, strings.ToLower(string(cur)))
		}
		cur = cur[:0]
	}
	var prev rune
	for _, r := range text {
		switch {
		case unicode.IsUpper(r) && unicode.IsLower(prev):
			flush()
			cur = append(cur, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			cur = append(cur, r)
		default:
			flush()
		}
		prev = r
	}
	flush()
	return out
}

func wordVector(text string) []float32 {
	vec := make([]float64, fakeEmbedDims)
	for _, w := range words(text) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(w))
		vec[h.Sum32()%fakeEmbedDims]++
	}
	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	out := make([]float32, fakeEmbedDims)
	for i, v := range vec {
		if norm > 0 {
			out[i] = float32(v / norm)
		}
	}
	return out
}

// postgresCacheForTest copies an already downloaded Postgres archive into a
// directory of the test's own, so the cluster neither downloads nor shares an
// extraction directory with another test binary.
func postgresCacheForTest(t *testing.T) string {
	dir := t.TempDir()
	home, _ := os.UserHomeDir()
	candidates := []string{
		os.Getenv("TASKTROOPER_TEST_PG_CACHE"),
		filepath.Join(home, "Library", "Application Support", "TaskTrooper", "postgres-bin"),
		filepath.Join(home, ".embedded-postgres-go"),
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(c, "embedded-postgres-binaries-*-17.*.txz"))
		for _, m := range matches {
			raw, err := os.ReadFile(m)
			if err == nil && os.WriteFile(filepath.Join(dir, filepath.Base(m)), raw, 0o600) == nil {
				return dir
			}
		}
	}
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	cmd := exec.Command("git", append([]string{"-c", "user.email=test@example.com", "-c", "user.name=test", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

var fixtureRepo = map[string]string{
	"auth.go": `package app

// RefreshToken exchanges a refresh token for a new access token.
func RefreshToken(refresh string) (string, error) {
	if refresh == "" {
		return "", errMissingRefreshToken
	}
	return "access-" + refresh, nil
}
`,
	"billing.go": `package app

// ChargeCard charges the customer's card for an invoice amount in cents.
func ChargeCard(customerID string, amountCents int) error {
	if amountCents <= 0 {
		return errInvalidAmount
	}
	return nil
}
`,
	"errors.go": `package app

import "errors"

var (
	errMissingRefreshToken = errors.New("missing refresh token")
	errInvalidAmount       = errors.New("invalid amount")
)
`,
}

type LocalIndexSuite struct {
	suite.Suite
	embedder    *fakeEmbedder
	embedServer *httptest.Server
	provider    *scriptedProvider
	llmServer   *httptest.Server
	cloud       *httptest.Server
	cloudSearch *mocks.ToolExecutor
	board       *mocks.ToolExecutor
	mcpToken    string
	root        string
	server      *Server
	baseURL     string
}

func TestLocalIndexSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the embedded postgres index store in short mode")
	}
	suite.Run(t, new(LocalIndexSuite))
}

func (s *LocalIndexSuite) toolMock(name string) *mocks.ToolExecutor {
	tool := mocks.NewToolExecutor(s.T())
	tool.On("Name").Return(name).Maybe()
	tool.On("Definition").Return(domain.ToolDefinition{Type: "function", Function: domain.FunctionDefinition{
		Name: name, Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}}).Maybe()
	return tool
}

func (s *LocalIndexSuite) SetupSuite() {
	s.embedder = &fakeEmbedder{}
	s.embedServer = httptest.NewServer(s.embedder)
	s.provider = &scriptedProvider{}
	s.llmServer = httptest.NewServer(s.provider)

	s.board = s.toolMock("list_board_tasks")
	s.cloudSearch = s.toolMock("codebase_search")
	cloudTools := registry.New()
	cloudTools.Register(s.board)
	cloudTools.Register(s.cloudSearch)
	tokens := mcpserver.NewRunTokenRegistry()
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	mcpserver.New(cloudTools, tokens).Register(app)
	s.cloud = httptest.NewServer(adaptor.FiberApp(app))
	var err error
	s.mcpToken, err = tokens.Mint(mcpserver.Run{
		Ctx:    context.Background(),
		Policy: domain.ToolPolicy{AllowTools: []string{"list_board_tasks", "codebase_search"}},
	})
	s.Require().NoError(err)

	s.root = s.T().TempDir()
	var stdout bytes.Buffer
	s.server, err = Start(Config{
		Listen:            defaultListen,
		Token:             validToken,
		WorkspaceRoot:     s.root,
		DataDir:           s.T().TempDir(),
		EmbeddingsBaseURL: s.embedServer.URL + "/v1",
		PostgresCacheDir:  postgresCacheForTest(s.T()),
		Providers: []ProviderConfig{{
			ID: "byok", Type: typeOpenAICompatible, BaseURL: s.llmServer.URL, APIKey: providerKey, Models: []string{"gpt-test"},
		}},
	}, &stdout, Options{})
	s.Require().NoError(err)
	s.baseURL = strings.TrimPrefix(strings.TrimSpace(stdout.String()), ListeningPrefix)
}

func (s *LocalIndexSuite) TearDownSuite() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if s.server != nil {
		s.NoError(s.server.Shutdown(ctx))
	}
	s.cloud.Close()
	s.llmServer.Close()
	s.embedServer.Close()
}

// checkout makes a git repository under the workspace root and returns its
// path relative to the root.
func (s *LocalIndexSuite) checkout(rel string, files map[string]string) string {
	dir := filepath.Join(s.root, rel)
	s.Require().NoError(os.MkdirAll(dir, 0o755))
	git(s.T(), dir, "init", "-q")
	writeFiles(s.T(), dir, files)
	git(s.T(), dir, "add", "-A")
	git(s.T(), dir, "commit", "-q", "-m", "initial")
	return rel
}

// branchOf clones rel to a task checkout on branch, then changes files there.
func (s *LocalIndexSuite) branchOf(rel, taskRel, branch string, changes map[string]string) string {
	git(s.T(), s.root, "clone", "-q", rel, taskRel)
	dir := filepath.Join(s.root, taskRel)
	git(s.T(), dir, "checkout", "-q", "-b", branch)
	writeFiles(s.T(), dir, changes)
	git(s.T(), dir, "add", "-A")
	git(s.T(), dir, "commit", "-q", "-m", "task work")
	return taskRel
}

func (s *LocalIndexSuite) post(path string, body any) *http.Response {
	raw, err := json.Marshal(body)
	s.Require().NoError(err)
	req, err := http.NewRequest(http.MethodPost, s.baseURL+path, bytes.NewReader(raw))
	s.Require().NoError(err)
	req.Header.Set("Authorization", "Bearer "+validToken)
	resp, err := http.DefaultClient.Do(req)
	s.Require().NoError(err)
	return resp
}

func (s *LocalIndexSuite) frames(resp *http.Response) []frame {
	defer resp.Body.Close()
	s.Require().Equal(http.StatusOK, resp.StatusCode)
	var frames []frame
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		var f frame
		s.Require().NoError(json.Unmarshal(scanner.Bytes(), &f))
		frames = append(frames, f)
	}
	return frames
}

type ensuredIndex struct {
	Status         string `json:"status"`
	Branch         string `json:"branch"`
	Files          int    `json:"files"`
	Chunks         int    `json:"chunks"`
	Symbols        int    `json:"symbols"`
	CommitSHA      string `json:"commit_sha"`
	EmbeddingModel string `json:"embedding_model"`
	EmbeddingDims  int    `json:"embedding_dims"`
	SeededFrom     string `json:"seeded_from"`
}

func (s *LocalIndexSuite) ensure(body map[string]any) (ensuredIndex, []frame) {
	frames := s.frames(s.post("/exec/index.ensure", body))
	s.Require().GreaterOrEqual(len(frames), 2)
	s.Equal("started", frames[0].Event)
	done := frames[len(frames)-1]
	s.Require().Equal("done", done.Event)
	s.Require().True(*done.OK, "%v", done.Error)
	var state ensuredIndex
	s.Require().NoError(json.Unmarshal(done.Result, &state))
	return state, frames
}

type searchAnswer struct {
	Index struct {
		Branch string `json:"branch"`
		Status string `json:"status"`
	} `json:"index"`
	Results []struct {
		Path      string  `json:"path"`
		Symbol    string  `json:"symbol"`
		StartLine int     `json:"start_line"`
		EndLine   int     `json:"end_line"`
		Score     float64 `json:"score"`
		Snippet   string  `json:"snippet"`
	} `json:"results"`
}

func (s *LocalIndexSuite) search(body map[string]any) (int, searchAnswer, map[string]any) {
	resp := s.post("/exec/index.search", body)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	s.Require().NoError(err)
	var answer searchAnswer
	var errBody map[string]any
	if resp.StatusCode == http.StatusOK {
		s.Require().NoError(json.Unmarshal(raw, &answer))
	} else {
		s.Require().NoError(json.Unmarshal(raw, &errBody))
	}
	return resp.StatusCode, answer, errBody
}

func phases(frames []frame) []string {
	var out []string
	for _, f := range frames {
		if f.Event == "event" && f.Payload["kind"] == "index_progress" {
			out = append(out, f.Payload["phase"].(string))
		}
	}
	return out
}

func (s *LocalIndexSuite) TestEnsureThenSearchFindsTheCodeThatAnswers() {
	rel := s.checkout("repos/app/main", fixtureRepo)

	state, frames := s.ensure(map[string]any{"workspace": rel, "repo_key": "github.com/acme/app"})

	s.Equal("completed", state.Status)
	s.Equal(3, state.Files)
	s.GreaterOrEqual(state.Chunks, 2)
	s.Equal("nomic-embed-text-v1.5@onnx-int8", state.EmbeddingModel)
	s.Equal(fakeEmbedDims, state.EmbeddingDims)
	s.Len(state.CommitSHA, 40)
	s.Contains(phases(frames), "indexing")

	status, answer, _ := s.search(map[string]any{"repo_key": "github.com/acme/app", "query": "refresh the access token", "k": 2})

	s.Require().Equal(http.StatusOK, status)
	s.Equal("completed", answer.Index.Status)
	s.Require().NotEmpty(answer.Results)
	top := answer.Results[0]
	s.Equal("auth.go", top.Path)
	s.Equal("RefreshToken", top.Symbol)
	s.Positive(top.StartLine)
	s.GreaterOrEqual(top.EndLine, top.StartLine)
	s.Contains(top.Snippet, "func RefreshToken")
	s.LessOrEqual(len(answer.Results), 2)

	embedded := len(s.embedder.chunkInputs())
	again, _ := s.ensure(map[string]any{"workspace": rel, "repo_key": "github.com/acme/app"})
	s.Equal("completed", again.Status)
	s.Len(s.embedder.chunkInputs(), embedded, "an unchanged checkout embeds nothing again")
}

func (s *LocalIndexSuite) TestABranchIndexStartsFromTheBaseAndEmbedsOnlyWhatDiffers() {
	base := s.checkout("repos/branchy/main", fixtureRepo)
	s.ensure(map[string]any{"workspace": base, "repo_key": "github.com/acme/branchy"})
	before := len(s.embedder.chunkInputs())
	task := s.branchOf(base, "repos/branchy/task-1", "tt/task-1", map[string]string{
		"billing.go": fixtureRepo["billing.go"] + `
// RefundCard returns a charge to the customer's card.
func RefundCard(customerID string, amountCents int) error {
	return nil
}
`,
	})

	state, frames := s.ensure(map[string]any{"workspace": task, "repo_key": "github.com/acme/branchy", "branch": "tt/task-1"})

	s.Equal("completed", state.Status)
	s.Equal("tt/task-1", state.Branch)
	s.Equal("base", state.SeededFrom)
	s.Contains(phases(frames), "seeded")
	branchPass := s.embedder.chunkInputs()[before:]
	s.NotEmpty(branchPass)
	for _, in := range branchPass {
		s.True(strings.HasPrefix(in, "billing.go"), "the branch pass embedded %q", strings.SplitN(in, "\n", 2)[0])
	}

	status, onBranch, _ := s.search(map[string]any{"repo_key": "github.com/acme/branchy", "branch": "tt/task-1", "query": "refund card"})
	s.Require().Equal(http.StatusOK, status)
	s.Equal("tt/task-1", onBranch.Index.Branch)
	s.Require().NotEmpty(onBranch.Results)
	s.Equal("RefundCard", onBranch.Results[0].Symbol)

	status, onBase, _ := s.search(map[string]any{"repo_key": "github.com/acme/branchy", "query": "refund card"})
	s.Require().Equal(http.StatusOK, status)
	s.Empty(onBase.Index.Branch)
	for _, hit := range onBase.Results {
		s.NotEqual("RefundCard", hit.Symbol)
	}
}

func (s *LocalIndexSuite) TestABranchWithNoBaseStartsFromAnotherBranch() {
	first := s.checkout("repos/siblings/task-a", fixtureRepo)
	git(s.T(), filepath.Join(s.root, first), "checkout", "-q", "-b", "tt/a")
	a, _ := s.ensure(map[string]any{"workspace": first, "repo_key": "github.com/acme/siblings", "branch": "tt/a"})
	s.Empty(a.SeededFrom)
	before := len(s.embedder.chunkInputs())
	second := s.branchOf(first, "repos/siblings/task-b", "tt/b", map[string]string{
		"auth.go": strings.Replace(fixtureRepo["auth.go"], "access-", "token-", 1),
	})

	b, _ := s.ensure(map[string]any{"workspace": second, "repo_key": "github.com/acme/siblings", "branch": "tt/b"})

	s.Equal("branch:tt/a", b.SeededFrom)
	s.Equal("completed", b.Status)
	for _, in := range s.embedder.chunkInputs()[before:] {
		s.True(strings.HasPrefix(in, "auth.go"), "the seeded pass embedded %q", strings.SplitN(in, "\n", 2)[0])
	}
}

func (s *LocalIndexSuite) TestOnlyTheMostRecentlyIndexedBranchesAreKept() {
	rel := s.checkout("repos/pruned/main", fixtureRepo)
	s.ensure(map[string]any{"workspace": rel, "repo_key": "github.com/acme/pruned"})
	branches := []string{"tt/p-0", "tt/p-1", "tt/p-2", "tt/p-3", "tt/p-4", "tt/p-5", "tt/p-6", "tt/p-7"}
	for _, branch := range branches {
		s.ensure(map[string]any{"workspace": rel, "repo_key": "github.com/acme/pruned", "branch": branch})
	}
	s.ensure(map[string]any{"workspace": rel, "repo_key": "github.com/acme/pruned", "branch": "tt/p-0"})

	s.ensure(map[string]any{"workspace": rel, "repo_key": "github.com/acme/pruned", "branch": "tt/p-8"})

	served := func(branch string) string {
		status, answer, errBody := s.search(map[string]any{"repo_key": "github.com/acme/pruned", "branch": branch, "query": "charge card"})
		s.Require().Equal(http.StatusOK, status, "%s: %v", branch, errBody)
		return answer.Index.Branch
	}
	s.Equal("", served("tt/p-1"), "the branch indexed longest ago is dropped and its searches read the base")
	s.Equal("tt/p-0", served("tt/p-0"), "a branch indexed again is recent, however old its row")
	s.Equal("tt/p-8", served("tt/p-8"))
}

// The caller never asks for a pass: the run indexes its own checkout.
func (s *LocalIndexSuite) TestAgentRunIndexesItsCheckoutGetsItsContextAndServesTheCodeToolsLocally() {
	rel := s.checkout("repos/agent/task-1", fixtureRepo)
	s.provider.mu.Lock()
	s.provider.requests = nil
	s.provider.turns = []map[string]any{
		toolCallChoice("c1", "codebase_search", `{"query":"refresh token"}`),
		toolCallChoice("c2", "get_symbol_skeleton", `{"symbol_name":"RefreshToken"}`),
		toolCallChoice("c3", "expand_symbol_context", `{"symbol_name":"RefreshToken"}`),
		textChoice("RefreshToken lives in auth.go."),
	}
	s.provider.mu.Unlock()
	indexTools := []string{"codebase_search", "expand_symbol_context", "get_symbol_skeleton"}

	frames := s.frames(s.post("/exec/agent.run", map[string]any{
		"run_id": "run-indexed",
		"kind":   "board",
		"agent": map[string]any{
			"name": "backend-developer", "system_prompt": "You build.", "provider_id": "byok",
			"tool_policy": map[string]any{"allow_tools": append([]string{"list_board_tasks"}, indexTools...)},
		},
		"prompt":    "Where is the refresh token exchanged?",
		"workspace": rel,
		"mcp":       map[string]any{"url": s.cloud.URL + mcpserver.Path, "token": s.mcpToken, "server_name": "tasktrooper"},
		"index":     map[string]any{"repo_key": "github.com/acme/agent"},
	}))

	done := frames[len(frames)-1]
	s.Require().True(*done.OK, "%v", done.Error)
	results := map[string]map[string]any{}
	var injected bool
	var order []string
	var ensured map[string]any
	for _, f := range frames {
		if f.Event != "event" {
			continue
		}
		switch f.Payload["kind"] {
		case "tool_result":
			results[f.Payload["name"].(string)] = f.Payload
		case "index_progress":
			if len(order) == 0 || order[len(order)-1] != "index_progress" {
				order = append(order, "index_progress")
			}
		case "step":
			switch f.Payload["step"] {
			case "index_ensure":
				ensured = f.Payload["data"].(map[string]any)
				order = append(order, "index_ensure")
			case "index_context":
				injected, _ = f.Payload["data"].(map[string]any)["injected"].(bool)
				order = append(order, "index_context")
			}
		}
	}
	s.Equal([]string{"index_progress", "index_ensure", "index_context"}, order)
	s.Require().NotNil(ensured)
	s.Equal("completed", ensured["status"])
	s.Empty(ensured["error"])
	s.True(injected)
	for _, name := range indexTools {
		result := results[name]
		s.Require().NotNil(result, name)
		s.Equal("local", result["source"], name)
		s.Equal(false, result["is_error"], "%s: %v", name, result["content"])
		s.Contains(result["content"], "RefreshToken", name)
	}
	s.Contains(results["codebase_search"]["content"], "auth.go")

	s.provider.mu.Lock()
	first := s.provider.requests[0]
	s.provider.mu.Unlock()
	var offered []string
	for _, tool := range first["tools"].([]any) {
		offered = append(offered, tool.(map[string]any)["function"].(map[string]any)["name"].(string))
	}
	s.ElementsMatch(append([]string{"list_board_tasks"}, indexTools...), offered)
	messages := first["messages"].([]any)
	last := messages[len(messages)-1].(map[string]any)
	s.Equal("system", last["role"])
	s.Contains(last["content"], "auth.go")
	s.Contains(last["content"], "RefreshToken")
}

func (s *LocalIndexSuite) setEmbeddings(url string) int {
	resp := s.post("/exec/embeddings.set", map[string]any{"embeddings_base_url": url})
	defer resp.Body.Close()
	return resp.StatusCode
}

func (s *LocalIndexSuite) TestTheEmbedderMovesWithoutARestart() {
	moved := &fakeEmbedder{}
	movedServer := httptest.NewServer(moved)
	defer movedServer.Close()
	defer func() { s.Equal(http.StatusOK, s.setEmbeddings(s.embedServer.URL+"/v1")) }()
	rel := s.checkout("repos/moved/main", fixtureRepo)
	before := len(s.embedder.chunkInputs())

	s.Require().Equal(http.StatusOK, s.setEmbeddings(movedServer.URL+"/v1"))
	state, _ := s.ensure(map[string]any{"workspace": rel, "repo_key": "github.com/acme/moved"})

	s.Equal("completed", state.Status)
	s.Equal("nomic-embed-text-v1.5@onnx-int8", state.EmbeddingModel, "the same engine on another port keeps the provenance")
	s.NotEmpty(moved.chunkInputs())
	s.Len(s.embedder.chunkInputs(), before, "nothing went to the old address")

	s.Equal(http.StatusBadRequest, s.setEmbeddings("http://10.0.0.8:8080/v1"))
	s.Require().Equal(http.StatusOK, s.setEmbeddings(""))
	status, _, errBody := s.search(map[string]any{"repo_key": "github.com/acme/moved", "query": "refresh token"})
	s.Equal(http.StatusServiceUnavailable, status)
	s.Contains(errBody["error"].(map[string]any)["message"], "embeddings_base_url")
}

func (s *LocalIndexSuite) TestSearchBeforeAnyIndexAndEnsureOfAMissingCheckout() {
	status, _, errBody := s.search(map[string]any{"repo_key": "github.com/acme/never", "query": "anything"})
	s.Equal(http.StatusServiceUnavailable, status)
	s.Equal("not_ready", errBody["error"].(map[string]any)["code"])

	resp := s.post("/exec/index.ensure", map[string]any{"workspace": "repos/missing", "repo_key": "github.com/acme/missing"})
	defer resp.Body.Close()
	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

// A CLI run's tool surface searches this computer's index of its own
// checkout, never the coordination endpoint's, which the cloud served too.
func (s *LocalIndexSuite) TestACLIRunsSurfaceSearchesThisComputersIndex() {
	rel := s.checkout("repos/surface", fixtureRepo)
	resp := s.post(executorapi.PathMCPOpen, map[string]any{
		"run_id":    "cli-index-1",
		"workspace": rel,
		"index":     map[string]any{"repo_key": "surface-repo", "wait_ms": 120000},
		"cloud_mcp": map[string]any{"url": s.cloud.URL + mcpserver.Path, "token": s.mcpToken, "server_name": "tasktrooper"},
	})
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	s.Require().NoError(err)
	s.Require().Equal(http.StatusOK, resp.StatusCode, string(raw))
	var opened openedSurface
	s.Require().NoError(json.Unmarshal(raw, &opened))
	defer func() { _ = s.post(executorapi.PathMCPClose, map[string]any{"run_id": "cli-index-1"}).Body.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	session, err := connect(ctx, opened.URL, opened.Token)
	s.Require().NoError(err)
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	s.Require().NoError(err)
	for _, want := range []string{"codebase_search", "expand_symbol_context", "get_symbol_skeleton", "list_board_tasks"} {
		s.Contains(names(listed.Tools), want)
	}

	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "codebase_search", Arguments: map[string]any{"query": "exchange a refresh token for an access token"}})
	s.Require().NoError(err)
	s.False(res.IsError, text(res))
	s.Contains(text(res), "RefreshToken")
	s.cloudSearch.AssertNotCalled(s.T(), "Execute", mock.Anything, mock.Anything)
}
