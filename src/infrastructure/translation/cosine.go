package translation

import (
	"math"
	"sort"

	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
)

// cosineSimilarity returns the cosine similarity in [-1, 1] for two equal-length
// float32 vectors. Returns 0 for empty or zero-magnitude inputs so downstream
// code can rank without special-casing.
//
// Implementation notes:
//   - We accumulate in float64 to avoid loss for higher-dimensional vectors
//     (1536-D is well within float32 precision but the dot product can grow).
//   - The hot path is two multiplies and two adds per element. For 1500-D
//     vectors that's ~6000 ops per pair; benchmarks show ~5µs/pair on a laptop
//     CPU, so a few thousand candidates remain comfortably interactive.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		fa, fb := float64(a[i]), float64(b[i])
		dot += fa * fb
		normA += fa * fa
		normB += fb * fb
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}

// scoredEmbedded pairs an embedded message with its similarity score for sorting.
type scoredEmbedded struct {
	score float32
	msg   *domainTranslation.EmbeddedMessage
}

// topK returns the top-k entries from candidates by cosine similarity to query.
// Stable order: ties broken by recency (newer first) so the prompt favors
// recent exemplars when scores are close.
//
// k <= 0 or empty inputs return nil. Candidates whose stored vector is the
// wrong length (e.g. embedded with a different model) are skipped silently.
func topK(query []float32, candidates []*domainTranslation.EmbeddedMessage, k int) []*domainTranslation.EmbeddedMessage {
	if k <= 0 || len(query) == 0 || len(candidates) == 0 {
		return nil
	}

	scored := make([]scoredEmbedded, 0, len(candidates))
	for _, c := range candidates {
		if c == nil || len(c.Vector) != len(query) {
			continue
		}
		scored = append(scored, scoredEmbedded{
			score: cosineSimilarity(query, c.Vector),
			msg:   c,
		})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].msg.Timestamp.After(scored[j].msg.Timestamp)
	})

	if len(scored) > k {
		scored = scored[:k]
	}

	out := make([]*domainTranslation.EmbeddedMessage, 0, len(scored))
	for _, s := range scored {
		out = append(out, s.msg)
	}
	return out
}

// TopK is the exported entry point used by the usecase package. Kept as a thin
// wrapper around the unexported topK so test/bench code in this package can
// continue to call the lowercase form directly.
func TopK(query []float32, candidates []*domainTranslation.EmbeddedMessage, k int) []*domainTranslation.EmbeddedMessage {
	return topK(query, candidates, k)
}
