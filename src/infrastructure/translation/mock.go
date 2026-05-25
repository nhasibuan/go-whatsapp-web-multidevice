package translation

import (
	"context"
	"fmt"

	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
)

// mockProvider returns deterministic, dependency-free suggestions. It is the
// default when no provider is configured so the feature degrades gracefully
// (no API key required) and unit tests don't hit the network.
//
// The three suggestions are derived from the source text — they are NOT
// real translations. The provider is intended for development, smoke tests,
// and the open-source default experience.
type mockProvider struct{}

// NewMockProvider creates a mock translation provider.
func NewMockProvider() domainTranslation.Provider {
	return &mockProvider{}
}

func (p *mockProvider) Name() string { return "mock" }

func (p *mockProvider) Translate(_ context.Context, in domainTranslation.ProviderRequest) ([]domainTranslation.Suggestion, error) {
	return NormalizeSuggestions([]domainTranslation.Suggestion{
		{
			Variant:    domainTranslation.VariantLiteral,
			Text:       fmt.Sprintf("[%s|literal] %s", in.TargetLang, in.SourceText),
			Rationale:  "Word-for-word rendering (mock provider).",
			Confidence: 0.6,
		},
		{
			Variant:    domainTranslation.VariantNatural,
			Text:       fmt.Sprintf("[%s|natural] %s", in.TargetLang, in.SourceText),
			Rationale:  "Idiomatic phrasing (mock provider).",
			Confidence: 0.7,
		},
		{
			Variant:    domainTranslation.VariantToneMatched,
			Text:       fmt.Sprintf("[%s|tone] %s", in.TargetLang, in.SourceText),
			Rationale:  fmt.Sprintf("Matches recent thread tone (%d context messages).", len(in.Context)),
			Confidence: 0.55,
		},
	}), nil
}
