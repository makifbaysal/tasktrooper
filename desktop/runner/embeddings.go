package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// embeddings.create — the tenant's embeddings, computed on this Mac.
//
// TaskTrooper indexes repositories by embedding them, and the model that does
// it runs on this Mac's bundled embedding engine rather than at a provider.
// The cloud reaches it the only way it can reach anything here: down this
// tunnel.
//
// This is a proxy and nothing more. It does not batch, cache, retry or
// reshape: the request is OpenAI-compatible on the way in and the response is
// the embedding engine's own on the way out, so the caller's client is the
// same one it would use against any embeddings endpoint. What it DOES do is
// pin the model.

// embeddingsTimeout bounds one request. Generous because a cold model is
// loaded on the first request and that takes seconds; bounded because a call
// that never returns holds a tunnel slot until the session ends.
const embeddingsTimeout = 2 * time.Minute

// embeddingsResponseLimit bounds the body read back. A batch of 768-dimension
// vectors is large but not unbounded, and the embedder is a local process
// whose output this program otherwise copies into memory without asking how
// big it is.
const embeddingsResponseLimit = 64 * 1024 * 1024

type embeddingsParams struct {
	// Input is a string or an array of strings, exactly as OpenAI's API takes
	// it. Passed through rather than parsed: this program has no opinion about
	// the shape of the text, and re-encoding it would be a chance to change it.
	Input json.RawMessage `json:"input"`
	// Model is optional. When present it must be the pinned one — see below.
	Model string `json:"model,omitempty"`
	// EncodingFormat is OpenAI's "float" or "base64", passed through.
	EncodingFormat string `json:"encoding_format,omitempty"`
}

// createEmbeddings returns the embedding engine's status code and its body,
// both untouched, or an error of this Mac's own when the request never got
// that far.
//
// The status travels because it carries meaning this side cannot reconstruct.
// A 503 from the engine means "the model is not loaded yet"; re-labelling it
// as a 502 from this Mac would leave the caller looking for a network problem
// that does not exist. Only "could not reach the engine at all" and "refused
// before it was sent" are statuses this program invents.
func createEmbeddings(c *call) (int, []byte, *rpcError) {
	var p embeddingsParams
	if err := json.Unmarshal(c.params, &p); err != nil {
		return 0, nil, failure(codeBadRequest, "embeddings.create params are not the expected object: %v", err)
	}
	if len(p.Input) == 0 {
		return 0, nil, failure(codeBadRequest, "input is required")
	}

	// A machine with no local embedding engine configured is an ordinary
	// state, not something every machine has. Absence is not a failure of
	// this Mac's own — it is answered the way mobile.* answers a capability
	// this Mac lacks, not as codeUpstream, because nothing here failed to
	// reach anything.
	base := c.embeddingsBaseURL()
	if base == "" || c.cfg.embeddingModel == "" {
		return 0, nil, failure(codeNotReady, "this machine has no local embedding engine configured")
	}

	// The pin, and it is a refusal rather than a correction.
	//
	// Every vector in this tenant's indexes was produced by one model, and
	// vectors from two models are not comparable — they do not even have to
	// share a dimension count. Silently substituting the pinned model for the
	// one that was asked for would produce results that look fine and are
	// meaningless, which is strictly worse than an error naming both.
	if p.Model != "" && p.Model != c.cfg.embeddingModel {
		return 0, nil, failure(codeBadRequest,
			"this machine is pinned to %q and was asked for %q; a vector from another model cannot be compared with the ones already indexed",
			c.cfg.embeddingModel, p.Model)
	}

	body, err := marshalRequest(c.cfg.embeddingModel, p)
	if err != nil {
		return 0, nil, failure(codeBadRequest, "%v", err)
	}

	ctx, cancel := context.WithTimeout(c.ctx, embeddingsTimeout)
	defer cancel()

	url := base + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, failure(codeInternal, "building the request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if c.ctx.Err() != nil {
			return 0, nil, failure(codeCancelled, "the call was cancelled")
		}
		// The address is in the message on purpose: "the engine is not running"
		// and "the engine is running somewhere else" are the two causes, and the
		// address separates them.
		return 0, nil, failure(codeUpstream, "could not reach the embedding engine at %s (%v)", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	answer, err := io.ReadAll(io.LimitReader(resp.Body, embeddingsResponseLimit))
	if err != nil {
		return 0, nil, failure(codeUpstream, "reading the embedding engine's answer: %v", err)
	}

	// Not decoded, not reshaped, not even required to be a success. The caller
	// runs an OpenAI-compatible client against this, and that client already
	// knows what a 503 with an `error` object means.
	if !json.Valid(answer) {
		return 0, nil, failure(codeUpstream, "the embedding engine answered %d with something that is not JSON", resp.StatusCode)
	}
	return resp.StatusCode, answer, nil
}

// marshalRequest builds the body the embedding engine receives: the pinned
// model, the caller's input verbatim, and the encoding format only when one
// was asked for.
func marshalRequest(model string, p embeddingsParams) ([]byte, error) {
	payload := map[string]any{
		"model": model,
		"input": p.Input,
	}
	if p.EncodingFormat != "" {
		if p.EncodingFormat != "float" && p.EncodingFormat != "base64" {
			return nil, fmt.Errorf("encoding_format %q is neither float nor base64", p.EncodingFormat)
		}
		payload["encoding_format"] = p.EncodingFormat
	}
	return json.Marshal(payload)
}
