package translation

// Variant labels for the three suggestions returned by the translator.
// They are stable identifiers exposed in the JSON API and must remain
// constant — the UI keys cards on these values.
const (
	VariantLiteral     = "literal"
	VariantNatural     = "natural"
	VariantToneMatched = "tone_matched"
)

// Suggestion is a single translation candidate. Three are returned per request,
// one for each variant in [VariantLiteral, VariantNatural, VariantToneMatched].
type Suggestion struct {
	Variant    string  `json:"variant"`
	Text       string  `json:"text"`
	Rationale  string  `json:"rationale,omitempty"`
	Confidence float32 `json:"confidence,omitempty"`
}

// TranslateMessageRequest translates an existing stored message. The source
// text and surrounding context are pulled from chatstorage by (device_id, chat_jid, message_id).
type TranslateMessageRequest struct {
	ChatJID    string `json:"chat_jid"`
	MessageID  string `json:"message_id"`
	TargetLang string `json:"target_lang"`
	SourceLang string `json:"source_lang,omitempty"`
	// ForceRefresh skips the cache and asks the provider for a fresh result.
	ForceRefresh bool `json:"force_refresh,omitempty"`
}

// TranslateMessageResponse returns three context-aware suggestions plus echoed metadata.
type TranslateMessageResponse struct {
	MessageID   string       `json:"message_id"`
	ChatJID     string       `json:"chat_jid"`
	SourceText  string       `json:"source_text"`
	SourceLang  string       `json:"source_lang"`
	TargetLang  string       `json:"target_lang"`
	Suggestions []Suggestion `json:"suggestions"`
	Provider    string       `json:"provider"`
	Cached      bool         `json:"cached"`
}

// TranslateDraftRequest translates arbitrary user-supplied text — typically a
// reply being composed. ChatJID is optional and, when provided, lets the
// translator condition the result on recent thread context.
type TranslateDraftRequest struct {
	ChatJID    string `json:"chat_jid,omitempty"`
	Text       string `json:"text"`
	TargetLang string `json:"target_lang"`
	SourceLang string `json:"source_lang,omitempty"`
	// ForceRefresh skips the cache and asks the provider for a fresh result.
	ForceRefresh bool `json:"force_refresh,omitempty"`
}

// TranslateDraftResponse mirrors TranslateMessageResponse for drafts.
type TranslateDraftResponse struct {
	ChatJID     string       `json:"chat_jid,omitempty"`
	SourceText  string       `json:"source_text"`
	SourceLang  string       `json:"source_lang"`
	TargetLang  string       `json:"target_lang"`
	Suggestions []Suggestion `json:"suggestions"`
	Provider    string       `json:"provider"`
	Cached      bool         `json:"cached"`
}


// CachedTranslation is a persisted translation result. The repository uses
// it for both reads (cache hits) and writes (cache fills).
type CachedTranslation struct {
	DeviceID    string
	ChatJID     string
	MessageID   string // empty for draft entries
	TargetLang  string
	SourceLang  string
	SourceHash  string // sha256 of source text — distinguishes edited messages and drafts
	Provider    string
	Suggestions []Suggestion
	CreatedAt   int64 // unix seconds
	ExpiresAt   int64 // unix seconds; 0 means no expiry
}

// ChatTranslationPref is the per-chat user preference. It lets each
// conversation remember its own target language without forcing a global default.
type ChatTranslationPref struct {
	DeviceID         string
	ChatJID          string
	TargetLang       string
	AutoTranslate    bool
	TranslationOptIn bool
	UpdatedAt        int64
}

// MessageEmbedding is a staged record for Phase 3 (RAG). The schema is
// migrated up front so backfill jobs can populate it without further DDL.
// Vectors are stored as raw bytes (float32 little-endian) to keep the table
// portable across SQLite/Postgres without requiring sqlite-vec.
type MessageEmbedding struct {
	DeviceID  string
	ChatJID   string
	MessageID string
	Model     string
	Vector    []byte
	CreatedAt int64
}


// GetChatPrefsRequest fetches the persisted per-chat preference row.
type GetChatPrefsRequest struct {
	ChatJID string `json:"chat_jid"`
}

// SetChatPrefsRequest is a partial-update payload. All fields are pointers
// so a client can flip a single flag without re-sending the whole record.
// At least one field must be non-nil; the validator enforces that.
type SetChatPrefsRequest struct {
	ChatJID                  string  `json:"chat_jid"`
	TargetLang               *string `json:"target_lang,omitempty"`
	AutoTranslateInbound     *bool   `json:"auto_translate_inbound,omitempty"`
	AutoTranslateOutbound    *bool   `json:"auto_translate_outbound,omitempty"`
	TranslationOptIn         *bool   `json:"translation_opt_in,omitempty"`
}

// GetChatPrefsResponse echoes the persisted state plus the effective target
// language (per-chat → global default). The UI renders EffectiveTargetLang
// in the header so the user can see what's actually being used.
type GetChatPrefsResponse struct {
	ChatJID               string `json:"chat_jid"`
	TargetLang            string `json:"target_lang"`
	AutoTranslateInbound  bool   `json:"auto_translate_inbound"`
	AutoTranslateOutbound bool   `json:"auto_translate_outbound"`
	TranslationOptIn      bool   `json:"translation_opt_in"`
	EffectiveTargetLang   string `json:"effective_target_lang"`
	UpdatedAt             int64  `json:"updated_at,omitempty"`
}
