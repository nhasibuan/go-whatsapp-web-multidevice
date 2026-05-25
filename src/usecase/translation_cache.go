package usecase

import (
	"fmt"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
	"github.com/sirupsen/logrus"
)

// resolveTargetLang picks the best target language given (caller request,
// per-chat preference, global default). Caller's explicit choice always wins.
func (s *serviceTranslation) resolveTargetLang(deviceID, chatJID, requested string) string {
	if v := strings.ToLower(strings.TrimSpace(requested)); v != "" {
		return v
	}
	if chatJID != "" {
		if pref, err := s.translationRepo.GetChatPref(deviceID, chatJID); err == nil && pref != nil {
			if v := strings.ToLower(strings.TrimSpace(pref.TargetLang)); v != "" {
				return v
			}
		}
	}
	if v := strings.ToLower(strings.TrimSpace(config.TranslationDefaultTargetLang)); v != "" {
		return v
	}
	return "en"
}

// tryCacheLookup checks the persistent cache and returns (suggestions, true)
// on a hit, (nil, false) otherwise. Centralized so all of the cache-bypass
// + warn-and-fallback logic lives in one place.
func (s *serviceTranslation) tryCacheLookup(
	deviceID, chatJID, messageID, targetLang, sourceHash, providerKey string,
	forceRefresh bool,
) ([]domainTranslation.Suggestion, bool) {
	if forceRefresh {
		return nil, false
	}
	entry, err := s.translationRepo.GetCachedTranslation(deviceID, chatJID, messageID, targetLang, sourceHash, providerKey)
	if err != nil {
		logrus.WithError(err).Warn("translation: cache lookup failed; falling through to provider")
		return nil, false
	}
	if entry == nil {
		return nil, false
	}
	return entry.Suggestions, true
}

// persistCache writes the cache entry, swallowing errors at warn level —
// translation succeeded for the caller; failing the response over a cache
// write would punish the user for an internal hiccup.
func (s *serviceTranslation) persistCache(entry *domainTranslation.CachedTranslation) {
	if entry == nil || s.translationRepo == nil {
		return
	}
	if err := s.translationRepo.PutCachedTranslation(entry); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"chat_jid":   entry.ChatJID,
			"message_id": entry.MessageID,
		}).Warn("translation: failed to persist cache entry")
	}
}

// cacheProviderKey is what gets stamped on cache rows. It folds the prompt
// version into the key so a prompt rev (e.g. v1 -> v2) cleanly invalidates
// the existing cache without needing a schema change. RAG mode is also
// folded in so RAG-backed and system-context-backed translations don't
// collide in the cache.
func (s *serviceTranslation) cacheProviderKey() string {
	mode := "sys"
	if config.TranslationRAGEnabled && s.embedder != nil {
		mode = "rag"
	}
	return fmt.Sprintf("%s/%s/%s", s.provider.Name(), domainTranslation.PromptVersion, mode)
}

// computeExpiresAt converts the configured TTL into a unix timestamp; 0 means "no expiry".
func (s *serviceTranslation) computeExpiresAt() int64 {
	ttl := config.TranslationCacheTTLSeconds
	if ttl <= 0 {
		return 0
	}
	return s.now().Add(time.Duration(ttl) * time.Second).Unix()
}
