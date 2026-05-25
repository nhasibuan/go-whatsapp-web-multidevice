package validations

import (
	"context"

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
