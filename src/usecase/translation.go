package usecase

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
	infraTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/translation"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/sirupsen/logrus"
)

type serviceTranslation struct {
	chatStorageRepo domainChatStorage.IChatStorageRepository
	translationRepo domainTranslation.ITranslationRepository
	provider        domainTranslation.Provider

	// embedProvider is optional. When nil OR config.TranslationRAGEnabled is
	// false, the service falls back to the system-context path (Phase 1).
	embedProvider domainTranslation.EmbeddingProvider

	// backfillInFlight tracks (deviceID|chatJID) keys whose backfill goroutine
	// is currently running, so we don't spawn duplicates when a user clicks
	// translate several times in quick succession.
	backfillInFlight sync.Map
}

// NewTranslationService wires the translation usecase.
//
// provider is REQUIRED; when nil the service returns a clear error from every
// call so the rest of the app keeps booting.
//
// embedProvider is OPTIONAL — pass nil to disable RAG (the service then uses
// the recent-N-messages system context). When non-nil AND
// config.TranslationRAGEnabled is true, the service:
//  1. Embeds the source text on each call,
//  2. Retrieves top-K similar messages from the same chat + top-K user-style
//     exemplars across all chats,
//  3. Lazily backfills the chat's embeddings in the background.
func NewTranslationService(
	chatStorageRepo domainChatStorage.IChatStorageRepository,
	translationRepo domainTranslation.ITranslationRepository,
	provider domainTranslation.Provider,
	embedProvider domainTranslation.EmbeddingProvider,
) domainTranslation.ITranslationUsecase {
	return &serviceTranslation{
		chatStorageRepo: chatStorageRepo,
		translationRepo: translationRepo,
		provider:        provider,
		embedProvider:   embedProvider,
	}
}

// TranslateMessage translates a stored message by id, returning 3 suggestions.
func (s *serviceTranslation) TranslateMessage(ctx context.Context, request domainTranslation.TranslateMessageRequest) (domainTranslation.TranslateResponse, error) {
	var response domainTranslation.TranslateResponse

	if err := s.ensureReady(); err != nil {
		return response, err
	}
	if err := validations.ValidateTranslateMessage(ctx, &request); err != nil {
		return response, err
	}

	deviceID := deviceIDFromContext(ctx)
	if deviceID == "" {
		return response, fmt.Errorf("device identification required")
	}

	msg, err := s.chatStorageRepo.GetMessageByID(request.MessageID)
	if err != nil {
		return response, fmt.Errorf("lookup message: %w", err)
	}
	if msg == nil {
		return response, fmt.Errorf("message %s not found", request.MessageID)
	}
	if request.ChatJID != "" && msg.ChatJID != request.ChatJID {
		return response, fmt.Errorf("message %s does not belong to chat %s", request.MessageID, request.ChatJID)
	}
	if strings.TrimSpace(msg.Content) == "" {
		return response, fmt.Errorf("message %s has no text content to translate", request.MessageID)
	}

	targetLang := s.resolveTargetLang(deviceID, msg.ChatJID, request.TargetLang)

	// Cache hit?
	if config.TranslationCacheEnabled && s.translationRepo != nil {
		if cached, err := s.translationRepo.GetCachedTranslation(deviceID, msg.ID, msg.ChatJID, targetLang, infraTranslation.PromptVersion); err == nil && cached != nil {
			return domainTranslation.TranslateResponse{
				MessageID:   msg.ID,
				SourceText:  msg.Content,
				SourceLang:  cached.SourceLang,
				TargetLang:  targetLang,
				Provider:    cached.Provider,
				CacheHit:    true,
				Suggestions: cached.Suggestions,
			}, nil
		} else if err != nil {
			logrus.WithError(err).Warn("translation cache lookup failed; falling through to provider")
		}
	}

	contextMsgs, styleExamples := s.buildProviderInput(ctx, deviceID, msg.Content, msg.ChatJID, msg.ID)

	suggestions, err := s.provider.GenerateSuggestions(ctx, domainTranslation.ProviderRequest{
		SourceText:    msg.Content,
		SourceLang:    request.SourceLang,
		TargetLang:    targetLang,
		Context:       contextMsgs,
		StyleExamples: styleExamples,
	})
	if err != nil {
		return response, fmt.Errorf("translation provider: %w", err)
	}

	// Persist cache (best-effort)
	if config.TranslationCacheEnabled && s.translationRepo != nil {
		_ = s.translationRepo.SaveCachedTranslation(&domainTranslation.CachedTranslation{
			MessageID:     msg.ID,
			ChatJID:       msg.ChatJID,
			DeviceID:      deviceID,
			TargetLang:    targetLang,
			SourceLang:    request.SourceLang,
			Provider:      s.provider.Name(),
			PromptVersion: infraTranslation.PromptVersion,
			Suggestions:   suggestions,
		})
	}

	return domainTranslation.TranslateResponse{
		MessageID:   msg.ID,
		SourceText:  msg.Content,
		SourceLang:  request.SourceLang,
		TargetLang:  targetLang,
		Provider:    s.provider.Name(),
		CacheHit:    false,
		Suggestions: suggestions,
	}, nil
}

// TranslateDraft translates arbitrary user-supplied text (compose-assist).
// Drafts are not cached because the text isn't a stable key.
func (s *serviceTranslation) TranslateDraft(ctx context.Context, request domainTranslation.TranslateDraftRequest) (domainTranslation.TranslateResponse, error) {
	var response domainTranslation.TranslateResponse

	if err := s.ensureReady(); err != nil {
		return response, err
	}
	if err := validations.ValidateTranslateDraft(ctx, &request); err != nil {
		return response, err
	}

	deviceID := deviceIDFromContext(ctx)
	targetLang := s.resolveTargetLang(deviceID, request.ChatJID, request.TargetLang)

	var contextMsgs []domainTranslation.ContextMessage
	var styleExamples []string
	if request.ChatJID != "" && deviceID != "" {
		contextMsgs, styleExamples = s.buildProviderInput(ctx, deviceID, request.Text, request.ChatJID, "")
	}

	suggestions, err := s.provider.GenerateSuggestions(ctx, domainTranslation.ProviderRequest{
		SourceText:    request.Text,
		SourceLang:    request.SourceLang,
		TargetLang:    targetLang,
		Context:       contextMsgs,
		StyleExamples: styleExamples,
	})
	if err != nil {
		return response, fmt.Errorf("translation provider: %w", err)
	}

	return domainTranslation.TranslateResponse{
		SourceText:    request.Text,
		SourceLang:    request.SourceLang,
		TargetLang:    targetLang,
		Provider:      s.provider.Name(),
		Suggestions:   suggestions,
	}, nil
}

// buildProviderInput is the central decision point for what the provider sees
// as context. It returns:
//
//   - contextMsgs: ordered oldest→newest "thread-like" messages used for the
//     `natural` and `tone_matched` variants.
//   - styleExamples: bare strings of the user's own writing, used to bias
//     `tone_matched` toward how they actually write.
//
// When RAG is enabled and a query embedding is obtainable, contextMsgs comes
// from semantic retrieval over the chat's embeddings, and styleExamples comes
// from the user-style pool. Otherwise both come from the recent-N system
// context (Phase 1 behavior), and styleExamples is left empty.
func (s *serviceTranslation) buildProviderInput(ctx context.Context, deviceID, sourceText, chatJID, excludeID string) ([]domainTranslation.ContextMessage, []string) {
	if !s.ragAvailable() {
		return s.loadChatContext(deviceID, chatJID, excludeID), nil
	}

	// Always kick off lazy backfill so subsequent calls have more material to
	// retrieve from. Idempotent + rate-limited internally.
	s.kickBackfillIfNeeded(deviceID, chatJID)

	ctxMsgs, styleEx, err := s.loadRAGContext(ctx, deviceID, sourceText, chatJID, excludeID)
	if err != nil {
		logrus.WithError(err).Warn("RAG retrieval failed; falling back to system context")
		return s.loadChatContext(deviceID, chatJID, excludeID), nil
	}

	// If retrieval came back empty (e.g. brand-new chat with no embeddings
	// yet), fall back to system context so the user still gets a useful
	// thread. The backfill goroutine kicked above will populate the index
	// for next time.
	if len(ctxMsgs) == 0 {
		return s.loadChatContext(deviceID, chatJID, excludeID), styleEx
	}
	return ctxMsgs, styleEx
}

// loadRAGContext embeds the query text and runs two top-K retrievals:
//  1. per-chat: most semantically similar messages from the same chat
//  2. user-style: most semantically similar messages the user wrote themselves
//
// Both pools are bounded by config (TranslationRAGPerChatPool /
// TranslationRAGStylePool) before scoring, so cost is O(pool_size) per call.
func (s *serviceTranslation) loadRAGContext(ctx context.Context, deviceID, query, chatJID, excludeID string) ([]domainTranslation.ContextMessage, []string, error) {
	if s.embedProvider == nil || s.translationRepo == nil {
		return nil, nil, fmt.Errorf("RAG dependencies missing")
	}

	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil, nil
	}

	vecs, err := s.embedProvider.Embed(ctx, []string{q})
	if err != nil {
		return nil, nil, fmt.Errorf("embed query: %w", err)
	}
	if len(vecs) != 1 || len(vecs[0]) == 0 {
		return nil, nil, fmt.Errorf("embed query: empty result")
	}
	queryVec := vecs[0]
	model := s.embedProvider.Model()

	// Pull bounded candidate pools.
	perChatPool, err := s.translationRepo.ListEmbeddingsByChat(deviceID, chatJID, model, configIntDefault(config.TranslationRAGPerChatPool, 200))
	if err != nil {
		logrus.WithError(err).Warn("ListEmbeddingsByChat failed; per-chat retrieval disabled for this call")
		perChatPool = nil
	}
	stylePool, err := s.translationRepo.ListUserStyleEmbeddings(deviceID, model, configIntDefault(config.TranslationRAGStylePool, 500))
	if err != nil {
		logrus.WithError(err).Warn("ListUserStyleEmbeddings failed; user-style retrieval disabled for this call")
		stylePool = nil
	}

	// Filter the per-chat pool to exclude the source message itself when
	// translating a stored message.
	if excludeID != "" {
		filtered := perChatPool[:0]
		for _, c := range perChatPool {
			if c.MessageID != excludeID {
				filtered = append(filtered, c)
			}
		}
		perChatPool = filtered
	}

	// Score and pick top-K from each pool.
	perChatTop := topKExternal(queryVec, perChatPool, configIntDefault(config.TranslationRAGPerChatK, 8))
	styleTop := topKExternal(queryVec, stylePool, configIntDefault(config.TranslationRAGStyleK, 4))

	// Build the per-chat context as ContextMessages, ordered oldest→newest
	// the way the prompt expects (the topK helper returned newest-first).
	sort.Slice(perChatTop, func(i, j int) bool {
		return perChatTop[i].Timestamp.Before(perChatTop[j].Timestamp)
	})
	ctxMsgs := make([]domainTranslation.ContextMessage, 0, len(perChatTop))
	for _, m := range perChatTop {
		if m == nil || strings.TrimSpace(m.Content) == "" {
			continue
		}
		ctxMsgs = append(ctxMsgs, domainTranslation.ContextMessage{
			Sender:   m.Sender,
			Content:  m.Content,
			IsFromMe: m.IsFromMe,
		})
	}

	// Style examples are bare strings — the prompt formats them itself.
	styleEx := make([]string, 0, len(styleTop))
	for _, m := range styleTop {
		if m == nil {
			continue
		}
		t := strings.TrimSpace(m.Content)
		if t == "" {
			continue
		}
		styleEx = append(styleEx, t)
	}

	return ctxMsgs, styleEx, nil
}

// kickBackfillIfNeeded fires off a background goroutine to embed unindexed
// messages in this chat. Idempotent: a second call for the same chat while
// one is already running is a no-op. Uses context.Background() so it survives
// the request.
func (s *serviceTranslation) kickBackfillIfNeeded(deviceID, chatJID string) {
	if !s.ragAvailable() || deviceID == "" || chatJID == "" {
		return
	}
	limit := configIntDefault(config.TranslationRAGBackfillLimit, 100)
	if limit <= 0 {
		return
	}
	key := deviceID + "|" + chatJID
	if _, alreadyRunning := s.backfillInFlight.LoadOrStore(key, struct{}{}); alreadyRunning {
		return
	}
	go func() {
		defer s.backfillInFlight.Delete(key)
		// Detached context: keep the work going after the HTTP response.
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		s.runBackfill(bgCtx, deviceID, chatJID, limit)
	}()
}

// runBackfill embeds up to `limit` unindexed messages in the chat in batches.
// Errors are logged at warn level; the worker continues with the next batch
// so a single bad row doesn't stall the whole job.
func (s *serviceTranslation) runBackfill(ctx context.Context, deviceID, chatJID string, limit int) {
	if s.embedProvider == nil || s.translationRepo == nil {
		return
	}
	model := s.embedProvider.Model()
	batch := configIntDefault(config.TranslationRAGBackfillBatch, 32)
	if batch <= 0 {
		batch = 32
	}

	remaining := limit
	for remaining > 0 {
		select {
		case <-ctx.Done():
			return
		default:
		}

		take := batch
		if take > remaining {
			take = remaining
		}
		targets, err := s.translationRepo.ListMessageIDsMissingEmbedding(deviceID, chatJID, model, take)
		if err != nil {
			logrus.WithError(err).Warn("RAG backfill: list missing failed")
			return
		}
		if len(targets) == 0 {
			return // caught up
		}

		texts := make([]string, 0, len(targets))
		for _, t := range targets {
			texts = append(texts, t.Content)
		}
		vecs, err := s.embedProvider.Embed(ctx, texts)
		if err != nil {
			logrus.WithError(err).Warn("RAG backfill: embed batch failed")
			return
		}
		if len(vecs) != len(targets) {
			logrus.Warnf("RAG backfill: vector count mismatch (%d vs %d)", len(vecs), len(targets))
			return
		}

		now := time.Now()
		_ = now // reserved for future per-row metrics
		for i, t := range targets {
			if err := s.translationRepo.SaveEmbedding(&domainTranslation.MessageEmbedding{
				MessageID: t.MessageID,
				ChatJID:   t.ChatJID,
				DeviceID:  deviceID,
				Model:     model,
				Dim:       len(vecs[i]),
				Vector:    vecs[i],
			}); err != nil {
				logrus.WithError(err).Warnf("RAG backfill: save embedding failed for %s", t.MessageID)
				// keep going — one bad row shouldn't kill the batch
			}
		}
		remaining -= len(targets)
	}
}

// loadChatContext fetches up to TranslationContextWindow recent messages from
// the same chat (excluding excludeID), oldest-first. Empty/media-only messages
// are skipped because they add cost without semantic value.
func (s *serviceTranslation) loadChatContext(deviceID, chatJID, excludeID string) []domainTranslation.ContextMessage {
	if s.chatStorageRepo == nil || chatJID == "" || deviceID == "" {
		return nil
	}
	limit := config.TranslationContextWindow
	if limit <= 0 {
		return nil
	}
	// Pull a few extra so we can skip the source message + empties
	fetch := limit + 5

	msgs, err := s.chatStorageRepo.GetMessages(&domainChatStorage.MessageFilter{
		DeviceID: deviceID,
		ChatJID:  chatJID,
		Limit:    fetch,
		Offset:   0,
	})
	if err != nil || len(msgs) == 0 {
		return nil
	}

	// GetMessages typically returns newest-first; sort oldest-first for the prompt.
	sort.Slice(msgs, func(i, j int) bool {
		return msgs[i].Timestamp.Before(msgs[j].Timestamp)
	})

	out := make([]domainTranslation.ContextMessage, 0, limit)
	for _, m := range msgs {
		if m == nil || m.ID == excludeID {
			continue
		}
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		out = append(out, domainTranslation.ContextMessage{
			Sender:   m.Sender,
			Content:  m.Content,
			IsFromMe: m.IsFromMe,
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

// resolveTargetLang implements the source-of-truth precedence:
//  1. explicit request param
//  2. per-chat preference
//  3. global default from config
func (s *serviceTranslation) resolveTargetLang(deviceID, chatJID, requested string) string {
	if v := strings.TrimSpace(requested); v != "" {
		return v
	}
	if s.translationRepo != nil && deviceID != "" && chatJID != "" {
		if pref, _ := s.translationRepo.GetChatPref(deviceID, chatJID); pref != nil && pref.TargetLang != "" {
			return pref.TargetLang
		}
	}
	if config.TranslationDefaultTargetLang != "" {
		return config.TranslationDefaultTargetLang
	}
	return "en"
}

// ensureReady verifies the feature is configured. Without it, the rest of the
// app boots but translation endpoints return a clear 4xx-style error.
func (s *serviceTranslation) ensureReady() error {
	if !config.TranslationEnabled {
		return fmt.Errorf("translation feature is disabled (set TRANSLATION_ENABLED=true)")
	}
	if s.provider == nil {
		return fmt.Errorf("translation provider is not configured (check TRANSLATION_API_KEY)")
	}
	return nil
}

// ragAvailable reports whether the RAG branch should be considered. The flag
// gate happens here so any caller that wants to know "is RAG on right now?"
// has a single source of truth.
func (s *serviceTranslation) ragAvailable() bool {
	return config.TranslationRAGEnabled &&
		s.embedProvider != nil &&
		s.translationRepo != nil &&
		s.chatStorageRepo != nil
}

// topKExternal is a thin shim that calls into infra/translation.topK, exposed
// here through a small wrapper so the usecase isn't reaching into unexported
// helpers in the infrastructure package. We rely on the infra package to
// expose this via a public alias-style function — see its TopK below.
func topKExternal(query []float32, candidates []*domainTranslation.EmbeddedMessage, k int) []*domainTranslation.EmbeddedMessage {
	return infraTranslation.TopK(query, candidates, k)
}

// configIntDefault returns v if positive, else fallback. Centralizes the
// "config zero means use default" pattern used across this file.
func configIntDefault(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}
