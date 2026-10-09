// Package skillvectors builds the skill vectors a catalog ships with: every
// skill under catalog/agents/*/skills embedded once, with the desktop's
// embedder, so no install embeds them again.
package skillvectors

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/catalogrepo"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/llm"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type Options struct {
	CatalogDir string
	// Out defaults to CatalogDir/skills.vectors.json.
	Out string
	// EmbeddingsURL is the OpenAI-compatible base the desktop embedder
	// serves, e.g. http://127.0.0.1:<port>/v1.
	EmbeddingsURL string
	// Source names that engine; empty means the desktop's int8 ONNX build.
	Source string
	// Timeout bounds one embedding request; the first one also loads the model.
	Timeout time.Duration
	// Unavailable is how long an engine that answers "not ready" is waited for.
	Unavailable time.Duration
	Log         func(format string, args ...any)
}

type Report struct {
	Skills   int
	Unique   int
	Embedded int
	Reused   int
	Dropped  int
	Model    string
	Dims     int
	Out      string
}

func (o *Options) defaults() {
	if o.Out == "" {
		o.Out = filepath.Join(o.CatalogDir, catalogrepo.SkillVectorsFile)
	}
	if o.Source == "" {
		o.Source = domain.EmbeddingSourceONNXInt8
	}
	if o.Timeout <= 0 {
		o.Timeout = 2 * time.Minute
	}
	if o.Unavailable <= 0 {
		o.Unavailable = 5 * time.Minute
	}
	if o.Log == nil {
		o.Log = func(string, ...any) {}
	}
}

// SkillTexts is every distinct text the catalog's skills are embedded from,
// by key.
func SkillTexts(ctx context.Context, catalogDir string) (map[string]string, int, error) {
	reader := &catalogrepo.Reader{Source: catalogDir}
	agents, _, err := reader.ReadCatalog(ctx)
	if err != nil {
		return nil, 0, err
	}
	texts := make(map[string]string)
	skills := 0
	for _, agent := range agents {
		for _, sk := range agent.Skills {
			text := domain.SkillEmbeddingText(sk.Name, sk.Description, sk.Content)
			texts[domain.SkillVectorKey(text)] = text
			skills++
		}
	}
	return texts, skills, nil
}

// Build writes the vectors file. Vectors already in it for the same model and
// engine are kept for skills whose text did not change, and vectors of texts
// no skill has any more are dropped.
func Build(ctx context.Context, opts Options) (Report, error) {
	opts.defaults()
	if strings.TrimSpace(opts.EmbeddingsURL) == "" {
		return Report{}, errors.New("the embeddings URL is required")
	}
	texts, skills, err := SkillTexts(ctx, opts.CatalogDir)
	if err != nil {
		return Report{}, err
	}
	client := llm.NewOpenAICompatClient(opts.EmbeddingsURL, "", "", opts.Timeout)
	model, err := servedModel(ctx, client)
	if err != nil {
		return Report{}, err
	}
	previous := readExisting(opts.Out, model, opts.Source)

	report := Report{Skills: skills, Unique: len(texts), Model: model, Out: opts.Out}
	set := domain.NewSkillVectorSet(model, opts.Source, 0)
	dims := 0
	for i, key := range slices.Sorted(maps.Keys(texts)) {
		if vec, ok := previous.Vector(key); ok {
			set.Put(key, vec)
			report.Reused++
			continue
		}
		vec, err := embed(ctx, client, model, texts[key], opts)
		if err != nil {
			return report, fmt.Errorf("embed skill text %s: %w", key[:12], err)
		}
		if dims == 0 {
			dims = len(vec)
		}
		if len(vec) != dims || (previous != nil && previous.Dims != dims) {
			return report, fmt.Errorf("the engine answered %d dimensions for %s after %d before", len(vec), key[:12], dims)
		}
		set.Put(key, vec)
		report.Embedded++
		opts.Log("embedded %d/%d", i+1, len(texts))
	}
	if dims == 0 && previous != nil {
		dims = previous.Dims
	}
	if dims == 0 {
		return report, errors.New("no skill was embedded and no earlier vectors were reused")
	}
	set.Dims = dims
	report.Dims = dims
	if previous != nil {
		report.Dropped = previous.Len() - report.Reused
	}
	raw, err := catalogrepo.EncodeSkillVectors(set)
	if err != nil {
		return report, err
	}
	tmp := opts.Out + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return report, err
	}
	return report, os.Rename(tmp, opts.Out)
}

// Check reports the catalog skills the vectors file has no vector for, and
// the vectors no skill uses any more, without an embedder.
func Check(ctx context.Context, catalogDir, file string) (missing, stale int, err error) {
	texts, _, err := SkillTexts(ctx, catalogDir)
	if err != nil {
		return 0, 0, err
	}
	if file == "" {
		file = filepath.Join(catalogDir, catalogrepo.SkillVectorsFile)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return 0, 0, err
	}
	set, err := catalogrepo.ParseSkillVectors(raw)
	if err != nil {
		return 0, 0, err
	}
	for key := range texts {
		if _, ok := set.Vector(key); !ok {
			missing++
		}
	}
	for _, key := range set.Keys() {
		if _, ok := texts[key]; !ok {
			stale++
		}
	}
	return missing, stale, nil
}

// servedModel pins the build to the one model the engine says it serves,
// which is what the executor and the server pin their own calls to.
func servedModel(ctx context.Context, client port.LLMClient) (string, error) {
	models, err := client.Models(ctx)
	if err != nil {
		return "", fmt.Errorf("ask the embedding engine which model it serves: %w", err)
	}
	for _, m := range models {
		if m = strings.TrimSpace(m); m != "" {
			return m, nil
		}
	}
	return "", errors.New("the embedding engine lists no model")
}

func readExisting(path, model, source string) *domain.SkillVectorSet {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	set, err := catalogrepo.ParseSkillVectors(raw)
	if err != nil || set.Model != model || set.Source != source {
		return nil
	}
	return set
}

func embed(ctx context.Context, client port.LLMClient, model, text string, opts Options) ([]float32, error) {
	deadline := time.Now().Add(opts.Unavailable)
	wait := time.Second
	for {
		vec, err := client.Embed(ctx, text, model)
		if err == nil || !notReady(err) || time.Now().After(deadline) {
			return vec, err
		}
		opts.Log("the embedding engine is not ready (%v); waiting %s", err, wait)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		wait = min(2*wait, 15*time.Second)
	}
}

// notReady is an engine still starting or loading its model: the desktop
// embedder binds its port first and answers 503 until the model is on disk.
// A refused key is not worth waiting for.
func notReady(err error) bool {
	var unavailable *llm.EmbeddingUnavailableError
	if errors.As(err, &unavailable) {
		return unavailable.StatusCode == 0
	}
	msg := err.Error()
	for _, status := range []string{"502", "503", "504"} {
		if strings.Contains(msg, "embeddings returned "+status) {
			return true
		}
	}
	return false
}
