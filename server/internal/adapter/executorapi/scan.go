package executorapi

import (
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/executor"
)

type scanRequest struct {
	ID string `json:"id,omitempty"`
	executor.ScanRequest
}

// scan streams one repository scan of a checkout in the envelope agent.run
// uses: started, a scan_stage event per stage the scanner reports, and one
// done frame whose result is the domain.ScanResult.
func (h *Handler) scan(w http.ResponseWriter, r *http.Request) {
	var req scanRequest
	if failure := decode(r, unaryBodyLimit, &req); failure != nil {
		h.writeError(w, failure)
		return
	}
	prepared, failure := h.svc.PrepareScan(r.Context(), req.ID, req.ScanRequest)
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
		log.Debug().Err(err).Str("id", prepared.ID()).Msg("caller went away before the scan started")
		return
	}
	result, failure := prepared.Execute(stream)
	if failure != nil {
		stream.done(nil, failure)
		return
	}
	stream.done(result, nil)
}
