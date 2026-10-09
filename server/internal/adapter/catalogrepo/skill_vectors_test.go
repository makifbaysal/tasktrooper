package catalogrepo

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func vectorSet(texts map[string][]float32) *domain.SkillVectorSet {
	set := domain.NewSkillVectorSet(domain.PinnedLocalEmbeddingModel, domain.EmbeddingSourceONNXInt8, 3)
	for text, vec := range texts {
		set.Put(domain.SkillVectorKey(text), vec)
	}
	return set
}

func TestSkillVectorsRoundTripExactly(t *testing.T) {
	text := domain.SkillEmbeddingText("api-design-conventions", "one error shape", "# API Design Conventions")
	in := vectorSet(map[string][]float32{text: {0.1, -0.25, 3.0000002}, "other": {1, 0, 0}})

	raw, err := EncodeSkillVectors(in)
	require.NoError(t, err)
	out, err := ParseSkillVectors(raw)
	require.NoError(t, err)

	assert.Equal(t, in.Keys(), out.Keys())
	assert.Equal(t, domain.PinnedLocalEmbeddingModel, out.Model)
	assert.Equal(t, domain.EmbeddingSourceONNXInt8, out.Source)
	assert.Equal(t, 3, out.Dims)
	vec, ok := out.Lookup(domain.PinnedLocalEmbeddingModel, domain.EmbeddingSourceONNXInt8, text)
	require.True(t, ok)
	assert.Equal(t, []float32{0.1, -0.25, 3.0000002}, vec)
	again, err := EncodeSkillVectors(out)
	require.NoError(t, err)
	assert.Equal(t, string(raw), string(again), "encoding is deterministic")
	lines := strings.Split(string(raw), "\n")
	for _, key := range in.Keys() {
		assert.True(t, slices.ContainsFunc(lines, func(line string) bool {
			return strings.HasPrefix(strings.TrimSpace(line), `"`+key+`": "`)
		}), "vector %s has a line of its own", key)
	}
}

func TestParseSkillVectorsRefuses(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"not json", "vectors"},
		{"another format", `{"format":2,"model":"m","source":"s","dims":1,"vectors":{}}`},
		{"no source", `{"format":1,"model":"m","dims":1,"vectors":{}}`},
		{"no dims", `{"format":1,"model":"m","source":"s","vectors":{}}`},
		{"short vector", `{"format":1,"model":"m","source":"s","dims":2,"vectors":{"k":"AAAAAA=="}}`},
		{"not base64", `{"format":1,"model":"m","source":"s","dims":1,"vectors":{"k":"%%%"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSkillVectors([]byte(tt.raw))

			assert.Error(t, err)
		})
	}
}

func TestReaderFindsTheVectorsBesideTheCatalogAndRereadsOnlyAChangedFile(t *testing.T) {
	dir := t.TempDir()
	reader := &Reader{Source: dir}

	none, err := reader.SkillVectors(context.Background())
	require.NoError(t, err)
	assert.Nil(t, none, "a catalog without the file ships no vectors")

	write := func(set *domain.SkillVectorSet, at time.Time) {
		raw, err := EncodeSkillVectors(set)
		require.NoError(t, err)
		path := filepath.Join(dir, SkillVectorsFile)
		require.NoError(t, os.WriteFile(path, raw, 0o644))
		require.NoError(t, os.Chtimes(path, at, at))
	}
	first := time.Now().Add(-time.Hour)
	write(vectorSet(map[string][]float32{"a": {1, 0, 0}}), first)

	loaded, err := reader.SkillVectors(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, loaded.Len())
	cached, err := reader.SkillVectors(context.Background())
	require.NoError(t, err)
	assert.Same(t, loaded, cached)

	write(vectorSet(map[string][]float32{"a": {1, 0, 0}, "b": {0, 1, 0}}), first.Add(time.Minute))
	reloaded, err := reader.SkillVectors(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, reloaded.Len())
}

func TestTheGitCheckoutCarriesTheVectorsOfAGitSource(t *testing.T) {
	cache := t.TempDir()
	raw, err := EncodeSkillVectors(vectorSet(map[string][]float32{"a": {1, 0, 0}}))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cache, SkillVectorsFile), raw, 0o644))
	reader := &Reader{Source: "https://github.com/acme/catalog.git", CacheDir: cache}

	loaded, err := reader.SkillVectors(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, loaded.Len())
}
