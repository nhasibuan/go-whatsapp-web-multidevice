package usecase

import (
	"math"
	"testing"

	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
	"github.com/stretchr/testify/assert"
)

func TestCosineSimilarity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		a, b []float32
		want float32
	}{
		{
			name: "identical unit vectors -> 1",
			a:    []float32{1, 0, 0},
			b:    []float32{1, 0, 0},
			want: 1,
		},
		{
			name: "orthogonal vectors -> 0",
			a:    []float32{1, 0},
			b:    []float32{0, 1},
			want: 0,
		},
		{
			name: "opposite vectors -> -1",
			a:    []float32{1, 0},
			b:    []float32{-1, 0},
			want: -1,
		},
		{
			name: "empty inputs -> 0 (defensive)",
			a:    []float32{},
			b:    []float32{1},
			want: 0,
		},
		{
			name: "zero vector -> 0 (avoid NaN)",
			a:    []float32{0, 0},
			b:    []float32{1, 1},
			want: 0,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := cosineSimilarity(tc.a, tc.b)
			// Allow a small epsilon for float math.
			assert.InDelta(t, float64(tc.want), float64(got), 1e-6)
			assert.False(t, math.IsNaN(float64(got)), "result must not be NaN")
		})
	}
}

func TestTopKByCosine_RanksAndDedupes(t *testing.T) {
	t.Parallel()

	query := []float32{1, 0}
	candidates := []*domainTranslation.EmbeddingCandidate{
		{MessageID: "m1", Content: "perfect match", Vector: []float32{1, 0}},
		{MessageID: "m2", Content: "orthogonal", Vector: []float32{0, 1}},
		{MessageID: "m3", Content: "near match", Vector: []float32{0.9, 0.1}},
		// duplicate of m1, should be dropped
		{MessageID: "m1", Content: "dup", Vector: []float32{1, 0}},
		// empty content should be dropped
		{MessageID: "m4", Content: "  ", Vector: []float32{1, 0}},
		// excluded message should be dropped
		{MessageID: "exclude-me", Content: "skip", Vector: []float32{1, 0}},
	}

	got := topKByCosine(query, candidates, 2, "exclude-me")

	if assert.Len(t, got, 2) {
		assert.Equal(t, "m1", got[0].MessageID, "best match first")
		assert.Equal(t, "m3", got[1].MessageID, "second best second")
	}
}

func TestCandidatesToContext_PreservesMetadata(t *testing.T) {
	t.Parallel()

	in := []*domainTranslation.EmbeddingCandidate{
		{MessageID: "m1", Sender: "alice", IsFromMe: false, Content: "hi", Timestamp: "2026-01-01T00:00:00Z"},
		{MessageID: "m2", Sender: "me", IsFromMe: true, Content: "yo", Timestamp: "2026-01-02T00:00:00Z"},
	}
	got := candidatesToContext(in)

	assert.Len(t, got, 2)
	assert.Equal(t, "alice", got[0].Sender)
	assert.False(t, got[0].IsFromMe)
	assert.True(t, got[1].IsFromMe)
	assert.Equal(t, "yo", got[1].Content)
}
