package translation

import "context"

// ITranslationUsecase is the application-level entry point for translation.
type ITranslationUsecase interface {
	// TranslateMessage translates a stored message by ID, returning 3 candidates.
	TranslateMessage(ctx context.Context, request TranslateMessageRequest) (TranslateResponse, error)

	// TranslateDraft translates arbitrary text (used by compose-assist).
	TranslateDraft(ctx context.Context, request TranslateDraftRequest) (TranslateResponse, error)

	// GetChatPref returns the per-chat preference, with the effective target
	// language applied. Missing rows return device-level defaults rather
	// than an error so the UI can render a "not yet configured" panel.
	GetChatPref(ctx context.Context, request GetChatPrefRequest) (ChatPrefResponse, error)

	// SetChatPref upserts the per-chat preference. Nil pointer fields in the
	// request are left unchanged.
	SetChatPref(ctx context.Context, request SetChatPrefRequest) (ChatPrefResponse, error)
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

	// ListEmbeddingsByChat returns recent embedded messages for a single chat,
	// limited to `limit` entries newest-first by created_at. Used for per-chat
	// retrieval in the RAG pipeline.
	ListEmbeddingsByChat(deviceID, chatJID, model string, limit int) ([]*EmbeddedMessage, error)

	// ListUserStyleEmbeddings returns embedded messages the user wrote
	// themselves (is_from_me = true), across all chats, limited to `limit`
	// entries newest-first. Provides the "user style" pool that powers the
	// tone_matched suggestion variant.
	ListUserStyleEmbeddings(deviceID, model string, limit int) ([]*EmbeddedMessage, error)

	// ListMessageIDsMissingEmbedding returns message IDs from a chat that have
	// text content but no embedding row for the given model. Used by the lazy
	// backfill worker to know what to embed next.
	ListMessageIDsMissingEmbedding(deviceID, chatJID, model string, limit int) ([]MessageEmbeddingTarget, error)
}

// Provider abstracts the external translation backend (OpenAI, etc.).
// Implementations live in infrastructure/translation/*.
type Provider interface {
	Name() string
	GenerateSuggestions(ctx context.Context, in ProviderRequest) ([]Suggestion, error)
}

// EmbeddingProvider abstracts an embeddings backend. A single batched call
// returns one vector per input string. Implementations live in
// infrastructure/translation/*.
type EmbeddingProvider interface {
	// Model identifies the embedding model in use (e.g. "text-embedding-3-small").
	// Persisted with each row so vectors stay isolated by model.
	Model() string

	// Embed turns a slice of texts into vectors of identical length. The
	// returned slice has the same length and order as the input.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
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
	// StyleExamples are short exemplars of the user's own writing in the
	// target language, used to bias the tone_matched variant. Optional.
	StyleExamples []string
}
