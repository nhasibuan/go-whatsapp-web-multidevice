package translation

import "context"

// ITranslationUsecase is the application-level entry point for translation.
type ITranslationUsecase interface {
	// TranslateMessage translates a stored message by ID, returning 3 candidates.
	TranslateMessage(ctx context.Context, request TranslateMessageRequest) (TranslateResponse, error)

	// TranslateDraft translates arbitrary text (used by compose-assist).
	TranslateDraft(ctx context.Context, request TranslateDraftRequest) (TranslateResponse, error)
}

// ITranslationRepository handles persistence of translation cache, per-chat
// preferences, and (Phase 3) message embeddings. Kept separate from
// IChatStorageRepository to avoid bloating the core chat repository contract.
type ITranslationRepository interface {
	// Cache operations
	GetCachedTranslation(deviceID, messageID, chatJID, targetLang, promptVersion string) (*CachedTranslation, error)
	SaveCachedTranslation(entry *CachedTranslation) error

	// Per-chat preferences
	GetChatPref(deviceID, chatJID string) (*ChatPref, error)
	UpsertChatPref(pref *ChatPref) error

	// Embeddings (Phase 3)
	SaveEmbedding(emb *MessageEmbedding) error
	GetEmbedding(deviceID, messageID, chatJID, model string) (*MessageEmbedding, error)
}

// Provider abstracts the external translation backend (OpenAI, etc.).
// Implementations live in infrastructure/translation/*.
type Provider interface {
	Name() string
	GenerateSuggestions(ctx context.Context, in ProviderRequest) ([]Suggestion, error)
}

// ContextMessage is one prior message used to condition the translation. The
// usecase builds a slice of these from chatstorage before calling the provider.
type ContextMessage struct {
	Sender   string
	Content  string
	IsFromMe bool
	Lang     string // optional, may be empty
}

// ProviderRequest is the structured input handed to a Provider implementation.
type ProviderRequest struct {
	SourceText string
	SourceLang string // optional, "" means autodetect
	TargetLang string
	Context    []ContextMessage
}
