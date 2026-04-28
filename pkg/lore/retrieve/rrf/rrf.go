// Package rrf implements Reciprocal Rank Fusion (RRF), a score-free
// rank aggregation algorithm that combines multiple ranked lists into one
// ordered result without requiring comparable score scales across rankers.
//
// Reference: Cormack, Clarke, Buettcher (2009), "Reciprocal Rank Fusion
// outperforms Condorcet and individual rank learning methods".
//
// The algorithm:
//
//	score(d) = sum over rankers r: 1 / (k + rank_r(d))
//
// where k=60 is the standard smoothing constant and rank_r(d) is the
// 1-indexed position of document d in ranker r (or absent, contributing 0).
// Documents are returned sorted by descending fused score; ties break by
// ascending ID for determinism.
package rrf

import (
	"sort"
)

// DefaultK is the standard RRF smoothing constant. The value 60 comes from
// the original Cormack et al. paper and is widely used in production systems.
// Callers that want a different constant pass k explicitly to Fuse.
const DefaultK = 60

// ScoredID pairs a document ID with its RRF fused score.
type ScoredID struct {
	ID    int64
	Score float64
}

// Fuse combines multiple ranked lists into a single fused list using RRF.
// Each input list is a slice of document IDs in ranked order (best first,
// index 0 = rank 1). k is the RRF smoothing parameter; pass DefaultK (60)
// when in doubt.
//
// Returns IDs sorted by descending fused score. Ties between equal scores
// are broken by ascending ID so output is deterministic for identical inputs.
//
// Fuse is safe for concurrent use: it does not modify its inputs.
func Fuse(rankings [][]int64, k int) []ScoredID {
	if k <= 0 {
		k = DefaultK
	}

	// Accumulate per-ID scores across all rankers.
	scores := make(map[int64]float64)
	for _, ranking := range rankings {
		for rank, id := range ranking {
			// rank is 0-indexed; RRF uses 1-indexed positions.
			scores[id] += 1.0 / float64(k+rank+1)
		}
	}

	if len(scores) == 0 {
		return nil
	}

	result := make([]ScoredID, 0, len(scores))
	for id, score := range scores {
		result = append(result, ScoredID{ID: id, Score: score})
	}

	// Sort descending by score; break ties ascending by ID for determinism.
	sort.Slice(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return result[i].ID < result[j].ID
	})

	return result
}
