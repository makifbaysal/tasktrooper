package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// The control plane parks calls for this computer and this program collects
// them over plain HTTPS: nothing is held open between calls, so the cloud can
// scale to zero while this computer sits idle. See the control plane's
// runnertunnel package for the other half.
const (
	mailboxHelloPath = "hello"
	mailboxCallsPath = "calls/"

	// flushEvery batches a streaming answer into one post per interval
	// rather than one per line.
	flushEvery = 250 * time.Millisecond
	// keepaliveEvery posts an empty chunk while a call is quiet, so the
	// control plane can tell a silent agent from a computer that is gone.
	keepaliveEvery = time.Minute
	// defaultHelloEvery is used until the control plane names its own.
	defaultHelloEvery = 30 * time.Minute

	mailboxRequestTimeout = 30 * time.Second
	postAttempts          = 4
)

// errCallGone is the control plane saying it no longer wants this answer.
var errCallGone = errors.New("the control plane gave up on this call")

type mailboxClient struct {
	base   string
	token  string
	tenant string
	http   *http.Client
}

func newMailboxClient(cfg config) *mailboxClient {
	return &mailboxClient{
		base:   cfg.runnerURL,
		token:  cfg.runnerToken,
		tenant: cfg.tenantID,
		http:   &http.Client{Timeout: mailboxRequestTimeout},
	}
}

func (m *mailboxClient) post(ctx context.Context, path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+m.token)
	req.Header.Set("X-Runner-Tenant", m.tenant)
	return m.http.Do(req)
}

// postOK posts once; hello and claim leave retrying to run's backoff.
func (m *mailboxClient) postOK(ctx context.Context, path string, body []byte) ([]byte, error) {
	return m.postTries(ctx, path, body, 1)
}

// postRetried retries a transport failure or a 5xx: a chunk of a live answer
// is worth a few tries before the call is given up. A 410 is errCallGone, a
// 401/403 an *authError; neither is retried.
func (m *mailboxClient) postRetried(ctx context.Context, path string, body []byte) ([]byte, error) {
	return m.postTries(ctx, path, body, postAttempts)
}

func (m *mailboxClient) postTries(ctx context.Context, path string, body []byte, tries int) ([]byte, error) {
	var last error
	for attempt := 0; attempt < tries; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		resp, err := m.post(ctx, path, body)
		if err != nil {
			last = err
			continue
		}
		out, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		_ = resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return nil, &authError{status: resp.StatusCode}
		case resp.StatusCode == http.StatusGone:
			return nil, errCallGone
		case resp.StatusCode >= 500:
			last = fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
			continue
		case resp.StatusCode >= 300:
			return nil, fmt.Errorf("%s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(out)))
		}
		return out, nil
	}
	return nil, last
}

type helloAnswer struct {
	HelloIntervalSeconds int `json:"hello_interval_seconds"`
}

func (m *mailboxClient) hello(ctx context.Context) (helloAnswer, error) {
	out, err := m.postOK(ctx, mailboxHelloPath, nil)
	if err != nil {
		return helloAnswer{}, err
	}
	var a helloAnswer
	_ = json.Unmarshal(out, &a)
	return a, nil
}

type mailboxCall struct {
	ID     string      `json:"id"`
	Method string      `json:"method"`
	Path   string      `json:"path"`
	Query  string      `json:"query"`
	Header http.Header `json:"header"`
	Body   []byte      `json:"body"`
}

func (m *mailboxClient) claim(ctx context.Context) ([]mailboxCall, error) {
	out, err := m.postOK(ctx, mailboxCallsPath+"claim", nil)
	if err != nil {
		return nil, err
	}
	var a struct {
		Calls []mailboxCall `json:"calls"`
	}
	if err := json.Unmarshal(out, &a); err != nil {
		return nil, fmt.Errorf("claim answer: %w", err)
	}
	return a.Calls, nil
}

// connectAndServe says hello, then collects and serves calls at start,
// whenever this computer is woken (state.wake) and at each hello. There is no
// poll: an idle computer costs the cloud nothing. It returns how long it was
// connected; run reconnects with backoff on any error but an *authError.
func connectAndServe(ctx context.Context, cfg config, state *state) (time.Duration, error) {
	client := newMailboxClient(cfg)
	ans, err := client.hello(ctx)
	if err != nil {
		return 0, err
	}
	helloEvery := defaultHelloEvery
	if ans.HelloIntervalSeconds > 0 {
		helloEvery = time.Duration(ans.HelloIntervalSeconds) * time.Second
	}

	sessionCtx, endSession := context.WithCancel(ctx)
	runner := newRunnerServer(cfg, state)
	handler := runner.handler()
	var calls sync.WaitGroup
	var served atomic.Int64
	// The two lines and the reconnect warning are a contract with the
	// desktop's runner-log.ts, which still calls this state "tunnel".
	log.Info().Msg("tunnel attached")
	connectedAt := time.Now()
	defer func() {
		endSession()
		runner.drain()
		calls.Wait()
		log.Info().Int64("streams", served.Load()).Msg("tunnel detached")
	}()

	helloTick := time.NewTicker(helloEvery)
	defer helloTick.Stop()

	for {
		pending, err := client.claim(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return time.Since(connectedAt), nil
			}
			return time.Since(connectedAt), fmt.Errorf("collecting calls: %w", err)
		}
		for _, c := range pending {
			served.Add(1)
			calls.Add(1)
			go func(c mailboxCall) {
				defer calls.Done()
				serveMailboxCall(sessionCtx, client, handler, c)
			}(c)
		}

		select {
		case <-ctx.Done():
			return time.Since(connectedAt), nil
		case <-state.wakeChan():
		case <-helloTick.C:
			if _, err := client.hello(ctx); err != nil {
				return time.Since(connectedAt), err
			}
		}
	}
}

// serveMailboxCall runs one collected call against this program's own
// handler and streams the answer back.
func serveMailboxCall(ctx context.Context, client *mailboxClient, handler http.Handler, c mailboxCall) {
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	path := c.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	req, err := http.NewRequestWithContext(callCtx, c.Method, "http://runner"+path, bytes.NewReader(c.Body))
	if err != nil {
		_, _ = client.postRetried(ctx, mailboxCallsPath+c.ID+"/end", endBody(err.Error()))
		return
	}
	req.URL.RawQuery = c.Query
	for k, v := range c.Header {
		req.Header[k] = v
	}
	req.ContentLength = int64(len(c.Body))

	w := newMailboxWriter(callCtx, client, c.ID, cancel)
	handler.ServeHTTP(w, req)
	w.finish()
}

func endBody(msg string) []byte {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return b
}

// mailboxWriter is the http.ResponseWriter a collected call is served into.
// It posts the head once the handler commits it, then the body in batches
// (on Flush, or every flushEvery), and an empty chunk when the answer has
// been quiet for keepaliveEvery.
type mailboxWriter struct {
	ctx    context.Context
	client *mailboxClient
	id     string
	cancel context.CancelFunc

	header http.Header

	mu          sync.Mutex
	status      int
	wroteHeader bool
	headSent    bool
	buf         bytes.Buffer
	seq         int
	lastPost    time.Time
	gone        bool
	failed      error

	kick chan struct{}
	stop chan struct{}
	done chan struct{}
}

func newMailboxWriter(ctx context.Context, client *mailboxClient, id string, cancel context.CancelFunc) *mailboxWriter {
	w := &mailboxWriter{
		ctx: ctx, client: client, id: id, cancel: cancel,
		header:   http.Header{},
		lastPost: time.Now(),
		kick:     make(chan struct{}, 1),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go w.pump()
	return w
}

func (w *mailboxWriter) Header() http.Header { return w.header }

func (w *mailboxWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
}

func (w *mailboxWriter) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.gone {
		return 0, errCallGone
	}
	w.buf.Write(p)
	return len(p), nil
}

func (w *mailboxWriter) Flush() {
	w.WriteHeader(http.StatusOK)
	select {
	case w.kick <- struct{}{}:
	default:
	}
}

func (w *mailboxWriter) pump() {
	defer close(w.done)
	tick := time.NewTicker(flushEvery)
	defer tick.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-w.kick:
		case <-tick.C:
		}
		if err := w.send(false); err != nil {
			w.markFailed(err)
			return
		}
	}
}

// send posts the head if it is due and the buffered body, or a keepalive.
func (w *mailboxWriter) send(final bool) error {
	w.mu.Lock()
	if w.gone {
		w.mu.Unlock()
		return errCallGone
	}
	if !w.wroteHeader {
		w.mu.Unlock()
		return nil
	}
	needHead := !w.headSent
	status := w.status
	header := w.header.Clone()
	chunk := append([]byte(nil), w.buf.Bytes()...)
	w.buf.Reset()
	seq := w.seq
	quiet := time.Since(w.lastPost) >= keepaliveEvery
	w.mu.Unlock()

	if needHead {
		head, _ := json.Marshal(map[string]any{"status": status, "header": header})
		if _, err := w.client.postRetried(w.ctx, mailboxCallsPath+w.id+"/head", head); err != nil {
			w.requeue(chunk)
			return err
		}
		w.mu.Lock()
		w.headSent = true
		w.lastPost = time.Now()
		w.mu.Unlock()
	}
	if len(chunk) == 0 && !quiet && !final {
		return nil
	}
	if len(chunk) == 0 && final {
		return nil
	}
	if _, err := w.client.postRetried(w.ctx, mailboxCallsPath+w.id+"/body?seq="+strconv.Itoa(seq), chunk); err != nil {
		w.requeue(chunk)
		return err
	}
	w.mu.Lock()
	w.seq = seq + 1
	w.lastPost = time.Now()
	w.mu.Unlock()
	return nil
}

func (w *mailboxWriter) requeue(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	w.mu.Lock()
	rest := append([]byte(nil), w.buf.Bytes()...)
	w.buf.Reset()
	w.buf.Write(chunk)
	w.buf.Write(rest)
	w.mu.Unlock()
}

func (w *mailboxWriter) markFailed(err error) {
	w.mu.Lock()
	w.gone = true
	w.failed = err
	w.mu.Unlock()
	if !errors.Is(err, context.Canceled) {
		log.Warn().Err(err).Str("call", w.id).Msg("answering a call failed; stopping it")
	}
	w.cancel()
}

// finish posts what is left and ends the call. The handler has returned.
func (w *mailboxWriter) finish() {
	close(w.stop)
	<-w.done
	w.WriteHeader(http.StatusOK)
	w.mu.Lock()
	failed := w.failed
	w.mu.Unlock()
	if failed != nil {
		return
	}
	// The handler's own context may be cancelled by now (a cancel call); the
	// answer still has to reach the control plane, so the tail is posted on
	// a context of its own.
	tail, cancel := context.WithTimeout(context.WithoutCancel(w.ctx), 2*mailboxRequestTimeout)
	defer cancel()
	w.ctx = tail
	if err := w.send(true); err != nil {
		if !errors.Is(err, errCallGone) {
			log.Warn().Err(err).Str("call", w.id).Msg("posting the end of an answer failed")
		}
		return
	}
	if _, err := w.client.postRetried(tail, mailboxCallsPath+w.id+"/end", []byte("{}")); err != nil && !errors.Is(err, errCallGone) {
		log.Warn().Err(err).Str("call", w.id).Msg("ending a call failed")
	}
}
