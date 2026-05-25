package usecase

import (
	"context"
	"fmt"
	"sort"
	"strings"

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
}

// NewTranslationService wires the translation usecase.
//
// provider is REQUIRED — pass a *infraTranslation.OpenAIProvider (or any other
// domainTranslation.Provider implementation). When provider is nil the service
// returns a clear error from every call so the rest of the app keeps booting
// even if TRANSLATION_API_KEY is unset.
func NewTranslationService(
	chatStorageRepo domainChatStorage.IChatStorageRepository,
	translationRepo domainTranslation.ITranslationRepository,
	provider domainTranslation.Provider,
) domainTranslation.ITranslationUsecase {
	return &serviceTranslation{
		chatStorageRepo: chatStorageRepo,
		translationRepo: translationRepo,
		provider:        provider,
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

	// Pull source message
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

	// Build context window from prior messages in the same chat
	contextMsgs := s.loadChatContext(deviceID, msg.ChatJID, msg.ID)

	suggestions, err := s.provider.GenerateSuggestions(ctx, domainTranslation.ProviderRequest{
		SourceText: msg.Content,
		SourceLang: request.SourceLang,
		TargetLang: targetLang,
		Context:    contextMsgs,
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
	if request.ChatJID != "" && deviceID != "" {
		contextMsgs = s.loadChatContext(deviceID, request.ChatJID, "")
	}

	suggestions, err := s.provider.GenerateSuggestions(ctx, domainTranslation.ProviderRequest{
		SourceText: request.Text,
		SourceLang: request.SourceLang,
		TargetLang: targetLang,
		Context:    contextMsgs,
	})
	if err != nil {
		return response, fmt.Errorf("translation provider: %w", err)
	}

	return domainTranslation.TranslateResponse{
		SourceText:  request.Text,
		SourceLang:  request.SourceLang,
		TargetLang:  targetLang,
		Provider:    s.provider.Name(),
		Suggestions: suggestions,
	}, nil
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
