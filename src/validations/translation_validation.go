package validations

import (
	"context"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// applyTargetLangDefault sets TargetLang to the configured default when missing
// and lower-cases it so providers and cache keys see a normalized value.
func applyTargetLangDefault(target *string) {
	if target == nil {
		return
	}
	*target = strings.TrimSpace(strings.ToLower(*target))
	if *target == "" {
		*target = strings.TrimSpace(strings.ToLower(config.TranslationDefaultTargetLang))
	}
	if *target == "" {
		*target = "en"
	}
}

func ValidateTranslateMessage(ctx context.Context, request *domainTranslation.TranslateMessageRequest) error {
	applyTargetLangDefault(&request.TargetLang)
	request.SourceLang = strings.TrimSpace(strings.ToLower(request.SourceLang))

	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.ChatJID, validation.Required),
		validation.Field(&request.MessageID, validation.Required),
		validation.Field(&request.TargetLang, validation.Required, validation.Length(2, 16)),
		validation.Field(&request.SourceLang, validation.Length(0, 16)),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}
	return nil
}

func ValidateTranslateDraft(ctx context.Context, request *domainTranslation.TranslateDraftRequest) error {
	applyTargetLangDefault(&request.TargetLang)
	request.SourceLang = strings.TrimSpace(strings.ToLower(request.SourceLang))
	request.Text = strings.TrimSpace(request.Text)

	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.Text, validation.Required, validation.Length(1, 4096)),
		validation.Field(&request.TargetLang, validation.Required, validation.Length(2, 16)),
		validation.Field(&request.SourceLang, validation.Length(0, 16)),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}
	return nil
}


func ValidateGetChatPrefs(ctx context.Context, request *domainTranslation.GetChatPrefsRequest) error {
	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.ChatJID, validation.Required),
	)
	if err != nil {
		return pkgError.ValidationError(err.Error())
	}
	return nil
}

func ValidateSetChatPrefs(ctx context.Context, request *domainTranslation.SetChatPrefsRequest) error {
	// Reject empty bodies — partial-update semantics need at least one
	// field present so an accidental empty PUT can't silently no-op.
	if request.TargetLang == nil &&
		request.AutoTranslateInbound == nil &&
		request.AutoTranslateOutbound == nil &&
		request.TranslationOptIn == nil {
		return pkgError.ValidationError("at least one of target_lang, auto_translate_inbound, auto_translate_outbound, translation_opt_in must be set")
	}
	if request.TargetLang != nil {
		// Empty string is allowed and means "unset → use global default";
		// any non-empty value must look like a language code.
		v := strings.TrimSpace(strings.ToLower(*request.TargetLang))
		*request.TargetLang = v
		if v != "" && (len(v) < 2 || len(v) > 16) {
			return pkgError.ValidationError("target_lang must be 2-16 characters or empty")
		}
	}
	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.ChatJID, validation.Required),
	)
	if err != nil {
		return pkgError.ValidationError(err.Error())
	}
	return nil
}
