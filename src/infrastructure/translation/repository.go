package translation

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
)

// sqliteRepository persists translation cache, per-chat preferences, and the
// staged embeddings table that Phase 3 (RAG) will populate.
//
// It shares the chatstorage SQLite database — the migrations that create
// these tables are appended to chatstorage's getMigrations() (entries 23-25)
// so there is exactly one schema source of truth and one migration timeline.
type sqliteRepository struct {
	db *sql.DB
}

// NewSQLiteRepository wires a translation repository over an existing SQL
// connection. Pass the same *sql.DB created in cmd/root.go for chatstorage.
func NewSQLiteRepository(db *sql.DB) domainTranslation.ITranslationRepository {
	return &sqliteRepository{db: db}
}

func (r *sqliteRepository) InitializeSchema() error {
	// The chatstorage repository owns the migration timeline. This is a
	// deliberate no-op so callers can treat the translation repo uniformly
	// regardless of where its tables live.
	return nil
}

// GetCachedTranslation returns a non-expired cache row matching every key.
// Returns (nil, nil) when there is no hit.
func (r *sqliteRepository) GetCachedTranslation(deviceID, chatJID, messageID, targetLang, sourceHash, provider string) (*domainTranslation.CachedTranslation, error) {
	row := r.db.QueryRow(`
		SELECT device_id, chat_jid, message_id, target_lang, source_lang,
			source_hash, provider, suggestions, created_at, expires_at
		FROM message_translations
		WHERE device_id = ? AND chat_jid = ? AND message_id = ?
			AND target_lang = ? AND source_hash = ? AND provider = ?
		LIMIT 1
	`, deviceID, chatJID, messageID, normalizeLang(targetLang), sourceHash, provider)

	var entry domainTranslation.CachedTranslation
	var suggestionsJSON string
	err := row.Scan(
		&entry.DeviceID, &entry.ChatJID, &entry.MessageID,
		&entry.TargetLang, &entry.SourceLang,
		&entry.SourceHash, &entry.Provider, &suggestionsJSON,
		&entry.CreatedAt, &entry.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	// Treat expired rows as misses without deleting them here — the periodic
	// PurgeExpiredTranslations call handles cleanup. This keeps reads cheap
	// and avoids surprising writers during a hot read path.
	if entry.ExpiresAt > 0 && entry.ExpiresAt < time.Now().Unix() {
		return nil, nil
	}

	if err := json.Unmarshal([]byte(suggestionsJSON), &entry.Suggestions); err != nil {
		return nil, err
	}
	return &entry, nil
}

// PutCachedTranslation upserts via update-then-insert to stay portable across
// SQLite/MySQL/PostgreSQL without an ON CONFLICT clause.
func (r *sqliteRepository) PutCachedTranslation(entry *domainTranslation.CachedTranslation) error {
	if entry == nil {
		return errors.New("translation cache: nil entry")
	}
	if strings.TrimSpace(entry.TargetLang) == "" {
		return errors.New("translation cache: target_lang is required")
	}
	if strings.TrimSpace(entry.SourceHash) == "" {
		return errors.New("translation cache: source_hash is required")
	}
	if strings.TrimSpace(entry.Provider) == "" {
		return errors.New("translation cache: provider is required")
	}

	suggestionsJSON, err := json.Marshal(entry.Suggestions)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	if entry.CreatedAt == 0 {
		entry.CreatedAt = now
	}
	target := normalizeLang(entry.TargetLang)
	source := normalizeLang(entry.SourceLang)

	res, err := r.db.Exec(`
		UPDATE message_translations
		SET source_lang = ?, suggestions = ?, created_at = ?, expires_at = ?
		WHERE device_id = ? AND chat_jid = ? AND message_id = ?
			AND target_lang = ? AND source_hash = ? AND provider = ?
	`, source, string(suggestionsJSON), entry.CreatedAt, entry.ExpiresAt,
		entry.DeviceID, entry.ChatJID, entry.MessageID,
		target, entry.SourceHash, entry.Provider)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows > 0 {
		return nil
	}

	_, err = r.db.Exec(`
		INSERT INTO message_translations
			(device_id, chat_jid, message_id, target_lang, source_lang,
			 source_hash, provider, suggestions, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, entry.DeviceID, entry.ChatJID, entry.MessageID,
		target, source, entry.SourceHash, entry.Provider,
		string(suggestionsJSON), entry.CreatedAt, entry.ExpiresAt)
	return err
}

func (r *sqliteRepository) PurgeExpiredTranslations() (int64, error) {
	res, err := r.db.Exec(`DELETE FROM message_translations WHERE expires_at > 0 AND expires_at < ?`, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (r *sqliteRepository) GetChatPref(deviceID, chatJID string) (*domainTranslation.ChatTranslationPref, error) {
	row := r.db.QueryRow(`
		SELECT device_id, chat_jid, target_lang, auto_translate, translation_opt_in, updated_at
		FROM chat_translation_prefs
		WHERE device_id = ? AND chat_jid = ?
		LIMIT 1
	`, deviceID, chatJID)

	var pref domainTranslation.ChatTranslationPref
	err := row.Scan(&pref.DeviceID, &pref.ChatJID, &pref.TargetLang, &pref.AutoTranslate, &pref.TranslationOptIn, &pref.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &pref, nil
}

func (r *sqliteRepository) SetChatPref(pref *domainTranslation.ChatTranslationPref) error {
	if pref == nil {
		return errors.New("translation pref: nil entry")
	}
	if strings.TrimSpace(pref.ChatJID) == "" {
		return errors.New("translation pref: chat_jid is required")
	}
	now := time.Now().Unix()
	if pref.UpdatedAt == 0 {
		pref.UpdatedAt = now
	}
	target := normalizeLang(pref.TargetLang)

	res, err := r.db.Exec(`
		UPDATE chat_translation_prefs
		SET target_lang = ?, auto_translate = ?, translation_opt_in = ?, updated_at = ?
		WHERE device_id = ? AND chat_jid = ?
	`, target, pref.AutoTranslate, pref.TranslationOptIn, pref.UpdatedAt, pref.DeviceID, pref.ChatJID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows > 0 {
		return nil
	}

	_, err = r.db.Exec(`
		INSERT INTO chat_translation_prefs (device_id, chat_jid, target_lang, auto_translate, translation_opt_in, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, pref.DeviceID, pref.ChatJID, target, pref.AutoTranslate, pref.TranslationOptIn, pref.UpdatedAt)
	return err
}

func (r *sqliteRepository) StoreEmbedding(emb *domainTranslation.MessageEmbedding) error {
	if emb == nil {
		return errors.New("embedding: nil entry")
	}
	if strings.TrimSpace(emb.MessageID) == "" {
		return errors.New("embedding: message_id is required")
	}
	if strings.TrimSpace(emb.Model) == "" {
		return errors.New("embedding: model is required")
	}
	if len(emb.Vector) == 0 {
		return errors.New("embedding: vector is empty")
	}
	now := time.Now().Unix()
	if emb.CreatedAt == 0 {
		emb.CreatedAt = now
	}

	// Decode the BLOB payload into floats and re-encode as JSON so retrieval
	// can do similarity in plain Go without depending on a vector extension.
	floats := bytesToFloats(emb.Vector)
	vectorJSON, err := json.Marshal(floats)
	if err != nil {
		return fmt.Errorf("embedding: marshal vector_json: %w", err)
	}

	res, err := r.db.Exec(`
		UPDATE message_embeddings
		SET chat_jid = ?, vector = ?, vector_json = ?, created_at = ?
		WHERE device_id = ? AND message_id = ? AND model = ?
	`, emb.ChatJID, emb.Vector, string(vectorJSON), emb.CreatedAt, emb.DeviceID, emb.MessageID, emb.Model)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows > 0 {
		return nil
	}

	_, err = r.db.Exec(`
		INSERT INTO message_embeddings (device_id, chat_jid, message_id, model, vector, vector_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, emb.DeviceID, emb.ChatJID, emb.MessageID, emb.Model, emb.Vector, string(vectorJSON), emb.CreatedAt)
	return err
}

func (r *sqliteRepository) GetEmbedding(deviceID, chatJID, messageID string) (*domainTranslation.MessageEmbedding, error) {
	row := r.db.QueryRow(`
		SELECT device_id, chat_jid, message_id, model, vector, created_at
		FROM message_embeddings
		WHERE device_id = ? AND message_id = ?
		LIMIT 1
	`, deviceID, messageID)

	var emb domainTranslation.MessageEmbedding
	if err := row.Scan(&emb.DeviceID, &emb.ChatJID, &emb.MessageID, &emb.Model, &emb.Vector, &emb.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if chatJID != "" && emb.ChatJID != "" && emb.ChatJID != chatJID {
		return nil, nil
	}
	return &emb, nil
}

// ListChatEmbeddingCandidates returns the N most recent embeddings for the
// chat plus enough message metadata to render each result back as a
// ContextMessage. The usecase scores cosine similarity in-memory so the SQL
// stays simple and portable across SQLite/MySQL/Postgres.
func (r *sqliteRepository) ListChatEmbeddingCandidates(deviceID, chatJID, model string, limit int) ([]*domainTranslation.EmbeddingCandidate, error) {
	if limit <= 0 {
		return nil, nil
	}
	const q = `
		SELECT e.message_id, e.chat_jid, e.vector_json,
			COALESCE(m.sender, ''),
			COALESCE(m.is_from_me, 0),
			COALESCE(m.content, ''),
			COALESCE(m.timestamp, '')
		FROM message_embeddings e
		LEFT JOIN messages m
			ON m.id = e.message_id AND m.chat_jid = e.chat_jid AND m.device_id = e.device_id
		WHERE e.device_id = ? AND e.chat_jid = ? AND e.model = ? AND e.vector_json <> ''
		ORDER BY e.created_at DESC
		LIMIT ?`
	return r.scanEmbeddingCandidates(q, deviceID, chatJID, model, limit)
}

// ListUserStyleEmbeddingCandidates returns the most recent outbound
// embeddings (is_from_me = true) authored by the user across every chat.
// These feed the tone_matched variant.
func (r *sqliteRepository) ListUserStyleEmbeddingCandidates(deviceID, model string, limit int) ([]*domainTranslation.EmbeddingCandidate, error) {
	if limit <= 0 {
		return nil, nil
	}
	const q = `
		SELECT e.message_id, e.chat_jid, e.vector_json,
			COALESCE(m.sender, ''),
			COALESCE(m.is_from_me, 1),
			COALESCE(m.content, ''),
			COALESCE(m.timestamp, '')
		FROM message_embeddings e
		INNER JOIN messages m
			ON m.id = e.message_id AND m.chat_jid = e.chat_jid AND m.device_id = e.device_id
		WHERE e.device_id = ? AND e.model = ?
			AND e.vector_json <> ''
			AND m.is_from_me = ?
		ORDER BY e.created_at DESC
		LIMIT ?`
	return r.scanEmbeddingCandidates(q, deviceID, model, 1, limit)
}

func (r *sqliteRepository) scanEmbeddingCandidates(query string, args ...any) ([]*domainTranslation.EmbeddingCandidate, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domainTranslation.EmbeddingCandidate
	for rows.Next() {
		var (
			messageID, chatJID, vectorJSON, sender, content, timestamp string
			isFromMe                                                   bool
		)
		var rawTimestamp sql.NullTime
		// The messages table stores TIMESTAMP, scan into NullTime then format.
		if err := rows.Scan(&messageID, &chatJID, &vectorJSON, &sender, &isFromMe, &content, &rawTimestamp); err != nil {
			// SQLite is forgiving — the timestamp column may already be a
			// formatted string. Fall back to a string scan if NullTime fails.
			return nil, err
		}
		if rawTimestamp.Valid {
			timestamp = rawTimestamp.Time.UTC().Format(time.RFC3339)
		}
		var vec []float32
		if vectorJSON != "" {
			if err := json.Unmarshal([]byte(vectorJSON), &vec); err != nil {
				// Skip rows with corrupted vectors instead of failing the whole query.
				continue
			}
		}
		out = append(out, &domainTranslation.EmbeddingCandidate{
			MessageID: messageID,
			ChatJID:   chatJID,
			Sender:    sender,
			IsFromMe:  isFromMe,
			Content:   content,
			Vector:    vec,
			Timestamp: timestamp,
		})
	}
	return out, rows.Err()
}

// CountEmbeddings reports how many vectors exist for (device, chat, model).
// Used by the usecase to gate the lazy backfill — no need to enqueue work
// when the chat is fully embedded.
func (r *sqliteRepository) CountEmbeddings(deviceID, chatJID, model string) (int64, error) {
	var n int64
	err := r.db.QueryRow(`
		SELECT COUNT(*) FROM message_embeddings
		WHERE device_id = ? AND chat_jid = ? AND model = ? AND vector_json <> ''
	`, deviceID, chatJID, model).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// ListMessagesNeedingEmbedding finds text-bearing messages that don't yet
// have a vector under the requested model. The query uses NOT EXISTS instead
// of LEFT JOIN so it stays cheap even for chats with thousands of messages.
func (r *sqliteRepository) ListMessagesNeedingEmbedding(deviceID, chatJID, model string, limit int) ([]*domainTranslation.EmbeddingBackfillItem, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := r.db.Query(`
		SELECT m.id, m.chat_jid, m.content
		FROM messages m
		WHERE m.device_id = ? AND m.chat_jid = ?
			AND m.content IS NOT NULL AND m.content <> ''
			AND NOT EXISTS (
				SELECT 1 FROM message_embeddings e
				WHERE e.device_id = m.device_id
					AND e.message_id = m.id
					AND e.model = ?
					AND e.vector_json <> ''
			)
		ORDER BY m.timestamp DESC
		LIMIT ?
	`, deviceID, chatJID, model, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domainTranslation.EmbeddingBackfillItem
	for rows.Next() {
		var item domainTranslation.EmbeddingBackfillItem
		if err := rows.Scan(&item.MessageID, &item.ChatJID, &item.Content); err != nil {
			return nil, err
		}
		out = append(out, &item)
	}
	return out, rows.Err()
}

// bytesToFloats decodes a little-endian float32 BLOB. Used during the
// migration from BLOB-only storage to BLOB+JSON: when we store a new vector
// we still have it as []byte from the OpenAI client, and we want the JSON
// shadow to match exactly.
func bytesToFloats(b []byte) []float32 {
	if len(b) < 4 || len(b)%4 != 0 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		bits := uint32(b[i*4]) |
			uint32(b[i*4+1])<<8 |
			uint32(b[i*4+2])<<16 |
			uint32(b[i*4+3])<<24
		out[i] = math.Float32frombits(bits)
	}
	return out
}

// FloatsToBytes encodes a float32 slice as little-endian bytes — exposed for
// the embedding provider so caller code stays out of the repository internals.
func FloatsToBytes(vec []float32) []byte {
	out := make([]byte, len(vec)*4)
	for i, v := range vec {
		bits := math.Float32bits(v)
		out[i*4] = byte(bits)
		out[i*4+1] = byte(bits >> 8)
		out[i*4+2] = byte(bits >> 16)
		out[i*4+3] = byte(bits >> 24)
	}
	return out
}

func normalizeLang(l string) string {
	return strings.ToLower(strings.TrimSpace(l))
}
