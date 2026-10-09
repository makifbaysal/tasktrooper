package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/rs/zerolog/log"
)

// The post-run half of a board run, forwarded to the local executor exactly
// as agent.run is: the verification pass of a checkout (streamed), git's view
// of it, and the commit and push. The cloud decides what to verify and when to
// push; the checkout, the build and this computer's git credentials stay here.

const githubTokenLabel = "GITHUB_TOKEN_PARAM"

// checkoutHead is what this side reads out of a post-run body before
// forwarding it unchanged.
type checkoutHead struct {
	ID          string `json:"id"`
	Workspace   string `json:"workspace"`
	GitHubToken string `json:"github_token"`
}

func (s *runnerServer) readCheckoutCall(w http.ResponseWriter, r *http.Request) ([]byte, checkoutHead, *executorSupervisor, bool) {
	body, readErr := readBody(r)
	if readErr != nil {
		writeError(w, readErr)
		return nil, checkoutHead{}, nil, false
	}
	ex := s.state.executorSupervisor()
	if ex == nil {
		writeError(w, failure(codeNotReady, "this machine has no local executor (executor_bin was not sent)"))
		return nil, checkoutHead{}, nil, false
	}
	var head checkoutHead
	if err := json.Unmarshal(body, &head); err != nil {
		writeError(w, failure(codeBadRequest, "the request body is not the expected object: %v", err))
		return nil, checkoutHead{}, nil, false
	}
	if _, err := resolveInWorkspace(s.cfg.workspaceDir, head.Workspace); err != nil {
		writeError(w, failure(codeBadRequest, "workspace: %v", err))
		return nil, checkoutHead{}, nil, false
	}
	if head.ID != "" && !agentRunID.MatchString(head.ID) {
		writeError(w, failure(codeBadRequest, "id %q is not a call id this runner will register", head.ID))
		return nil, checkoutHead{}, nil, false
	}
	return body, head, ex, true
}

func (s *runnerServer) checkoutRedactor(githubToken string) func([]byte) []byte {
	held := providerSecrets(s.cfg.providers)
	held[githubTokenLabel] = githubToken
	return heldSecretRedactor(s.cfg.policy, held)
}

// handleVerify forwards POST /verify to /exec/verify and streams its NDJSON
// back as handleAgentRun does. A pass builds and tests, so it takes a session
// slot like a run, and POST /cancel names it by its id.
func (s *runnerServer) handleVerify(w http.ResponseWriter, r *http.Request) {
	body, head, ex, ok := s.readCheckoutCall(w, r)
	if !ok {
		return
	}
	if head.ID == "" {
		writeError(w, failure(codeBadRequest, "id is required: it is the handle POST /cancel stops this verification by"))
		return
	}
	if !s.enterRun() {
		writeError(w, failure(codeCancelled, "this machine's tunnel session is shutting down and is not taking new runs"))
		return
	}
	defer s.leaveRun()

	run, releaseSlot, queued := s.queueDurable(w, r, head.ID, "verify")
	if !queued {
		return
	}
	refuse := func(e *rpcError) {
		releaseSlot()
		run.abandon()
		writeError(w, e)
	}

	inst, rpcErr := ex.ready(run.ctx)
	if rpcErr != nil {
		refuse(rpcErr)
		return
	}
	redact := s.checkoutRedactor("")
	// On the run's context, not the request's: the verification is what
	// outlives this stream, so the connection to the executor must too.
	req, err := http.NewRequestWithContext(run.ctx, http.MethodPost, inst.base+"/exec/verify", bytes.NewReader(body))
	if err != nil {
		refuse(failure(codeInternal, "building the executor request: %v", err))
		return
	}
	req.Header.Set("Authorization", "Bearer "+inst.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")
	resp, err := ex.client.Do(req)
	if err != nil {
		if run.ctx.Err() != nil {
			refuse(failure(codeCancelled, "the call was cancelled"))
			return
		}
		refuse(failure(codeUpstream, "could not reach the local executor: %v", err))
		return
	}
	if resp.StatusCode != http.StatusOK {
		relayDocument(w, resp, redact)
		_ = resp.Body.Close()
		releaseSlot()
		run.abandon()
		return
	}
	closeBody := func() { _ = resp.Body.Close() }
	tellExecutor := func() {
		if run.ctx.Err() != nil {
			ex.cancelStream(head.ID, inst)
		}
	}

	s.serveDurable(w, r, run, []func(){releaseSlot, closeBody, tellExecutor}, func(runCtx context.Context, frames io.Writer) {
		if relayFrames(frames, resp.Body, redact, head.ID) {
			return
		}
		code, message := codeUpstream, "the local executor ended the verification's stream without a done line"
		if runCtx.Err() != nil {
			code, message = codeCancelled, "the verification was cancelled"
		}
		closing, _ := json.Marshal(doneEvent{V: protocolVersion, ID: head.ID, Event: "done", OK: false, Error: &rpcError{Code: code, Message: message}})
		_, _ = frames.Write(closing)
	})
}

func (s *runnerServer) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	s.forwardCheckoutDocument(w, r, "/exec/git.status")
}

func (s *runnerServer) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	s.forwardCheckoutDocument(w, r, "/exec/git.diff")
}

func (s *runnerServer) handleGitLog(w http.ResponseWriter, r *http.Request) {
	s.forwardCheckoutDocument(w, r, "/exec/git.log")
}

// handleCommitPush forwards the commit and push. A github_token in the body
// goes to the executor, which hands it to git through the environment; it is
// scrubbed out of whatever comes back.
func (s *runnerServer) handleCommitPush(w http.ResponseWriter, r *http.Request) {
	s.forwardCheckoutDocument(w, r, "/exec/commit_push")
}

// forwardCheckoutDocument is llm.complete's forwarding for a call that names a
// checkout: one document each way, the executor's status and body passed back
// scrubbed. Closing the request is how a caller abandons one.
func (s *runnerServer) forwardCheckoutDocument(w http.ResponseWriter, r *http.Request, path string) {
	body, head, ex, ok := s.readCheckoutCall(w, r)
	if !ok {
		return
	}
	if !s.enterRun() {
		writeError(w, failure(codeCancelled, "this machine's tunnel session is shutting down and is not taking new calls"))
		return
	}
	defer s.leaveRun()

	inst, rpcErr := ex.ready(r.Context())
	if rpcErr != nil {
		writeError(w, rpcErr)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, inst.base+path, bytes.NewReader(body))
	if err != nil {
		writeError(w, failure(codeInternal, "building the executor request: %v", err))
		return
	}
	req.Header.Set("Authorization", "Bearer "+inst.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := ex.client.Do(req)
	if err != nil {
		if r.Context().Err() != nil {
			writeError(w, failure(codeCancelled, "the call was cancelled"))
			return
		}
		writeError(w, failure(codeUpstream, "could not reach the local executor: %v", err))
		return
	}
	defer func() { _ = resp.Body.Close() }()
	relayDocument(w, resp, s.checkoutRedactor(head.GitHubToken))
}

// cancelStream tells the executor a forwarded stream's caller is gone, by
// the stream's own id — how /exec/cancel names a verification.
func (e *executorSupervisor) cancelStream(id string, inst *executorProcess) {
	ctx, cancel := context.WithTimeout(context.Background(), executorCancelTimeout)
	defer cancel()
	payload, _ := json.Marshal(map[string]string{"id": id})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, inst.base+"/exec/cancel", bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+inst.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		log.Debug().Err(err).Str("id", id).Msg("could not tell the executor a verification was cancelled")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	_ = resp.Body.Close()
}
