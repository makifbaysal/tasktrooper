package skillvectors

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/catalogrepo"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const fakeDims = 8

func fakeVector(text string) []float32 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	sum := h.Sum64()
	vec := make([]float32, fakeDims)
	for i := range vec {
		vec[i] = float32((sum>>(8*i))&0xff) / 255
	}
	return vec
}

// fakeEngine is the desktop embedder's surface: one model, and a 503 while it
// loads.
type fakeEngine struct {
	calls     atomic.Int32
	notReady  atomic.Int32
	wrongDims atomic.Bool
}

func (f *fakeEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/v1/models":
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": domain.PinnedLocalEmbeddingModel}}})
	case "/v1/embeddings":
		if f.notReady.Load() > 0 {
			f.notReady.Add(-1)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"code":"not_ready"}}`))
			return
		}
		var body struct {
			Input string `json:"input"`
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.calls.Add(1)
		vec := fakeVector(body.Input)
		if f.wrongDims.Load() {
			vec = vec[:fakeDims-1]
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"embedding": vec}}})
	default:
		http.NotFound(w, r)
	}
}

func writeSkill(t *testing.T, catalog, agent, name, description, body string) {
	t.Helper()
	dir := filepath.Join(catalog, "agents", agent)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "skills", name), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "catalog.yaml"), []byte("name: "+agent+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("You are "+agent+".\n"), 0o644))
	doc := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skills", name, "SKILL.md"), []byte(doc), 0o644))
}

func fixtureCatalog(t *testing.T) string {
	catalog := t.TempDir()
	writeSkill(t, catalog, "backend-developer", "api-design", "one error shape", "# API design")
	writeSkill(t, catalog, "backend-developer", "board-comment-style", "how to comment", "# Comments")
	writeSkill(t, catalog, "frontend-developer", "board-comment-style", "how to comment", "# Comments")
	return catalog
}

func newEngine(t *testing.T) (*fakeEngine, string) {
	engine := &fakeEngine{}
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return engine, server.URL + "/v1"
}

func TestBuildEmbedsEachDistinctSkillTextOnceAndTheServerFindsIt(t *testing.T) {
	catalog := fixtureCatalog(t)
	engine, url := newEngine(t)
	engine.notReady.Store(1)

	report, err := Build(context.Background(), Options{CatalogDir: catalog, EmbeddingsURL: url})

	require.NoError(t, err)
	assert.Equal(t, 3, report.Skills)
	assert.Equal(t, 2, report.Unique, "a skill copied under two agents is one text")
	assert.Equal(t, 2, report.Embedded)
	assert.EqualValues(t, 2, engine.calls.Load())
	assert.Equal(t, fakeDims, report.Dims)

	set, err := (&catalogrepo.Reader{Source: catalog}).SkillVectors(context.Background())
	require.NoError(t, err)
	text := domain.SkillEmbeddingText("api-design", "one error shape", "# API design")
	vec, ok := set.Lookup(domain.PinnedLocalEmbeddingModel, domain.EmbeddingSourceONNXInt8, text)
	require.True(t, ok)
	assert.Equal(t, fakeVector(text), vec)
	_, ok = set.Lookup(domain.PinnedLocalEmbeddingModel, domain.EmbeddingSourceTEI, text)
	assert.False(t, ok)
}

func TestARebuildEmbedsOnlyTheSkillsThatChanged(t *testing.T) {
	catalog := fixtureCatalog(t)
	engine, url := newEngine(t)
	_, err := Build(context.Background(), Options{CatalogDir: catalog, EmbeddingsURL: url})
	require.NoError(t, err)
	writeSkill(t, catalog, "backend-developer", "api-design", "one error shape", "# API design, revised")
	missing, stale, err := Check(context.Background(), catalog, "")
	require.NoError(t, err)
	assert.Equal(t, 1, missing)
	assert.Equal(t, 1, stale)
	engine.calls.Store(0)

	report, err := Build(context.Background(), Options{CatalogDir: catalog, EmbeddingsURL: url})

	require.NoError(t, err)
	assert.Equal(t, 1, report.Embedded)
	assert.Equal(t, 1, report.Reused)
	assert.Equal(t, 1, report.Dropped)
	assert.EqualValues(t, 1, engine.calls.Load())
	missing, stale, err = Check(context.Background(), catalog, "")
	require.NoError(t, err)
	assert.Zero(t, missing)
	assert.Zero(t, stale)
}

func TestBuildRefusesAnEngineThatChangesItsDimensions(t *testing.T) {
	catalog := fixtureCatalog(t)
	engine, url := newEngine(t)
	_, err := Build(context.Background(), Options{CatalogDir: catalog, EmbeddingsURL: url})
	require.NoError(t, err)
	writeSkill(t, catalog, "backend-developer", "api-design", "one error shape", "# changed")
	engine.wrongDims.Store(true)

	_, err = Build(context.Background(), Options{CatalogDir: catalog, EmbeddingsURL: url})

	assert.Error(t, err)
}
