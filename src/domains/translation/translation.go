package translation

import "time"

// Variant identifiers for the three suggestions returned per request.
// Keep these stable — they're persisted in the cache and consumed by the UI.
const (
	VariantLiteral     = "literal"
	VariantNatural     = "natural"
	VariantToneMatched = "tone_matched"
)

// Suggestion is one of three candidates returned by the provider.
type Suggestion struct {
	Variant    string  `json:"variant"`
	Text       string  `json:"text"`
	Rationale  string  `json:"rationale,omitempty"`
	Confidence float32 `json:"confidence,omitempty"`
}

// TranslateMessageRequest translates an existing stored message by ID.
// The source text and conversation context are pulled from chatstorage.
type TranslateMessageRequest struct {
	MessageID  string `json:"message_id" uri:"message_id"`
	ChatJID    string `json:"chat_jid" form:"chat_jid"`
	TargetLang string `json:"target_lang" form:"target_lang"`
	SourceLang string `json:"source_lang" form:"source_lang"`
}

// TranslateDraftRequest translates arbitrary user-supplied text (compose-assist).
// ChatJID is optional — when provided, recent messages from that chat are used
// as context so the suggestions match the conversation's tone.
type TranslateDraftRequest struct {
	Text       string `json:"text" form:"text"`
	ChatJID    string `json:"chat_jid" form:"chat_jid"`
	TargetLang string `json:"target_lang" form:"target_lang"`
	SourceLang string `json:"source_lang" form:"source_lang"`
}

// TranslateResponse is the unified response for both translate endpoints.
type TranslateResponse struct {
	MessageID   string       `json:"message_id,omitempty"`
	SourceText  string       `json:"source_text"`
	SourceLang  string       `json:"source_lang"`
	TargetLang  string       `json:"target_lang"`
	Provider    string       `json:"provider"`
	CacheHit    bool         `json:"cache_hit"`
	Suggestions []Suggestion `json:"suggestions"`
}

// CachedTranslation is the persisted form of a 3-suggestion result.
type CachedTranslation struct {
	MessageID     string
	ChatJID       string
	DeviceID      string
	TargetLang    string
	SourceLang    string
	Provider      string
	PromptVersion string
	Suggestions   []Suggestion
	CreatedAt     time.Time
}

// ChatPref is the per-chat translation preference (Phase 4 wiring; schema is
// in place now so the migration is once-and-done).
type ChatPref struct {
	DeviceID              string
	ChatJID               string
	TargetLang            string
	AutoTranslateInbound  bool
	AutoTranslateOutbound bool
}

// MessageEmbedding is reserved for Phase 3 (RAG). The vector is stored as a
// JSON-encoded float slice for cross-DB portability.
type MessageEmbedding struct {
	MessageID string
	ChatJID   string
	DeviceID  string
	Model     string
	Dim       int
	Vector    []float32
	CreatedAt time.Time
}

// EmbeddedMessage is a hydrated row that joins message_embeddings with the
// underlying message text. Used by retrieval helpers so the usecase can score
// against the vector and still have the original content for the prompt.
type EmbeddedMessage struct {
	MessageID string
	ChatJID   string
	Sender    string
	Content   string
	IsFromMe  bool
	Vector    []float32
	Timestamp time.Time
}

// MessageEmbeddingTarget identifies a message that needs to be embedded.
// Returned by the repository to drive the lazy backfill worker.
type MessageEmbeddingTarget struct {
	MessageID string
	ChatJID   string
	Content   string
}
