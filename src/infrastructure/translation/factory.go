package translation

import (
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
	"github.com/sirupsen/logrus"
)

// BuildProvider selects a translation provider based on the global config.
//
// It belongs in infrastructure/translation rather than cmd/ because the
// decision is pure boot-time wiring of concrete adapters around the same
// domain interface. cmd/ stays small and only needs to call this helper.
//
// Falls back to the mock provider so the feature degrades gracefully when
// no API key is configured — failing closed at boot would surprise users
// who flip TRANSLATION_ENABLED on without remembering the key.
func BuildProvider() domainTranslation.Provider {
	switch strings.ToLower(strings.TrimSpace(config.TranslationProvider)) {
	case "openai":
		if strings.TrimSpace(config.TranslationOpenAIAPIKey) == "" {
			logrus.Warn("translation: provider 'openai' selected but TRANSLATION_OPENAI_API_KEY is empty; falling back to mock provider")
			return NewMockProvider()
		}
		return NewOpenAIProvider(OpenAIConfig{
			BaseURL: config.TranslationOpenAIBaseURL,
			APIKey:  config.TranslationOpenAIAPIKey,
			Model:   config.TranslationOpenAIModel,
			Timeout: time.Duration(config.TranslationRequestTimeoutSec) * time.Second,
		})
	case "", "mock":
		return NewMockProvider()
	default:
		logrus.Warnf("translation: unknown provider %q; falling back to mock provider", config.TranslationProvider)
		return NewMockProvider()
	}
}

// BuildEmbedder returns the embedding provider used for Phase 3 (RAG) or
// nil when RAG is disabled / no key is available. The usecase treats nil
// as "RAG unavailable" and falls through to the system-context path so
// flipping TRANSLATION_RAG_ENABLED on without a key is a no-op rather
// than a hard failure.
func BuildEmbedder() domainTranslation.EmbeddingProvider {
	if !config.TranslationRAGEnabled {
		return nil
	}
	apiKey := strings.TrimSpace(config.TranslationOpenAIEmbeddingAPIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(config.TranslationOpenAIAPIKey)
	}
	if apiKey == "" {
		logrus.Warn("translation/rag: TRANSLATION_RAG_ENABLED=true but no embedding API key configured; RAG disabled at runtime")
		return nil
	}
	return NewOpenAIEmbeddingProvider(OpenAIEmbeddingConfig{
		BaseURL: config.TranslationOpenAIBaseURL,
		APIKey:  apiKey,
		Model:   config.TranslationOpenAIEmbeddingModel,
		Timeout: time.Duration(config.TranslationRequestTimeoutSec) * time.Second,
	})
}
