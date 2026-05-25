package translation

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
)

// SQLiteRepository implements ITranslationRepository against the chat storage DB.
// It shares the same *sql.DB the chatstorage repository uses, and the schema is
// installed via migrations 15-19 in chatstorage.getMigrations().
type SQLiteRepository struct {
	db *sql.DB
}

// NewSQLiteRepository wires a translation repository on top of the existing DB.
func NewSQLiteRepository(db *sql.DB) domainTranslation.ITranslationRepository {
	return &SQLiteRepository{db: db}
}

// --- cache --------------------------------------------------------------

func (r *SQLiteRepository) GetCachedTranslation(deviceID, messageID, chatJID, targetLang, promptVersion string) (*domainTranslation.CachedTranslation, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("translation repository: nil db")
	}

	row := r.db.QueryRow(`
		SELECT message_id, chat_jid, device_id, target_lang, source_lang,
		       provider, prompt_version, suggestions_json, created_at
		FROM message_translations
		WHERE message_id = ? AND chat_jid = ? AND device_id = ?
		      AND target_lang = ? AND prompt_version = ?
	`, messageID, chatJID, deviceID, targetLang, promptVersion)

	var (
		entry           domainTranslation.CachedTranslation
		suggestionsJSON string
		createdAt       time.Time
	)
	err := row.Scan(
		&entry.MessageID, &entry.ChatJID, &entry.DeviceID,
		&entry.TargetLang, &entry.SourceLang,
		&entry.Provider, &entry.PromptVersion, &suggestionsJSON, &createdAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(suggestionsJSON), &entry.Suggestions); err != nil {
		return nil, err
	}
	entry.CreatedAt = createdAt
	return &entry, nil
}

func (r *SQLiteRepository) SaveCachedTranslation(entry *domainTranslation.CachedTranslation) error {
	if entry == nil {
		return errors.New("translation repository: nil cache entry")
	}
	suggestionsJSON, err := json.Marshal(entry.Suggestions)
	if err != nil {
		return err
	}

	// UPSERT: try update first, insert if no row affected (cross-DB safe).
	res, err := r.db.Exec(`
		UPDATE message_translations
		SET source_lang = ?, provider = ?, suggestions_json = ?, created_at = ?
		WHERE message_id = ? AND chat_jid = ? AND device_id = ?
		      AND target_lang = ? AND prompt_version = ?
	`,
		entry.SourceLang, entry.Provider, string(suggestionsJSON), time.Now(),
		entry.MessageID, entry.ChatJID, entry.DeviceID,
		entry.TargetLang, entry.PromptVersion,
	)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows > 0 {
		return nil
	}

	_, err = r.db.Exec(`
		INSERT INTO message_translations
			(message_id, chat_jid, device_id, target_lang, source_lang,
			 provider, prompt_version, suggestions_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		entry.MessageID, entry.ChatJID, entry.DeviceID,
		entry.TargetLang, entry.SourceLang,
		entry.Provider, entry.PromptVersion, string(suggestionsJSON), time.Now(),
	)
	return err
}

// --- per-chat preferences -----------------------------------------------

func (r *SQLiteRepository) GetChatPref(deviceID, chatJID string) (*domainTranslation.ChatPref, error) {
	row := r.db.QueryRow(`
		SELECT device_id, chat_jid, target_lang, auto_translate_inbound, auto_translate_outbound
		FROM chat_translation_prefs
		WHERE device_id = ? AND chat_jid = ?
	`, deviceID, chatJID)

	var pref domainTranslation.ChatPref
	err := row.Scan(&pref.DeviceID, &pref.ChatJID, &pref.TargetLang,
		&pref.AutoTranslateInbound, &pref.AutoTranslateOutbound)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &pref, nil
}

func (r *SQLiteRepository) UpsertChatPref(pref *domainTranslation.ChatPref) error {
	if pref == nil {
		return errors.New("translation repository: nil chat pref")
	}
	now := time.Now()

	res, err := r.db.Exec(`
		UPDATE chat_translation_prefs
		SET target_lang = ?, auto_translate_inbound = ?, auto_translate_outbound = ?, updated_at = ?
		WHERE device_id = ? AND chat_jid = ?
	`, pref.TargetLang, pref.AutoTranslateInbound, pref.AutoTranslateOutbound, now,
		pref.DeviceID, pref.ChatJID)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows > 0 {
		return nil
	}

	_, err = r.db.Exec(`
		INSERT INTO chat_translation_prefs
			(device_id, chat_jid, target_lang, auto_translate_inbound, auto_translate_outbound, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, pref.DeviceID, pref.ChatJID, pref.TargetLang,
		pref.AutoTranslateInbound, pref.AutoTranslateOutbound, now, now)
	return err
}

// --- embeddings (Phase 3) -----------------------------------------------

func (r *SQLiteRepository) SaveEmbedding(emb *domainTranslation.MessageEmbedding) error {
	if emb == nil {
		return errors.New("translation repository: nil embedding")
	}
	vec, err := json.Marshal(emb.Vector)
	if err != nil {
		return err
	}
	res, err := r.db.Exec(`
		UPDATE message_embeddings SET dim = ?, vector_json = ?, created_at = ?
		WHERE message_id = ? AND chat_jid = ? AND device_id = ? AND model = ?
	`, emb.Dim, string(vec), time.Now(), emb.MessageID, emb.ChatJID, emb.DeviceID, emb.Model)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows > 0 {
		return nil
	}
	_, err = r.db.Exec(`
		INSERT INTO message_embeddings (message_id, chat_jid, device_id, model, dim, vector_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, emb.MessageID, emb.ChatJID, emb.DeviceID, emb.Model, emb.Dim, string(vec), time.Now())
	return err
}

func (r *SQLiteRepository) GetEmbedding(deviceID, messageID, chatJID, model string) (*domainTranslation.MessageEmbedding, error) {
	row := r.db.QueryRow(`
		SELECT message_id, chat_jid, device_id, model, dim, vector_json, created_at
		FROM message_embeddings
		WHERE device_id = ? AND message_id = ? AND chat_jid = ? AND model = ?
	`, deviceID, messageID, chatJID, model)

	var (
		emb     domainTranslation.MessageEmbedding
		vecJSON string
		created time.Time
	)
	err := row.Scan(&emb.MessageID, &emb.ChatJID, &emb.DeviceID, &emb.Model,
		&emb.Dim, &vecJSON, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(vecJSON), &emb.Vector); err != nil {
		return nil, err
	}
	emb.CreatedAt = created
	return &emb, nil
}

// ListEmbeddingsByChat returns recent embedded messages for a single chat,
// joined with the messages table so the caller has both the vector and the
// original content. Newest-first; the usecase reorders for the prompt.
func (r *SQLiteRepository) ListEmbeddingsByChat(deviceID, chatJID, model string, limit int) ([]*domainTranslation.EmbeddedMessage, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := r.db.Query(`
		SELECT e.message_id, e.chat_jid, m.sender, m.content, m.is_from_me, e.vector_json, m.timestamp
		FROM message_embeddings e
		INNER JOIN messages m
			ON m.id = e.message_id
			AND m.chat_jid = e.chat_jid
			AND m.device_id = e.device_id
		WHERE e.device_id = ? AND e.chat_jid = ? AND e.model = ?
		ORDER BY m.timestamp DESC
		LIMIT ?
	`, deviceID, chatJID, model, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEmbeddedMessages(rows)
}

// ListUserStyleEmbeddings returns embedded messages the user wrote themselves
// (is_from_me = true), across all chats. Powers the user-style retrieval
// pool that biases the tone_matched suggestion toward how the user actually
// writes.
func (r *SQLiteRepository) ListUserStyleEmbeddings(deviceID, model string, limit int) ([]*domainTranslation.EmbeddedMessage, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := r.db.Query(`
		SELECT e.message_id, e.chat_jid, m.sender, m.content, m.is_from_me, e.vector_json, m.timestamp
		FROM message_embeddings e
		INNER JOIN messages m
			ON m.id = e.message_id
			AND m.chat_jid = e.chat_jid
			AND m.device_id = e.device_id
		WHERE e.device_id = ? AND e.model = ? AND m.is_from_me = ?
		ORDER BY m.timestamp DESC
		LIMIT ?
	`, deviceID, model, 1, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEmbeddedMessages(rows)
}

// ListMessageIDsMissingEmbedding returns recent messages with text content
// that don't yet have an embedding row for the given model. Drives the lazy
// backfill worker.
//
// Uses LEFT JOIN ... IS NULL rather than NOT EXISTS for SQLite/MySQL/Postgres
// portability; both forms compile but LEFT JOIN tends to be friendlier to
// query planners that don't optimize correlated subqueries well.
func (r *SQLiteRepository) ListMessageIDsMissingEmbedding(deviceID, chatJID, model string, limit int) ([]domainTranslation.MessageEmbeddingTarget, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := r.db.Query(`
		SELECT m.id, m.chat_jid, m.content
		FROM messages m
		LEFT JOIN message_embeddings e
			ON e.message_id = m.id
			AND e.chat_jid = m.chat_jid
			AND e.device_id = m.device_id
			AND e.model = ?
		WHERE m.device_id = ? AND m.chat_jid = ?
			AND e.message_id IS NULL
			AND m.content IS NOT NULL AND m.content != ''
		ORDER BY m.timestamp DESC
		LIMIT ?
	`, model, deviceID, chatJID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domainTranslation.MessageEmbeddingTarget
	for rows.Next() {
		var t domainTranslation.MessageEmbeddingTarget
		if err := rows.Scan(&t.MessageID, &t.ChatJID, &t.Content); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// scanEmbeddedMessages decodes rows from the standard embedded-join SELECT
// shape used by ListEmbeddingsByChat and ListUserStyleEmbeddings.
func scanEmbeddedMessages(rows *sql.Rows) ([]*domainTranslation.EmbeddedMessage, error) {
	var out []*domainTranslation.EmbeddedMessage
	for rows.Next() {
		var (
			em      domainTranslation.EmbeddedMessage
			vecJSON string
			ts      time.Time
		)
		if err := rows.Scan(&em.MessageID, &em.ChatJID, &em.Sender, &em.Content,
			&em.IsFromMe, &vecJSON, &ts); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(vecJSON), &em.Vector); err != nil {
			// Skip malformed vectors rather than fail the whole retrieval —
			// retrieval is best-effort and we'd rather degrade gracefully.
			continue
		}
		em.Timestamp = ts
		out = append(out, &em)
	}
	return out, rows.Err()
}
