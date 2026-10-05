package embedmap_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/embedmap"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type locateStore struct {
	stubStore
	embeddings map[uuid.UUID][]float32
	calls      int
	gotIndex   uuid.UUID
}

func (l *locateStore) ChunkEmbeddings(_ context.Context, indexID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]float32, error) {
	l.calls++
	l.gotIndex = indexID
	out := map[uuid.UUID][]float32{}
	for _, id := range ids {
		if v, ok := l.embeddings[id]; ok {
			out[id] = v
		}
	}
	return out, nil
}

func newLocateFixture() (*embedmap.Service, *locateStore, uuid.UUID, map[string]uuid.UUID) {
	repoID, indexID := uuid.New(), uuid.New()
	ids := map[string]uuid.UUID{}
	emb := map[uuid.UUID][]float32{}
	add := func(name string, v ...float32) {
		id := uuid.New()
		ids[name] = id
		emb[id] = v
	}
	add("a-x", 1, 0, 0)
	add("a-y", 0, 1, 0)
	add("near-x", 5, 1, 0)
	add("near-y", 0.2, 3, 0)
	add("zero", 0, 0, 0)
	add("short", 1, 0)
	st := &locateStore{
		stubStore:  stubStore{repos: []port.EmbeddingRepositorySource{{RepositoryID: repoID, IndexID: indexID}}},
		embeddings: emb,
	}
	return embedmap.New(st), st, repoID, ids
}

func strs(ids map[string]uuid.UUID, names ...string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = ids[n].String()
	}
	return out
}

func TestLocatePicksNearestAnchorInRequestOrder(t *testing.T) {
	svc, st, repoID, ids := newLocateFixture()
	res, err := svc.Locate(context.Background(), embedmap.LocateQuery{
		RepositoryID: repoID,
		AnchorIDs:    strs(ids, "a-x", "a-y"),
		ChunkIDs:     strs(ids, "near-y", "a-x", "near-x"),
	})
	require.NoError(t, err)
	require.Equal(t, 1, st.calls)
	require.Equal(t, st.repos[0].IndexID, st.gotIndex)
	require.Len(t, res.Locations, 3)
	require.Equal(t, ids["near-y"].String(), res.Locations[0].ChunkID)
	require.Equal(t, ids["a-y"].String(), res.Locations[0].AnchorID)
	require.Equal(t, ids["a-x"].String(), res.Locations[1].ChunkID)
	require.Equal(t, ids["a-x"].String(), res.Locations[1].AnchorID)
	require.InDelta(t, 1, res.Locations[1].Similarity, 1e-9)
	require.Equal(t, ids["a-x"].String(), res.Locations[2].AnchorID)
	require.Greater(t, res.Locations[2].Similarity, 0.9)
	require.Less(t, res.Locations[2].Similarity, 1.0)
}

func TestLocateTieGoesToFirstAnchor(t *testing.T) {
	svc, st, repoID, _ := newLocateFixture()
	first, second, probe := uuid.New(), uuid.New(), uuid.New()
	st.embeddings[first] = []float32{1, 0, 0}
	st.embeddings[second] = []float32{2, 0, 0}
	st.embeddings[probe] = []float32{3, 0, 0}
	res, err := svc.Locate(context.Background(), embedmap.LocateQuery{
		RepositoryID: repoID,
		AnchorIDs:    []string{first.String(), second.String()},
		ChunkIDs:     []string{probe.String()},
	})
	require.NoError(t, err)
	require.Len(t, res.Locations, 1)
	require.Equal(t, first.String(), res.Locations[0].AnchorID)
}

func TestLocateOmitsUnknownAndUnusableChunks(t *testing.T) {
	svc, _, repoID, ids := newLocateFixture()
	res, err := svc.Locate(context.Background(), embedmap.LocateQuery{
		RepositoryID: repoID,
		AnchorIDs:    append(strs(ids, "a-x"), "not-a-uuid"),
		ChunkIDs:     append(strs(ids, "zero", "short", "near-x"), uuid.NewString(), "also-bad"),
	})
	require.NoError(t, err)
	require.Len(t, res.Locations, 1)
	require.Equal(t, ids["near-x"].String(), res.Locations[0].ChunkID)
}

func TestLocateNoIndexIsEmptyNotNull(t *testing.T) {
	svc, _, _, ids := newLocateFixture()
	res, err := svc.Locate(context.Background(), embedmap.LocateQuery{
		RepositoryID: uuid.New(),
		AnchorIDs:    strs(ids, "a-x"),
		ChunkIDs:     strs(ids, "a-x"),
	})
	require.NoError(t, err)
	require.NotNil(t, res.Locations)
	require.Empty(t, res.Locations)
}

func TestLocateValidation(t *testing.T) {
	svc, _, repoID, ids := newLocateFixture()
	one := strs(ids, "a-x")
	many := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = uuid.NewString()
		}
		return out
	}
	cases := []struct {
		name string
		q    embedmap.LocateQuery
		want error
	}{
		{"no repository", embedmap.LocateQuery{AnchorIDs: one, ChunkIDs: one}, embedmap.ErrLocateRepositoryRequired},
		{"no anchors", embedmap.LocateQuery{RepositoryID: repoID, ChunkIDs: one}, embedmap.ErrLocateAnchorsRequired},
		{"no chunks", embedmap.LocateQuery{RepositoryID: repoID, AnchorIDs: one}, embedmap.ErrLocateChunksRequired},
		{"too many anchors", embedmap.LocateQuery{RepositoryID: repoID, AnchorIDs: many(embedmap.MaxLimit + 1), ChunkIDs: one}, embedmap.ErrLocateTooManyAnchors},
		{"too many chunks", embedmap.LocateQuery{RepositoryID: repoID, AnchorIDs: one, ChunkIDs: many(embedmap.MaxLocateChunks + 1)}, embedmap.ErrLocateTooManyChunks},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Locate(context.Background(), tc.q)
			require.ErrorIs(t, err, tc.want)
		})
	}
}
