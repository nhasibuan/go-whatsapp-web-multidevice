package translation

import "context"

// PromptVersion is bumped whenever the system prompt or input contract for
// translation changes in a way that would invalidate cached results. The
// usecase folds it into the cache provider key so old entries don't leak
// into a new prompt regime — v1 entries simply don't match and expire on
// TTL without a schema change.
const PromptVersion = "v2"

// ITranslationUsecase defines the contract for in-chat translation.
//
// Both methods must honor device scoping via the request context (the same
// pattern used by chat/send/message usecases).
type ITranslationUsecase interface {
	// TranslateMessage translates an already-stored message identified by
	// (chat_jid, message_id) into TargetLang and returns three context-aware
	// suggestions (literal / natural / tone_matched).
	TranslateMessage(ctx context.Context, request TranslateMessageRequest) (TranslateMessageResponse, error)

	// TranslateDraft translates arbitrary text the user is composing. When
	// ChatJID is provided, the recent thread is used as context.
	TranslateDraft(ctx context.Context, request TranslateDraftRequest) (TranslateDraftResponse, error)

	// GetChatPrefs returns the persisted per-chat preferences plus the
	// effective target language (preference → global default). Missing rows
	// resolve to defaults so the UI can render a clean panel without 404 handling.
	GetChatPrefs(ctx context.Context, request GetChatPrefsRequest) (GetChatPrefsResponse, error)

	// SetChatPrefs partially updates the per-chat preferences. Only the
	// pointer fields supplied on the request are written; other fields stay
	// untouched. Returns the resulting persisted state.
	SetChatPrefs(ctx context.Context, request SetChatPrefsRequest) (GetChatPrefsResponse, error)
}

// ContextMessage represents a single recent message used as conversational
// context when generating translation candidates. Providers consume these
// to keep tone, names, and references consistent with the thread.
type ContextMessage struct {
	Sender    string
	Content   string
	IsFromMe  bool
	Timestamp string
}

// Provider is the pluggable translation backend. Implementations live in
// infrastructure/translation/ and are selected via TRANSLATION_PROVIDER.
type Provider interface {
	// Name returns a stable identifier (e.g. "openai", "mock") used for
	// logging and caching keys.
	Name() string

	// Translate returns exactly three suggestions, one per variant.
	// Implementations must be safe for concurrent use.
	Translate(ctx context.Context, in ProviderRequest) ([]Suggestion, error)
}

// ProviderRequest is the normalized payload providers receive. The usecase
// builds it from chatstorage data so providers don't need to know anything
// about the WhatsApp domain.
//
// Context and StyleExamples are both optional; the provider should render
// each in its own section so the model can tell "what people said in this
// chat" apart from "how this user writes". Phase 3 (RAG) populates
// StyleExamples; Phase 1 leaves it empty and only fills Context.
type ProviderRequest struct {
	SourceText    string
	SourceLang    string
	TargetLang    string
	Context       []ContextMessage
	StyleExamples []ContextMessage
}

// EmbeddingProvider produces dense vector representations for short texts.
// It is a separate interface from Provider so the embedding model can evolve
// independently of the chat-completion model — the OpenAI implementation
// uses text-embedding-3-small while the chat model is gpt-4o-mini.
type EmbeddingProvider interface {
	// Name returns a stable identifier (e.g. "openai-embeddings").
	Name() string
	// Model returns the underlying model identifier; this is stored alongside
	// each vector so vectors produced by different models stay separated.
	Model() string
	// Embed batches arbitrary input strings into vectors. Implementations
	// should chunk internally to honor provider batch limits.
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
}

// ITranslationRepository persists translation cache, per-chat prefs, and the
// embeddings table that backs Phase 3 (RAG). Implementations live in
// infrastructure/translation/ alongside the providers.
type ITranslationRepository interface {
	// GetCachedTranslation returns a cache hit by exact (device, chat, message?, target, source_hash, provider)
	// match. Expired entries are treated as misses. Returns (nil, nil) on miss.
	GetCachedTranslation(deviceID, chatJID, messageID, targetLang, sourceHash, provider string) (*CachedTranslation, error)

	// PutCachedTranslation upserts a cache row. Caller is responsible for
	// computing SourceHash (sha256 hex of the source text).
	PutCachedTranslation(entry *CachedTranslation) error

	// PurgeExpiredTranslations deletes rows whose ExpiresAt is in the past.
	// Returns the number of rows deleted.
	PurgeExpiredTranslations() (int64, error)

	// GetChatPref / SetChatPref persist per-chat target language and toggles.
	GetChatPref(deviceID, chatJID string) (*ChatTranslationPref, error)
	SetChatPref(pref *ChatTranslationPref) error

	// StoreEmbedding / GetEmbedding back the Phase 3 RAG pipeline.
	StoreEmbedding(emb *MessageEmbedding) error
	GetEmbedding(deviceID, chatJID, messageID string) (*MessageEmbedding, error)

	// ListChatEmbeddingCandidates returns the most recent embeddings in the
	// chat alongside the message text, sender, and is_from_me flag — enough
	// to score similarity in Go and render the result as a ContextMessage.
	// The pool size caps how many candidates are loaded; the usecase scores
	// in-memory and returns top-K.
	ListChatEmbeddingCandidates(deviceID, chatJID, model string, limit int) ([]*EmbeddingCandidate, error)

	// ListUserStyleEmbeddingCandidates returns the most recent outbound
	// (is_from_me = true) embeddings across every chat for the device. Used
	// to bias the tone_matched variant toward the user's actual writing.
	ListUserStyleEmbeddingCandidates(deviceID, model string, limit int) ([]*EmbeddingCandidate, error)

	// CountEmbeddings reports how many embeddings exist for a chat under a
	// given model. The usecase uses this to decide whether a lazy backfill
	// is worth kicking off before serving a request.
	CountEmbeddings(deviceID, chatJID, model string) (int64, error)

	// ListMessagesNeedingEmbedding returns text-bearing messages from the
	// chat that don't yet have an embedding under the given model. The
	// background backfiller walks this list and pushes vectors back via
	// StoreEmbedding. Limit caps a single burst.
	ListMessagesNeedingEmbedding(deviceID, chatJID, model string, limit int) ([]*EmbeddingBackfillItem, error)

	// InitializeSchema is a no-op for backends that share the chatstorage DB
	// (migrations are appended into chatstorage's getMigrations). Kept on the
	// interface so alternative implementations can self-bootstrap.
	InitializeSchema() error
}

// EmbeddingCandidate is the joined view that retrieval needs: the vector
// itself plus enough message metadata to render it back as a ContextMessage.
type EmbeddingCandidate struct {
	MessageID string
	ChatJID   string
	Sender    string
	IsFromMe  bool
	Content   string
	Vector    []float32
	Timestamp string
}

// EmbeddingBackfillItem is a (message_id, content) pair the backfiller
// should embed. Kept narrow so the SQL query stays cheap.
type EmbeddingBackfillItem struct {
	MessageID string
	ChatJID   string
	Content   string
}
