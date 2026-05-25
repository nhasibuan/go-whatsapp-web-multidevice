package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/sirupsen/logrus"
)

// serviceTranslation is the concrete usecase. Direct field access is
// unexported; construct via NewTranslationService and configure with the
// functional options below.
type serviceTranslation struct {
	chatStorageRepo domainChatStorage.IChatStorageRepository
	translationRepo domainTranslation.ITranslationRepository
	provider        domainTranslation.Provider
	embedder        domainTranslation.EmbeddingProvider // optional; nil => RAG disabled
	backfill        *backfillTracker
	now             func() time.Time // overridable for tests
}

// TranslationOption is a functional option for the translation usecase.
// Functional options are the idiomatic Go way to keep a single constructor
// signature stable while allowing future knobs (e.g. a custom clock for
// tests, a no-op embedder for the mock pipeline) without breaking callers.
type TranslationOption func(*serviceTranslation)

// WithEmbedder sets the embedding provider that powers Phase 3 (RAG). Pass
// nil — or omit the option entirely — to keep RAG disabled.
func WithEmbedder(embedder domainTranslation.EmbeddingProvider) TranslationOption {
	return func(s *serviceTranslation) { s.embedder = embedder }
}

// WithClock injects a deterministic time source. Production callers should
// not need this; tests use it to assert cache expiry without sleeping.
func WithClock(now func() time.Time) TranslationOption {
	return func(s *serviceTranslation) {
		if now != nil {
			s.now = now
		}
	}
}

// NewTranslationService wires the translation usecase. Provider and
// repositories are required positional dependencies; everything else is
// configured via functional options so the signature stays small.
func NewTranslationService(
	chatStorageRepo domainChatStorage.IChatStorageRepository,
	translationRepo domainTranslation.ITranslationRepository,
	provider domainTranslation.Provider,
	opts ...TranslationOption,
) domainTranslation.ITranslationUsecase {
	s := &serviceTranslation{
		chatStorageRepo: chatStorageRepo,
		translationRepo: translationRepo,
		provider:        provider,
		backfill:        newBackfillTracker(),
		now:             time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ---- Translate ----

func (s *serviceTranslation) TranslateMessage(ctx context.Context, request domainTranslation.TranslateMessageRequest) (response domainTranslation.TranslateMessageResponse, err error) {
	if !config.TranslationEnabled {
		return response, pkgError.TranslationDisabledError("translation is disabled (set TRANSLATION_ENABLED=true to enable)")
	}
	if err = validations.ValidateTranslateMessage(ctx, &request); err != nil {
		return response, err
	}
	deviceID := deviceIDFromContext(ctx)
	if deviceID == "" {
		return response, pkgError.TranslationDeviceMissingError("device identification required")
	}

	msg, err := s.chatStorageRepo.GetMessageByID(request.MessageID)
	if err != nil {
		logrus.WithError(err).WithField("message_id", request.MessageID).Error("translation: failed to load message")
		return response, err
	}
	if msg == nil {
		return response, pkgError.TranslationMessageNotFoundError(fmt.Sprintf("message %s not found", request.MessageID))
	}
	if msg.DeviceID != "" && msg.DeviceID != deviceID {
		// Don't leak cross-device data. Mirrors the device scoping rule from CLAUDE.md.
		return response, pkgError.TranslationMessageNotFoundError(fmt.Sprintf("message %s not found for current device", request.MessageID))
	}
	if msg.ChatJID != "" && request.ChatJID != "" && msg.ChatJID != request.ChatJID {
		return response, pkgError.TranslationMessageNotFoundError(fmt.Sprintf("message %s does not belong to chat %s", request.MessageID, request.ChatJID))
	}
	sourceText := strings.TrimSpace(msg.Content)
	if sourceText == "" {
		return response, pkgError.TranslationEmptyMessageError(fmt.Sprintf("message %s has no text content to translate", request.MessageID))
	}

	chatJID := request.ChatJID
	if chatJID == "" {
		chatJID = msg.ChatJID
	}

	targetLang := s.resolveTargetLang(deviceID, chatJID, request.TargetLang)
	sourceHash := hashSource(sourceText)
	cacheProviderKey := s.cacheProviderKey()

	suggestions, cached := s.tryCacheLookup(deviceID, chatJID, request.MessageID, targetLang, sourceHash, cacheProviderKey, request.ForceRefresh)

	var providerInput domainTranslation.ProviderRequest
	var ragUsed bool
	if suggestions == nil {
		providerInput, ragUsed = s.buildProviderInput(ctx, deviceID, chatJID, request.MessageID, sourceText, request.SourceLang, targetLang, msg.Timestamp)
		fresh, perr := s.callProvider(ctx, providerInput, request.MessageID)
		if perr != nil {
			return response, perr
		}
		suggestions = fresh
		s.persistCache(&domainTranslation.CachedTranslation{
			DeviceID:    deviceID,
			ChatJID:     chatJID,
			MessageID:   request.MessageID,
			TargetLang:  targetLang,
			SourceLang:  request.SourceLang,
			SourceHash:  sourceHash,
			Provider:    cacheProviderKey,
			Suggestions: suggestions,
			CreatedAt:   s.now().Unix(),
			ExpiresAt:   s.computeExpiresAt(),
		})
	}

	response.Suggestions = suggestions
	response.Cached = cached
	response.MessageID = request.MessageID
	response.ChatJID = chatJID
	response.SourceText = sourceText
	response.SourceLang = request.SourceLang
	response.TargetLang = targetLang
	response.Provider = s.provider.Name()

	logrus.WithFields(logrus.Fields{
		"provider":    response.Provider,
		"message_id":  response.MessageID,
		"target_lang": response.TargetLang,
		"cached":      response.Cached,
		"context_n":   len(providerInput.Context),
		"style_n":     len(providerInput.StyleExamples),
		"rag_used":    ragUsed,
	}).Info("translation: message translated")

	return response, nil
}

func (s *serviceTranslation) TranslateDraft(ctx context.Context, request domainTranslation.TranslateDraftRequest) (response domainTranslation.TranslateDraftResponse, err error) {
	if !config.TranslationEnabled {
		return response, pkgError.TranslationDisabledError("translation is disabled (set TRANSLATION_ENABLED=true to enable)")
	}
	if err = validations.ValidateTranslateDraft(ctx, &request); err != nil {
		return response, err
	}
	deviceID := deviceIDFromContext(ctx)
	if deviceID == "" {
		return response, pkgError.TranslationDeviceMissingError("device identification required")
	}

	targetLang := s.resolveTargetLang(deviceID, request.ChatJID, request.TargetLang)
	sourceHash := hashSource(request.Text)
	cacheProviderKey := s.cacheProviderKey()

	suggestions, cached := s.tryCacheLookup(deviceID, request.ChatJID, "", targetLang, sourceHash, cacheProviderKey, request.ForceRefresh)

	var providerInput domainTranslation.ProviderRequest
	var ragUsed bool
	if suggestions == nil {
		providerInput, ragUsed = s.buildProviderInput(ctx, deviceID, request.ChatJID, "", request.Text, request.SourceLang, targetLang, time.Time{})
		fresh, perr := s.callProvider(ctx, providerInput, "")
		if perr != nil {
			return response, perr
		}
		suggestions = fresh
		s.persistCache(&domainTranslation.CachedTranslation{
			DeviceID:    deviceID,
			ChatJID:     request.ChatJID,
			MessageID:   "",
			TargetLang:  targetLang,
			SourceLang:  request.SourceLang,
			SourceHash:  sourceHash,
			Provider:    cacheProviderKey,
			Suggestions: suggestions,
			CreatedAt:   s.now().Unix(),
			ExpiresAt:   s.computeExpiresAt(),
		})
	}

	response.Suggestions = suggestions
	response.Cached = cached
	response.ChatJID = request.ChatJID
	response.SourceText = request.Text
	response.SourceLang = request.SourceLang
	response.TargetLang = targetLang
	response.Provider = s.provider.Name()

	logrus.WithFields(logrus.Fields{
		"provider":    response.Provider,
		"target_lang": response.TargetLang,
		"cached":      response.Cached,
		"context_n":   len(providerInput.Context),
		"style_n":     len(providerInput.StyleExamples),
		"rag_used":    ragUsed,
		"text_len":    len(response.SourceText),
	}).Info("translation: draft translated")

	return response, nil
}

// callProvider centralizes provider invocation + error wrapping so every
// caller surfaces TranslationProviderError uniformly.
func (s *serviceTranslation) callProvider(ctx context.Context, in domainTranslation.ProviderRequest, messageID string) ([]domainTranslation.Suggestion, error) {
	out, err := s.provider.Translate(ctx, in)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"provider":   s.provider.Name(),
			"message_id": messageID,
		}).Error("translation: provider error")
		return nil, pkgError.TranslationProviderError(err.Error())
	}
	return out, nil
}

// hashSource is deterministic across processes so the cache key stays stable
// after restarts and across replicas. sha256 is overkill for plain text but
// avoids collisions when message content differs only by whitespace.
func hashSource(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:])
}
