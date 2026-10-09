package executorapi

import (
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/executor"
)

type verifyRequest struct {
	ID string `json:"id,omitempty"`
	executor.VerifyRequest
}

// verify streams one verification pass in the envelope agent.run uses:
// started, verify_stage and verify_output events, and one done frame whose
// result is the pass's verdict.
func (h *Handler) verify(w http.ResponseWriter, r *http.Request) {
	var req verifyRequest
	if failure := decode(r, unaryBodyLimit, &req); failure != nil {
		h.writeError(w, failure)
		return
	}
	prepared, failure := h.svc.PrepareVerify(r.Context(), req.ID, req.VerifyRequest)
	if failure != nil {
		h.writeError(w, failure)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		prepared.Release()
		h.writeError(w, &executor.Failure{Code: executor.CodeInternal, Message: "this response cannot be streamed"})
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	stream := &frameWriter{w: w, flush: flusher.Flush, id: prepared.ID(), redact: h.redact}
	if err := stream.started(); err != nil {
		prepared.Release()
		log.Debug().Err(err).Str("id", prepared.ID()).Msg("caller went away before the verification started")
		return
	}
	result, failure := prepared.Execute(stream)
	if failure != nil {
		stream.done(nil, failure)
		return
	}
	stream.done(result, nil)
}

func (h *Handler) gitStatus(w http.ResponseWriter, r *http.Request) {
	var req executor.GitStatusRequest
	if failure := decode(r, unaryBodyLimit, &req); failure != nil {
		h.writeError(w, failure)
		return
	}
	status, failure := h.svc.GitStatus(r.Context(), req)
	if failure != nil {
		h.writeError(w, failure)
		return
	}
	h.writeJSON(w, http.StatusOK, status)
}

func (h *Handler) gitDiff(w http.ResponseWriter, r *http.Request) {
	var req executor.GitDiffRequest
	if failure := decode(r, unaryBodyLimit, &req); failure != nil {
		h.writeError(w, failure)
		return
	}
	diff, failure := h.svc.GitDiff(r.Context(), req)
	if failure != nil {
		h.writeError(w, failure)
		return
	}
	h.writeJSON(w, http.StatusOK, diff)
}

func (h *Handler) gitLog(w http.ResponseWriter, r *http.Request) {
	var req executor.GitLogRequest
	if failure := decode(r, unaryBodyLimit, &req); failure != nil {
		h.writeError(w, failure)
		return
	}
	commits, failure := h.svc.GitLog(r.Context(), req)
	if failure != nil {
		h.writeError(w, failure)
		return
	}
	h.writeJSON(w, http.StatusOK, commits)
}

// commitPush scrubs the request's own GitHub token from whatever it answers,
// on top of the keys every answer is scrubbed of.
func (h *Handler) commitPush(w http.ResponseWriter, r *http.Request) {
	var req executor.CommitPushRequest
	if failure := decode(r, unaryBodyLimit, &req); failure != nil {
		h.writeError(w, failure)
		return
	}
	result, failure := h.svc.CommitPush(r.Context(), req)
	if failure != nil {
		h.writeError(w, failure, req.GitHubToken)
		return
	}
	h.write(w, http.StatusOK, result, req.GitHubToken)
}
