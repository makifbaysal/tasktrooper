package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// workspace.prepare — get a repository onto this Mac and say where it landed.
//
// Deliberately the smallest possible thing: clone it, or fetch and check out a
// branch. Everything else a task needs done to a checkout — branching,
// committing, pushing — happens inside the Claude Code session, which is where
// the task's own judgement lives. A prepare step that started making commits
// would be a second, dumber agent operating on the same working copy.
//
// It reports the absolute path because the caller then names it, relative to
// the workspace root, in `claude.run`.

// gitTimeout bounds one git invocation. A clone of a large repository over a
// slow connection is a real thing, so it is generous; a git that has hung on a
// credential prompt is also real, which is why it is bounded at all.
const gitTimeout = 20 * time.Minute

// refName is what a caller may name as a branch. Git's own rules are longer
// than this and this is deliberately narrower: these become argv, and a name
// that cannot begin with a dash cannot become a flag.
var refName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._/-]{0,254}$`)

type prepareParams struct {
	// RepoURL is https:// or ssh://, or scp-style git@host:path.
	RepoURL string `json:"repo_url"`
	// Dir is where under <workspace>/repos it goes, relative and one or more
	// safe path segments.
	Dir string `json:"dir"`
	// Branch is checked out after the clone or fetch. Empty means whatever the
	// remote's default is.
	Branch string `json:"branch,omitempty"`
	// Depth, for a shallow clone. Ignored on a repository that already exists,
	// because deepening a shallow clone is a different operation.
	Depth int `json:"depth,omitempty"`
	// GitHubToken is the account's GitHub connection, sent by agent-server
	// only for an https://github.com/ remote. Handed to git through the
	// environment for this one clone or fetch (gitEnvFor) — never argv, never
	// written into the checkout's config.
	GitHubToken string `json:"github_token,omitempty"`
}

type prepareResult struct {
	// Path is absolute, because the runner knows the workspace root and the
	// caller does not.
	Path string `json:"path"`
	// Rel is the same location relative to the workspace root, which is what
	// `claude.run` takes.
	Rel    string `json:"rel"`
	Branch string `json:"branch"`
	Head   string `json:"head"`
	Cloned bool   `json:"cloned"`
}

// repoSubdir is fixed. Repositories live in one place under the workspace so a
// user opening the folder in Finder finds what they expect, and so no caller
// gets to decide the layout of somebody's home directory.
const repoSubdir = "repos"

func prepareWorkspace(c *call) (any, *rpcError) {
	var p prepareParams
	if err := json.Unmarshal(c.params, &p); err != nil {
		return nil, failure(codeBadRequest, "workspace.prepare params are not the expected object: %v", err)
	}
	if err := checkRepoURL(p.RepoURL); err != nil {
		return nil, failure(codeBadRequest, "repo_url: %v", err)
	}
	if p.Branch != "" && !refName.MatchString(p.Branch) {
		return nil, failure(codeBadRequest, "branch %q is not a branch name this runner will use", p.Branch)
	}
	if p.Depth < 0 || p.Depth > 1_000_000 {
		return nil, failure(codeBadRequest, "depth %d is outside 0..1000000", p.Depth)
	}

	// `dir` is required, and an empty one is refused rather than defaulted.
	//
	// filepath.Join("repos", "") is "repos", so a missing dir used to clone
	// straight into the repositories folder itself — and the NEXT prepare, for a
	// different repository and also with no dir, found a `.git` there, ran
	// `git fetch --prune origin` inside the FIRST repository, and answered
	// `{cloned:false}` with a path that has nothing to do with what it was
	// asked for. Reporting success for repository B while holding repository A
	// is worse than any refusal.
	dirName := strings.TrimSpace(p.Dir)
	if dirName == "" {
		return nil, failure(codeBadRequest, "dir is required: it names the folder under %s/ this repository is checked out into", repoSubdir)
	}
	rel := filepath.Join(repoSubdir, dirName)
	dir, err := resolveInWorkspace(c.cfg.workspaceDir, rel)
	if err != nil {
		return nil, failure(codeBadRequest, "dir: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, failure(codeInternal, "could not create %s: %v", filepath.Dir(dir), err)
	}

	cloned := false
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		if _, err := os.Stat(dir); err == nil {
			// A directory that is not a git repository. Refused rather than
			// cleared: it is somebody's folder, and deleting a user's files to
			// make room is not a decision a runner gets to make.
			return nil, failure(codeBadRequest, "%s exists and is not a git repository; move it aside first", rel)
		}
		if callErr := gitClone(c, p, dir); callErr != nil {
			return nil, callErr
		}
		cloned = true
	} else if callErr := gitFetch(c, p, dir); callErr != nil {
		return nil, callErr
	}

	branch, callErr := currentBranch(c, dir)
	if callErr != nil {
		return nil, callErr
	}
	head, callErr := git(c, dir, "rev-parse", "HEAD")
	if callErr != nil {
		return nil, callErr
	}

	log.Info().Str("call", c.id).Str("dir", rel).Str("branch", branch).Bool("cloned", cloned).Msg("workspace prepared")
	// Slash-separated on the wire whatever this OS spells it: the caller keeps
	// it and sends it back, possibly to a runner on another OS.
	return prepareResult{Path: dir, Rel: filepath.ToSlash(rel), Branch: branch, Head: strings.TrimSpace(head), Cloned: cloned}, nil
}

// agentSubdir is fixed, the same way repoSubdir is: a stable, predictable
// place under the workspace root rather than one this call gets to invent.
// Kept apart from repos/ because nothing here is a git checkout — mixing the
// two would put a plain scratch directory somewhere workspace.prepare's own
// ".git or refuse" rule would trip on it.
const agentSubdir = "agents"

// workspace.ensure — a bare directory for an agent chat to work in, on this
// Mac, with no git involved.
//
// Board tasks and task-bound chats have a repository to clone; most chat does
// not — an agent answering questions has no remote_url anywhere in it, and
// workspace.prepare refuses to run without one. The CLI still needs somewhere
// on disk to start in (its file and shell tools operate on THAT directory),
// so this is the smallest thing that gives it one: ensure the folder exists,
// say where, relative to the workspace root exactly as prepare does. Stable
// per caller-chosen dir, so the same chat's turns keep reusing one directory
// rather than each getting a fresh empty one.
type ensureWorkspaceParams struct {
	// Dir is where under <workspace>/agents/ this chat's directory goes,
	// relative and one or more safe path segments — the same grammar dir
	// already follows on workspace.prepare.
	Dir string `json:"dir"`
}

type ensureWorkspaceResult struct {
	Path string `json:"path"`
	Rel  string `json:"rel"`
}

func ensureWorkspace(c *call) (any, *rpcError) {
	var p ensureWorkspaceParams
	if err := json.Unmarshal(c.params, &p); err != nil {
		return nil, failure(codeBadRequest, "workspace.ensure params are not the expected object: %v", err)
	}
	dirName := strings.TrimSpace(p.Dir)
	if dirName == "" {
		return nil, failure(codeBadRequest, "dir is required: it names the folder under %s/ this chat works in", agentSubdir)
	}
	rel := filepath.Join(agentSubdir, dirName)
	dir, err := resolveInWorkspace(c.cfg.workspaceDir, rel)
	if err != nil {
		return nil, failure(codeBadRequest, "dir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, failure(codeInternal, "could not create %s: %v", dir, err)
	}
	return ensureWorkspaceResult{Path: dir, Rel: filepath.ToSlash(rel)}, nil
}

func gitClone(c *call, p prepareParams, dir string) *rpcError {
	args := []string{"clone"}
	if p.Depth > 0 {
		args = append(args, "--depth", strconv.Itoa(p.Depth))
	}
	if p.Branch != "" {
		args = append(args, "--branch", p.Branch)
	}
	// Written into the clone's own config on Windows, so the session's git —
	// which never sees gitEnv — can check out the same deep paths this clone
	// could.
	if runtime.GOOS == "windows" {
		args = append(args, "--config", "core.longpaths=true")
	}
	// `--` before the operands, so a URL or a path that somehow began with a
	// dash is an operand rather than a flag. The grammars above already forbid
	// it; this is the second lock on the same door.
	args = append(args, "--", p.RepoURL, dir)
	_, err := gitWithEnv(c, "", gitEnvFor(p), p.GitHubToken, args...)
	return err
}

func gitFetch(c *call, p prepareParams, dir string) *rpcError {
	if _, err := gitWithEnv(c, dir, gitEnvFor(p), p.GitHubToken, "fetch", "--prune", "origin"); err != nil {
		return err
	}
	if p.Branch == "" {
		return nil
	}
	// Check the branch out, creating a local one that tracks the remote if this
	// checkout has never seen it. Deliberately NOT a reset: whatever is in the
	// working copy belongs to a task, and discarding it here would throw away
	// work a session was in the middle of.
	//
	// The `--` goes AFTER the name, not before it. `git checkout -- <name>`
	// declares <name> a PATHSPEC — it restores that file from the index and can
	// never switch a branch — so this read `checkout -- <branch>` and did one of
	// two wrong things: failed with "pathspec did not match" on every repeat
	// prepare (the fallback below then failed too, because the local branch was
	// already there), or, when the branch shared a name with a tracked path,
	// quietly succeeded on the wrong operation and left HEAD where it was.
	// `checkout <branch> --` is the other half of the same rule: the name is a
	// revision, and the terminator still stops anything after it being read as
	// an option. `refName` already forbids a leading dash.
	if _, err := git(c, dir, "checkout", p.Branch, "--"); err == nil {
		return nil
	}
	if _, err := git(c, dir, "checkout", "-b", p.Branch, "--track", "origin/"+p.Branch); err != nil {
		return err
	}
	return nil
}

func currentBranch(c *call, dir string) (string, *rpcError) {
	out, err := git(c, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// gitEnv is trimmed of everything that could make git ask a human something.
//
// A prompt has nowhere to appear here — there is no terminal — so an unprompted
// git waits forever, which is a hang with no message. Failing instead gives the
// caller the credential error it can actually report.
var gitBaseEnv = []string{
	// Never open an interactive credential prompt or an askpass helper.
	"GIT_TERMINAL_PROMPT=0",
	"GIT_ASKPASS=",
	"SSH_ASKPASS=",
	// Git Credential Manager's own switch. Without it GCM opens a sign-in
	// window, which on a machine nobody is looking at is the same hang.
	"GCM_INTERACTIVE=never",
	// Never wait on an unknown host key; a task that needs a new host in
	// known_hosts is a decision for the person, not for this process.
	"GIT_SSH_COMMAND=ssh -oBatchMode=yes",
}

var gitEnv = append(append([]string{}, gitBaseEnv...), gitPlatformEnv()...)

// gitEnvFor is gitEnv plus, for a github.com https remote that came with the
// account's GitHub connection, that token as an http extraheader scoped to
// https://github.com/ — the same shape agent-server's own git client uses. It
// lives in GIT_CONFIG_* for this one process, so it reaches neither argv (ps)
// nor the checkout's .git/config, and the Mac's own credential helper is not
// touched.
func gitEnvFor(p prepareParams) []string {
	token := strings.TrimSpace(p.GitHubToken)
	if token == "" || !isGitHubHTTPS(p.RepoURL) {
		return gitEnv
	}
	pairs := [][2]string{}
	if runtime.GOOS == "windows" {
		pairs = append(pairs, [2]string{"core.longpaths", "true"})
	}
	header := "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
	pairs = append(pairs, [2]string{"http.https://github.com/.extraheader", header})
	env := append([]string{}, gitBaseEnv...)
	env = append(env, "GIT_CONFIG_COUNT="+strconv.Itoa(len(pairs)))
	for i, pair := range pairs {
		env = append(env, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, pair[0]), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, pair[1]))
	}
	return env
}

func isGitHubHTTPS(repoURL string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(repoURL)), "https://github.com/")
}

// gitPlatformEnv is core.longpaths=true on Windows, as configuration in the
// environment so it covers every git this program runs without a flag on each.
// Without it Git for Windows refuses any path over MAX_PATH (260 characters)
// with "Filename too long", and a checkout's node_modules under a workspace in
// the user's profile passes that routinely.
func gitPlatformEnv() []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	return []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.longpaths", "GIT_CONFIG_VALUE_0=true"}
}

// git runs one command and returns its stdout.
//
// The spawning, the process group and the group kill are `runTool`'s (spawn.go)
// — a `git clone` starts a transport helper, and cancelling the call has to take
// that down too rather than leaving it holding a partial checkout open. What is
// left here is the part that is about git: which failure means what, and saying
// it in git's own words.
func git(c *call, dir string, args ...string) (string, *rpcError) {
	return gitWithEnv(c, dir, gitEnv, "", args...)
}

// gitWithEnv is git with a caller-chosen environment. secret, when set, is
// scrubbed from anything this returns: git does not echo the extraheader, but
// a message that could carry it is not one to send back up the tunnel.
func gitWithEnv(c *call, dir string, env []string, secret string, args ...string) (string, *rpcError) {
	out, rpcErr := runGit(c, dir, env, args...)
	if rpcErr != nil && secret != "" {
		rpcErr.Message = strings.ReplaceAll(rpcErr.Message, secret, "[redacted]")
	}
	return out, rpcErr
}

func runGit(c *call, dir string, env []string, args ...string) (string, *rpcError) {
	out, err := runTool(c.ctx, c.cfg.gitBin, dir, env, gitTimeout, args...)
	if err == nil {
		return out.stdout, nil
	}
	if errors.Is(err, errStartFailed) {
		return "", failure(codeInternal, "could not start git: %v", err)
	}
	if out.timedOut {
		return "", failure(codeUpstream, "git %s took longer than %s", args[0], gitTimeout)
	}
	if c.ctx.Err() != nil {
		return "", failure(codeCancelled, "the call was cancelled")
	}
	// git's own words. They name the actual problem — a missing credential, an
	// unknown host, a branch that is not there — and replacing them with a
	// sentence of ours would lose that.
	message := strings.TrimSpace(out.stderr)
	if message == "" {
		message = err.Error()
	}
	return "", failure(codeUpstream, "git %s failed: %s", args[0], firstNonEmptyLine(message))
}

func firstNonEmptyLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return text
}

// scpStyle matches git's `user@host:path` remote form, which is not a URL and
// which url.Parse silently misreads as a path.
var scpStyle = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[A-Za-z0-9._/~-]+$`)

// checkRepoURL refuses everything but the two forms a real repository arrives
// as.
//
// This is a security check and not a tidiness one. Git's `ext::` transport
// takes a COMMAND — `git clone "ext::sh -c whoami"` runs it — and its
// `--upload-pack` option names another. Anything that is not plainly https or
// ssh is refused here rather than reasoned about, and a leading dash is
// refused separately because an operand that becomes a flag is the same class
// of problem wearing different clothes.
func checkRepoURL(raw string) error {
	url := strings.TrimSpace(raw)
	if url == "" {
		return fmt.Errorf("a repository URL is required")
	}
	if strings.HasPrefix(url, "-") {
		return fmt.Errorf("%q begins with a dash, which git would read as an option", url)
	}
	if strings.ContainsAny(url, " \t\n\r") {
		return fmt.Errorf("a repository URL cannot contain whitespace")
	}
	if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "ssh://") {
		return nil
	}
	if scpStyle.MatchString(url) {
		return nil
	}
	return fmt.Errorf("%q is not an https:// or ssh:// repository (git's other transports can name a command to run)", url)
}
