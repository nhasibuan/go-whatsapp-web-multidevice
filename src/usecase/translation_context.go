package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
	"github.com/sirupsen/logrus"
)

// buildProviderInput is the single decision point in the translation flow.
// It assembles the ProviderRequest the LLM sees, choosing between
// system-context (Phase 1) and RAG-augmented retrieval (Phase 3) based on
// configuration plus runtime signals (embedder available, retrieval
// returned anything). Every RAG failure mode falls through cleanly to the
// system-context path so a fresh DB still produces translations.
func (s *serviceTranslation) buildProviderInput(
	ctx context.Context,
	deviceID, chatJID, excludeMessageID, sourceText, sourceLang, targetLang string,
	before time.Time,
) (domainTranslation.ProviderRequest, bool) {
	in := domainTranslation.ProviderRequest{
		SourceText: sourceText,
		SourceLang: sourceLang,
		TargetLang: targetLang,
	}

	useRAG := config.TranslationRAGEnabled && s.embedder != nil
	if useRAG {
		// Kick a backfill burst before retrieval — the work is async and
		// idempotent, so it has no effect on this request but speeds up
		// the next one for fresh chats.
		s.kickBackfill(deviceID, chatJID)

		if rag := s.retrieveSimilarMessages(ctx, deviceID, chatJID, sourceText, excludeMessageID); rag.Used {
			in.Context = rag.Context
			in.StyleExamples = rag.StyleExamples
			return in, true
		}
		// Retrieval came up empty (fresh chat, embedding failure, etc.) —
		// drop through to system context so the user still gets a result.
	}

	in.Context = s.loadContext(deviceID, chatJID, excludeMessageID, before)
	return in, false
}

// loadContext pulls up to TRANSLATION_CONTEXT_WINDOW recent messages from the
// chat (excluding the message being translated, if any) and returns them in
// chronological order so providers see oldest -> newest. This is the Phase 1
// fallback path used when RAG is off or retrieval returned nothing useful.
func (s *serviceTranslation) loadContext(deviceID, chatJID, excludeID string, before time.Time) []domainTranslation.ContextMessage {
	if chatJID == "" {
		return nil
	}
	window := config.TranslationContextWindow
	if window <= 0 {
		return nil
	}

	// Fetch a few extra to allow excluding the source message and any empty rows.
	limit := window + 5
	filter := &domainChatStorage.MessageFilter{
		DeviceID: deviceID,
		ChatJID:  chatJID,
		Limit:    limit,
	}
	if !before.IsZero() {
		t := before
		filter.EndTime = &t
	}

	msgs, err := s.chatStorageRepo.GetMessages(filter)
	if err != nil {
		logrus.WithError(err).WithField("chat_jid", chatJID).Warn("translation: failed to load context messages")
		return nil
	}

	// Storage returns newest -> oldest. Reverse and trim down to the window size.
	out := make([]domainTranslation.ContextMessage, 0, window)
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m == nil {
			continue
		}
		if excludeID != "" && m.ID == excludeID {
			continue
		}
		text := strings.TrimSpace(m.Content)
		if text == "" {
			continue
		}
		out = append(out, domainTranslation.ContextMessage{
			Sender:    m.Sender,
			Content:   text,
			IsFromMe:  m.IsFromMe,
			Timestamp: m.Timestamp.UTC().Format(time.RFC3339),
		})
	}
	if len(out) > window {
		out = out[len(out)-window:]
	}
	return out
}
