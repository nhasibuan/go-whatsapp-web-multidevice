package usecase

import (
	"context"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/sirupsen/logrus"
)

// GetChatPrefs returns the persisted preferences plus the effective target
// language. A missing row resolves to defaults so the UI can render a clean
// panel without 404 handling — that's why this method always succeeds with
// an empty-but-valid response when the row doesn't exist yet.
func (s *serviceTranslation) GetChatPrefs(ctx context.Context, request domainTranslation.GetChatPrefsRequest) (response domainTranslation.GetChatPrefsResponse, err error) {
	if !config.TranslationEnabled {
		return response, pkgError.TranslationDisabledError("translation is disabled (set TRANSLATION_ENABLED=true to enable)")
	}
	if err = validations.ValidateGetChatPrefs(ctx, &request); err != nil {
		return response, err
	}
	deviceID := deviceIDFromContext(ctx)
	if deviceID == "" {
		return response, pkgError.TranslationDeviceMissingError("device identification required")
	}

	pref, err := s.translationRepo.GetChatPref(deviceID, request.ChatJID)
	if err != nil {
		logrus.WithError(err).WithField("chat_jid", request.ChatJID).Warn("translation: failed to read chat prefs")
		return response, err
	}

	response.ChatJID = request.ChatJID
	if pref != nil {
		// Map storage columns to the public field names.
		// (auto_translate -> auto_translate_inbound, translation_opt_in -> auto_translate_outbound)
		response.TargetLang = pref.TargetLang
		response.AutoTranslateInbound = pref.AutoTranslate
		response.AutoTranslateOutbound = pref.TranslationOptIn
		response.TranslationOptIn = pref.TranslationOptIn
		response.UpdatedAt = pref.UpdatedAt
	}
	response.EffectiveTargetLang = s.resolveTargetLang(deviceID, request.ChatJID, "")

	return response, nil
}

// SetChatPrefs applies a partial update. Pointer fields on the request let
// clients flip a single flag without re-sending the whole record. The stored
// row is upserted and the resulting state is returned, including the
// effective target language.
func (s *serviceTranslation) SetChatPrefs(ctx context.Context, request domainTranslation.SetChatPrefsRequest) (response domainTranslation.GetChatPrefsResponse, err error) {
	if !config.TranslationEnabled {
		return response, pkgError.TranslationDisabledError("translation is disabled (set TRANSLATION_ENABLED=true to enable)")
	}
	if err = validations.ValidateSetChatPrefs(ctx, &request); err != nil {
		return response, err
	}
	deviceID := deviceIDFromContext(ctx)
	if deviceID == "" {
		return response, pkgError.TranslationDeviceMissingError("device identification required")
	}

	// Read-modify-write so partial updates preserve fields the client
	// didn't send. Missing rows start from the zero value.
	current, err := s.translationRepo.GetChatPref(deviceID, request.ChatJID)
	if err != nil {
		logrus.WithError(err).WithField("chat_jid", request.ChatJID).Warn("translation: failed to read chat prefs for update")
		return response, err
	}
	pref := domainTranslation.ChatTranslationPref{
		DeviceID: deviceID,
		ChatJID:  request.ChatJID,
	}
	if current != nil {
		pref = *current
		pref.DeviceID = deviceID
		pref.ChatJID = request.ChatJID
	}

	if request.TargetLang != nil {
		pref.TargetLang = strings.ToLower(strings.TrimSpace(*request.TargetLang))
	}
	if request.AutoTranslateInbound != nil {
		pref.AutoTranslate = *request.AutoTranslateInbound
	}
	// AutoTranslateOutbound and the legacy TranslationOptIn share the same
	// storage column; whichever the client sent wins, with TranslationOptIn
	// taking precedence when both are set so existing callers keep working.
	if request.AutoTranslateOutbound != nil {
		pref.TranslationOptIn = *request.AutoTranslateOutbound
	}
	if request.TranslationOptIn != nil {
		pref.TranslationOptIn = *request.TranslationOptIn
	}

	if err := s.translationRepo.SetChatPref(&pref); err != nil {
		logrus.WithError(err).WithField("chat_jid", request.ChatJID).Error("translation: failed to write chat prefs")
		return response, err
	}

	response.ChatJID = request.ChatJID
	response.TargetLang = pref.TargetLang
	response.AutoTranslateInbound = pref.AutoTranslate
	response.AutoTranslateOutbound = pref.TranslationOptIn
	response.TranslationOptIn = pref.TranslationOptIn
	response.UpdatedAt = pref.UpdatedAt
	response.EffectiveTargetLang = s.resolveTargetLang(deviceID, request.ChatJID, "")

	logrus.WithFields(logrus.Fields{
		"chat_jid":              request.ChatJID,
		"effective_target_lang": response.EffectiveTargetLang,
		"auto_inbound":          response.AutoTranslateInbound,
		"auto_outbound":         response.AutoTranslateOutbound,
	}).Info("translation: chat prefs updated")

	return response, nil
}
