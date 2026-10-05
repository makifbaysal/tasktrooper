package activity

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type ctxKey int

const recorderKey ctxKey = 1

const persistTimeout = 10 * time.Second

type Recorder struct {
	store port.ActivityStore
	runID uuid.UUID
	ctx   context.Context
	tags  map[string]string
}

func StartRun(ctx context.Context, store port.ActivityStore, sessionID *uuid.UUID, requestID, model string) (context.Context, *Recorder, error) {
	if store == nil {
		return ctx, nil, nil
	}
	run, err := store.CreateRun(ctx, sessionID, requestID, model)
	if err != nil {
		return ctx, nil, err
	}
	rec := &Recorder{store: store, runID: run.ID, ctx: ctx}
	return context.WithValue(ctx, recorderKey, rec), rec, nil
}

func FromContext(ctx context.Context) *Recorder {
	rec, _ := ctx.Value(recorderKey).(*Recorder)
	return rec
}

// WithStepTag makes every object-shaped step recorded under the returned ctx
// carry key=value, so interleaved parallel work stays attributable. A key the
// caller already put in the payload is never overwritten.
func WithStepTag(ctx context.Context, key, value string) context.Context {
	rec := FromContext(ctx)
	if rec == nil {
		return ctx
	}
	tags := make(map[string]string, len(rec.tags)+1)
	for k, v := range rec.tags {
		tags[k] = v
	}
	tags[key] = value
	clone := *rec
	clone.tags = tags
	return context.WithValue(ctx, recorderKey, &clone)
}

func mergeTags(data []byte, tags map[string]string) []byte {
	if len(tags) == 0 {
		return data
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return data
	}
	for k, v := range tags {
		if _, set := obj[k]; set {
			continue
		}
		raw, err := json.Marshal(v)
		if err != nil {
			continue
		}
		obj[k] = raw
	}
	merged, err := json.Marshal(obj)
	if err != nil {
		return data
	}
	return merged
}

func (r *Recorder) RunID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return r.runID
}

func (r *Recorder) persistCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(r.ctx), persistTimeout)
}

func (r *Recorder) Step(stepType string, payload any) {
	if r == nil || r.store == nil {
		return
	}
	data, err := json.Marshal(payload)

	if err != nil || len(data) == 0 || string(data) == "null" {
		data = []byte("{}")
	}
	data = mergeTags(data, r.tags)
	ctx, cancel := r.persistCtx()
	defer cancel()
	_ = r.store.AppendStep(ctx, r.runID, stepType, data)
}

func (r *Recorder) Steps(ctx context.Context) []domain.SessionStep {
	if r == nil || r.store == nil {
		return nil
	}
	steps, err := r.store.ListStepsByRun(ctx, r.runID)
	if err != nil {
		log.Debug().Err(err).Str("run_id", r.runID.String()).Msg("activity steps read-back failed")
		return nil
	}
	return steps
}

func (r *Recorder) Complete(status string) {
	if r == nil || r.store == nil {
		return
	}
	ctx, cancel := r.persistCtx()
	defer cancel()
	_ = r.store.CompleteRun(ctx, r.runID, status)
}
