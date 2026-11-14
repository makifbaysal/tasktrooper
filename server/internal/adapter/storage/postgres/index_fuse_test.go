package postgres

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestFuseRRFAndCapChunksTakeAtMostTopK(t *testing.T) {
	a := []domain.WorkspaceChunk{{ID: uuid.New()}, {ID: uuid.New()}}
	b := []domain.WorkspaceChunk{{ID: uuid.New()}}

	assert.Len(t, fuseRRF(a, b, 2), 2)
	assert.Len(t, fuseRRF(a, b, 1_000_000), 3, "a topK larger than the merged list returns what was merged")
	assert.Empty(t, fuseRRF(a, b, -1), "a negative topK must not panic")
	assert.Empty(t, capChunks(a, -1), "a negative topK must not panic")
	assert.Len(t, capChunks(a, 1), 1)
}
