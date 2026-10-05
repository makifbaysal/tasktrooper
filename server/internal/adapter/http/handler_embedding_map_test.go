package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/embedmap"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeEmbedMapStore is an in-memory port.EmbeddingMapStore. The projection
// itself is covered by the embedmap package's own tests; this one is about the
// wire contract the web client codes against.
type fakeEmbedMapStore struct {
	stats     port.EmbeddingFileStats
	repos     []port.EmbeddingRepositorySource
	total     int
	chunks    []port.EmbeddingChunk
	lastLimit int

	embeddings map[uuid.UUID][]float32
}

var _ port.EmbeddingMapStore = (*fakeEmbedMapStore)(nil)

func (f *fakeEmbedMapStore) FileStats(context.Context) (port.EmbeddingFileStats, error) {
	return f.stats, nil
}

func (f *fakeEmbedMapStore) ListRepositorySources(context.Context) ([]port.EmbeddingRepositorySource, error) {
	return f.repos, nil
}

func (f *fakeEmbedMapStore) RepositorySource(_ context.Context, id uuid.UUID) (port.EmbeddingRepositorySource, error) {
	for _, r := range f.repos {
		if r.RepositoryID == id {
			return r, nil
		}
	}
	return port.EmbeddingRepositorySource{}, port.ErrNotFound
}

func (f *fakeEmbedMapStore) SampleFileChunks(_ context.Context, limit int) (int, []port.EmbeddingChunk, error) {
	f.lastLimit = limit
	return f.total, f.chunks, nil
}

func (f *fakeEmbedMapStore) SampleCodeChunks(_ context.Context, _ uuid.UUID, limit int) (int, []port.EmbeddingChunk, error) {
	f.lastLimit = limit
	return f.total, f.chunks, nil
}

func (f *fakeEmbedMapStore) ChunkEmbeddings(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]float32, error) {
	out := map[uuid.UUID][]float32{}
	for _, id := range ids {
		if v, ok := f.embeddings[id]; ok {
			out[id] = v
		}
	}
	return out, nil
}

func embedMapChunks(n, dim int) []port.EmbeddingChunk {
	out := make([]port.EmbeddingChunk, n)
	for i := range out {
		v := make([]float32, dim)
		for j := range v {
			v[j] = float32(math.Cos(float64((i+1)*(j+2))) + 1.5)
		}
		out[i] = port.EmbeddingChunk{
			ID:         uuid.NewSHA1(uuid.Nil, []byte{byte(i)}).String(),
			GroupID:    "internal/port/git.go",
			GroupLabel: "internal/port/git.go",
			ChunkIndex: i,
			Content:    "func Clone(ctx context.Context)",
			Language:   "go",
			Symbol:     "Clone",
			Embedding:  v,
		}
	}
	return out
}

func newEmbedMapTestApp(store *fakeEmbedMapStore) *fiber.App {
	h := &Handler{embedMapSvc: embedmap.New(store)}
	app := fiber.New()
	h.registerEmbeddingMapRoutes(app)
	return app
}

func getJSON(t *testing.T, app *fiber.App, target string, out any) int {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest("GET", target, nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	if out != nil {
		require.NoError(t, json.Unmarshal(body, out))
	}
	return resp.StatusCode
}

func TestEmbeddingMapSourcesShape(t *testing.T) {
	indexedAt := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	repoID, indexID := uuid.New(), uuid.New()
	app := newEmbedMapTestApp(&fakeEmbedMapStore{
		stats: port.EmbeddingFileStats{ChunkCount: 412, DocumentCount: 7},
		repos: []port.EmbeddingRepositorySource{{
			RepositoryID: repoID, Name: "local-llm", Branch: "main", IndexID: indexID,
			ChunkCount: 8321, FileCount: 640, IndexedAt: &indexedAt,
		}},
	})

	var body struct {
		Files struct {
			Available     bool `json:"available"`
			ChunkCount    int  `json:"chunk_count"`
			DocumentCount int  `json:"document_count"`
		} `json:"files"`
		Repositories []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Branch     string `json:"branch"`
			IndexID    string `json:"index_id"`
			ChunkCount int    `json:"chunk_count"`
			FileCount  int    `json:"file_count"`
			IndexedAt  string `json:"indexed_at"`
		} `json:"repositories"`
	}
	status := getJSON(t, app, "/v1/embedding-map/sources", &body)

	require.Equal(t, fiber.StatusOK, status)
	require.True(t, body.Files.Available)
	require.Equal(t, 412, body.Files.ChunkCount)
	require.Equal(t, 7, body.Files.DocumentCount)
	require.Len(t, body.Repositories, 1)
	require.Equal(t, repoID.String(), body.Repositories[0].ID)
	require.Equal(t, indexID.String(), body.Repositories[0].IndexID)
	require.Equal(t, "2026-08-05T10:00:00Z", body.Repositories[0].IndexedAt)
}

type embedMapBody struct {
	Source       string `json:"source"`
	RepositoryID string `json:"repository_id"`
	Branch       string `json:"branch"`
	Dimensions   int    `json:"dimensions"`
	Total        int    `json:"total"`
	Sampled      int    `json:"sampled"`
	Truncated    bool   `json:"truncated"`
	Points       []struct {
		ID         string    `json:"id"`
		GroupID    string    `json:"group_id"`
		GroupLabel string    `json:"group_label"`
		ChunkIndex int       `json:"chunk_index"`
		Snippet    string    `json:"snippet"`
		Language   string    `json:"language"`
		Symbol     string    `json:"symbol"`
		Vector     []float64 `json:"vector"`
	} `json:"points"`
}

func TestEmbeddingMapCodeShape(t *testing.T) {
	repoID := uuid.New()
	store := &fakeEmbedMapStore{
		repos:  []port.EmbeddingRepositorySource{{RepositoryID: repoID, IndexID: uuid.New(), Branch: "main"}},
		total:  8321,
		chunks: embedMapChunks(24, 96),
	}
	app := newEmbedMapTestApp(store)

	var body embedMapBody
	status := getJSON(t, app, "/v1/embedding-map?source=code&repository_id="+repoID.String(), &body)

	require.Equal(t, fiber.StatusOK, status)
	require.Equal(t, "code", body.Source)
	require.Equal(t, repoID.String(), body.RepositoryID)
	require.Equal(t, "main", body.Branch)
	require.Equal(t, 50, body.Dimensions)
	require.Equal(t, 8321, body.Total)
	require.Equal(t, 24, body.Sampled)
	require.True(t, body.Truncated)
	require.Len(t, body.Points, 24)
	require.Equal(t, "internal/port/git.go", body.Points[0].GroupLabel)
	require.Equal(t, "go", body.Points[0].Language)
	require.Equal(t, "Clone", body.Points[0].Symbol)
	for _, p := range body.Points {
		require.Len(t, p.Vector, body.Dimensions, "every vector is exactly `dimensions` long")
	}
}

func TestEmbeddingMapClampsLimitAndDims(t *testing.T) {
	store := &fakeEmbedMapStore{total: 10, chunks: embedMapChunks(10, 256)}
	app := newEmbedMapTestApp(store)

	var body embedMapBody
	status := getJSON(t, app, "/v1/embedding-map?source=files&limit=99999&dims=9999", &body)

	require.Equal(t, fiber.StatusOK, status)
	require.Equal(t, embedmap.MaxLimit, store.lastLimit)
	require.Equal(t, embedmap.MaxDims, body.Dimensions)
	require.False(t, body.Truncated)
	for _, p := range body.Points {
		require.Len(t, p.Vector, embedmap.MaxDims)
	}
}

func TestEmbeddingMapEmptySourceIsOK(t *testing.T) {
	app := newEmbedMapTestApp(&fakeEmbedMapStore{})

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/embedding-map?source=files", nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	require.Contains(t, string(raw), `"points":[]`, "an empty map serializes as [] not null")

	var body embedMapBody
	require.NoError(t, json.Unmarshal(raw, &body))
	require.Equal(t, 0, body.Total)
	require.Empty(t, body.Points)
}

func TestEmbeddingMapBadRequests(t *testing.T) {
	app := newEmbedMapTestApp(&fakeEmbedMapStore{})

	tests := []struct {
		name   string
		target string
	}{
		{name: "unknown source", target: "/v1/embedding-map?source=workspace"},
		{name: "missing source", target: "/v1/embedding-map"},
		{name: "code without repository", target: "/v1/embedding-map?source=code"},
		{name: "code with malformed repository", target: "/v1/embedding-map?source=code&repository_id=nope"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body errorResponse
			status := getJSON(t, app, tc.target, &body)
			require.Equal(t, fiber.StatusBadRequest, status)
			require.Equal(t, "invalid_request_error", body.Error.Type)
			require.NotEmpty(t, body.Error.Message)
		})
	}
}

// The routes must not exist at all when Postgres is absent, rather than
// answering 500 on every poll from the UI.
func TestEmbeddingMapRoutesAbsentWithoutService(t *testing.T) {
	h := &Handler{}
	app := fiber.New()
	h.registerEmbeddingMapRoutes(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/embedding-map?source=files", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusNotFound, resp.StatusCode)
}

func postLocate(t *testing.T, app *fiber.App, body string, out any) int {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/embedding-map/locate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	if out != nil {
		require.NoError(t, json.Unmarshal(raw, out))
	}
	return resp.StatusCode
}

func TestEmbeddingMapLocateShape(t *testing.T) {
	repoID, indexID := uuid.New(), uuid.New()
	anchor, hit := uuid.New(), uuid.New()
	app := newEmbedMapTestApp(&fakeEmbedMapStore{
		repos:      []port.EmbeddingRepositorySource{{RepositoryID: repoID, IndexID: indexID}},
		embeddings: map[uuid.UUID][]float32{anchor: {1, 0}, hit: {2, 0}},
	})
	body := fmt.Sprintf(`{"repository_id":%q,"anchor_ids":[%q],"chunk_ids":[%q,%q]}`,
		repoID, anchor, hit, uuid.New())

	var out struct {
		Locations []struct {
			ChunkID    string  `json:"chunk_id"`
			AnchorID   string  `json:"anchor_id"`
			Similarity float64 `json:"similarity"`
		} `json:"locations"`
	}
	require.Equal(t, fiber.StatusOK, postLocate(t, app, body, &out))
	require.Len(t, out.Locations, 1)
	require.Equal(t, hit.String(), out.Locations[0].ChunkID)
	require.Equal(t, anchor.String(), out.Locations[0].AnchorID)
	require.InDelta(t, 1, out.Locations[0].Similarity, 1e-9)
}

func TestEmbeddingMapLocateNoIndexIsEmptyArray(t *testing.T) {
	app := newEmbedMapTestApp(&fakeEmbedMapStore{})
	id := uuid.NewString()
	body := fmt.Sprintf(`{"repository_id":%q,"anchor_ids":[%q],"chunk_ids":[%q]}`, uuid.NewString(), id, id)
	resp, err := app.Test(func() *http.Request {
		r := httptest.NewRequest("POST", "/v1/embedding-map/locate", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		return r
	}())
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	require.JSONEq(t, `{"locations":[]}`, string(raw))
}

func TestEmbeddingMapLocateBadRequests(t *testing.T) {
	app := newEmbedMapTestApp(&fakeEmbedMapStore{})
	id := uuid.NewString()
	ids := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = fmt.Sprintf("%q", uuid.NewString())
		}
		return strings.Join(parts, ",")
	}
	cases := map[string]string{
		"malformed json":   `{`,
		"missing repo":     fmt.Sprintf(`{"anchor_ids":[%q],"chunk_ids":[%q]}`, id, id),
		"bad repo":         fmt.Sprintf(`{"repository_id":"x","anchor_ids":[%q],"chunk_ids":[%q]}`, id, id),
		"empty anchors":    fmt.Sprintf(`{"repository_id":%q,"anchor_ids":[],"chunk_ids":[%q]}`, id, id),
		"empty chunks":     fmt.Sprintf(`{"repository_id":%q,"anchor_ids":[%q],"chunk_ids":[]}`, id, id),
		"too many anchors": fmt.Sprintf(`{"repository_id":%q,"anchor_ids":[%s],"chunk_ids":[%q]}`, id, ids(embedmap.MaxLimit+1), id),
		"too many chunks":  fmt.Sprintf(`{"repository_id":%q,"anchor_ids":[%q],"chunk_ids":[%s]}`, id, id, ids(embedmap.MaxLocateChunks+1)),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, fiber.StatusBadRequest, postLocate(t, app, body, nil))
		})
	}
}
