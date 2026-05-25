package validations

import (
	"context"
	"strings"

	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// ValidateTranslateMessage validates a request to translate a stored message.
// target_lang is optional at this layer because the usecase falls back to
// per-chat / global defaults; the only hard requirement is message_id.
func ValidateTranslateMessage(ctx context.Context, request *domainTranslation.TranslateMessageRequest) error {
	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.MessageID, validation.Required),
		validation.Field(&request.TargetLang, validation.Length(0, 16)),
		validation.Field(&request.SourceLang, validation.Length(0, 16)),
	)
	if err != nil {
		return pkgError.ValidationError(err.Error())
	}
	return nil
}

// ValidateTranslateDraft validates a draft (compose-assist) translation request.
func ValidateTranslateDraft(ctx context.Context, request *domainTranslation.TranslateDraftRequest) error {
	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.Text, validation.Required, validation.Length(1, 4096)),
		validation.Field(&request.TargetLang, validation.Length(0, 16)),
		validation.Field(&request.SourceLang, validation.Length(0, 16)),
	)
	if err != nil {
		return pkgError.ValidationError(err.Error())
	}
	return nil
}

// ValidateGetChatPref validates a request to read per-chat translation prefs.
func ValidateGetChatPref(ctx context.Context, request *domainTranslation.GetChatPrefRequest) error {
	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.ChatJID, validation.Required),
	)
	if err != nil {
		return pkgError.ValidationError(err.Error())
	}
	return nil
}

// ValidateSetChatPref validates an upsert of per-chat translation prefs.
// At least one mutable field must be provided so an empty PUT body can't
// silently no-op the call.
func ValidateSetChatPref(ctx context.Context, request *domainTranslation.SetChatPrefRequest) error {
	if request == nil {
		return pkgError.ValidationError("nil request")
	}
	if strings.TrimSpace(request.ChatJID) == "" {
		return pkgError.ValidationError("chat_jid: cannot be blank")
	}
	if request.TargetLang == nil && request.AutoTranslateInbound == nil && request.AutoTranslateOutbound == nil {
		return pkgError.ValidationError("at least one of target_lang, auto_translate_inbound, auto_translate_outbound must be provided")
	}
	if request.TargetLang != nil {
		if l := len(*request.TargetLang); l > 16 {
			return pkgError.ValidationError("target_lang: max 16 characters")
		}
	}
	return nil
}
