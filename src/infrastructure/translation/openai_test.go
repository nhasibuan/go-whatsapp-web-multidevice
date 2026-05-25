package translation

import (
	"testing"

	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeSuggestions_EnforcesThreeCardContract(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    []domainTranslation.Suggestion
		wantLen  int
		wantVars []string // variants in the expected order
	}{
		{
			name: "all three variants returned in canonical order",
			input: []domainTranslation.Suggestion{
				{Variant: "tone_matched", Text: "casual"},
				{Variant: "literal", Text: "verbatim"},
				{Variant: "natural", Text: "fluent"},
			},
			wantLen:  3,
			wantVars: []string{"literal", "natural", "tone_matched"},
		},
		{
			name: "missing variants fall back to closest match",
			input: []domainTranslation.Suggestion{
				{Variant: "natural", Text: "fluent"},
			},
			wantLen:  3,
			wantVars: []string{"literal", "natural", "tone_matched"},
		},
		{
			name: "duplicate variants keep only the first",
			input: []domainTranslation.Suggestion{
				{Variant: "literal", Text: "first"},
				{Variant: "literal", Text: "second"},
				{Variant: "natural", Text: "fluent"},
				{Variant: "tone_matched", Text: "casual"},
			},
			wantLen:  3,
			wantVars: []string{"literal", "natural", "tone_matched"},
		},
		{
			name:    "empty input returns empty",
			input:   nil,
			wantLen: 0,
		},
		{
			name: "variant casing is normalized",
			input: []domainTranslation.Suggestion{
				{Variant: "LITERAL", Text: "verbatim"},
				{Variant: " Natural ", Text: "fluent"},
				{Variant: "Tone_Matched", Text: "casual"},
			},
			wantLen:  3,
			wantVars: []string{"literal", "natural", "tone_matched"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := NormalizeSuggestions(tc.input)
			if !assert.Len(t, got, tc.wantLen) {
				return
			}
			for i, want := range tc.wantVars {
				assert.Equal(t, want, got[i].Variant, "variant order at %d", i)
				assert.NotEmpty(t, got[i].Text, "text must be populated for %s", got[i].Variant)
			}
		})
	}
}

func TestNormalizeSuggestions_FillerHasFallbackRationale(t *testing.T) {
	t.Parallel()

	got := NormalizeSuggestions([]domainTranslation.Suggestion{
		{Variant: "natural", Text: "fluent", Rationale: "real rationale"},
	})

	if assert.Len(t, got, 3) {
		// Real entry keeps its rationale.
		assert.Equal(t, "real rationale", got[1].Rationale)
		// Filler entries are tagged so callers know they were synthesized.
		assert.Contains(t, got[0].Rationale, "Provider did not return")
		assert.Contains(t, got[2].Rationale, "Provider did not return")
	}
}

func TestFloatsToBytesRoundTrip(t *testing.T) {
	t.Parallel()

	in := []float32{1, -1, 3.14, 2.71, 0}
	bytes := FloatsToBytes(in)
	out := bytesToFloats(bytes)

	assert.Equal(t, len(in), len(out))
	for i := range in {
		assert.InDelta(t, float64(in[i]), float64(out[i]), 1e-7)
	}
}
