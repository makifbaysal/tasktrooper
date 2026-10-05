package embedmap

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type LocateQuery struct {
	RepositoryID uuid.UUID
	AnchorIDs    []string
	ChunkIDs     []string
}

type Location struct {
	ChunkID    string  `json:"chunk_id"`
	AnchorID   string  `json:"anchor_id"`
	Similarity float64 `json:"similarity"`
}

type LocateResult struct {
	Locations []Location `json:"locations"`
}

func (s *Service) Locate(ctx context.Context, q LocateQuery) (LocateResult, error) {
	if s == nil || s.store == nil {
		return LocateResult{}, ErrUnavailable
	}
	switch {
	case q.RepositoryID == uuid.Nil:
		return LocateResult{}, ErrLocateRepositoryRequired
	case len(q.AnchorIDs) == 0:
		return LocateResult{}, ErrLocateAnchorsRequired
	case len(q.ChunkIDs) == 0:
		return LocateResult{}, ErrLocateChunksRequired
	case len(q.AnchorIDs) > MaxLimit:
		return LocateResult{}, ErrLocateTooManyAnchors
	case len(q.ChunkIDs) > MaxLocateChunks:
		return LocateResult{}, ErrLocateTooManyChunks
	}

	empty := LocateResult{Locations: []Location{}}
	src, err := s.store.RepositorySource(ctx, q.RepositoryID)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return empty, nil
		}
		return LocateResult{}, fmt.Errorf("resolve repository index: %w", err)
	}

	anchorIDs := parseIDs(q.AnchorIDs)
	chunkIDs := parseIDs(q.ChunkIDs)
	if len(anchorIDs) == 0 || len(chunkIDs) == 0 {
		return empty, nil
	}
	all := make([]uuid.UUID, 0, len(anchorIDs)+len(chunkIDs))
	all = append(all, anchorIDs...)
	all = append(all, chunkIDs...)
	embeddings, err := s.store.ChunkEmbeddings(ctx, src.IndexID, all)
	if err != nil {
		return LocateResult{}, fmt.Errorf("load chunk embeddings: %w", err)
	}

	type anchor struct {
		id  uuid.UUID
		vec []float64
	}
	anchors := make([]anchor, 0, len(anchorIDs))
	seen := make(map[uuid.UUID]struct{}, len(anchorIDs))
	for _, id := range anchorIDs {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		if v := unitVector(embeddings[id]); v != nil {
			anchors = append(anchors, anchor{id: id, vec: v})
		}
	}

	out := LocateResult{Locations: make([]Location, 0, len(chunkIDs))}
	for _, id := range chunkIDs {
		v := unitVector(embeddings[id])
		if v == nil {
			continue
		}
		bestIdx, best := -1, math.Inf(-1)
		for i, a := range anchors {
			if a.id == id {
				bestIdx, best = i, 1
				break
			}
			if len(a.vec) != len(v) {
				continue
			}
			var dot float64
			for k := range v {
				dot += v[k] * a.vec[k]
			}
			if dot > best {
				bestIdx, best = i, dot
			}
		}
		if bestIdx < 0 {
			continue
		}
		out.Locations = append(out.Locations, Location{
			ChunkID:    id.String(),
			AnchorID:   anchors[bestIdx].id.String(),
			Similarity: math.Min(1, best),
		})
	}
	return out, nil
}

func parseIDs(raw []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(raw))
	for _, r := range raw {
		if id, err := uuid.Parse(r); err == nil {
			out = append(out, id)
		}
	}
	return out
}

func unitVector(v []float32) []float64 {
	if len(v) == 0 {
		return nil
	}
	out := make([]float64, len(v))
	var sum float64
	for i, x := range v {
		f := float64(x)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
		out[i] = f
		sum += f * f
	}
	if sum == 0 {
		return nil
	}
	norm := math.Sqrt(sum)
	for i := range out {
		out[i] /= norm
	}
	return out
}
