package rest

import (
	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type Translation struct {
	Service domainTranslation.ITranslationUsecase
}

// InitRestTranslation registers the translation endpoints on a device-scoped
// router (same group as the other chat/message routes).
func InitRestTranslation(app fiber.Router, service domainTranslation.ITranslationUsecase) Translation {
	rest := Translation{Service: service}

	// Translate a stored message by ID. message_id is in the path so this
	// composes naturally with the existing /message/:message_id/* family.
	app.Post("/message/:message_id/translate", rest.TranslateMessage)

	// Translate arbitrary draft text (used by compose-assist before send).
	app.Post("/translate/draft", rest.TranslateDraft)

	// Per-chat translation preferences. The chat JID lives in the path so it
	// composes naturally with the existing /chat/:chat_jid/* family.
	app.Get("/chat/:chat_jid/translation-prefs", rest.GetChatTranslationPrefs)
	app.Put("/chat/:chat_jid/translation-prefs", rest.SetChatTranslationPrefs)

	return rest
}

// TranslateMessage handles POST /message/:message_id/translate.
//
// Body (JSON, all optional):
//
//	{
//	  "chat_jid":   "6289...@s.whatsapp.net",
//	  "target_lang": "en",
//	  "source_lang": "id"
//	}
func (controller *Translation) TranslateMessage(c *fiber.Ctx) error {
	var request domainTranslation.TranslateMessageRequest
	if err := c.BodyParser(&request); err != nil && err.Error() != "" {
		// BodyParser tolerates empty bodies; only surface real parse failures.
		// Fall through — message_id from path is sufficient for the minimal call.
	}
	request.MessageID = c.Params("message_id")

	response, err := controller.Service.TranslateMessage(
		whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c)),
		request,
	)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Translation generated successfully",
		Results: response,
	})
}

// TranslateDraft handles POST /translate/draft.
//
// Body (JSON):
//
//	{
//	  "text":        "Halo apa kabar?",
//	  "chat_jid":    "6289...@s.whatsapp.net",   // optional, enables tone-match
//	  "target_lang": "en",                        // optional
//	  "source_lang": "id"                         // optional, autodetect if empty
//	}
func (controller *Translation) TranslateDraft(c *fiber.Ctx) error {
	var request domainTranslation.TranslateDraftRequest
	utils.PanicIfNeeded(c.BodyParser(&request))

	response, err := controller.Service.TranslateDraft(
		whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c)),
		request,
	)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Translation generated successfully",
		Results: response,
	})
}


// GetChatTranslationPrefs handles GET /chat/:chat_jid/translation-prefs.
//
// Always returns 200 with the effective target language. When no per-chat row
// exists yet, target_lang is empty and effective_target_lang reflects the
// global default — the UI uses this to render placeholder values.
func (controller *Translation) GetChatTranslationPrefs(c *fiber.Ctx) error {
	request := domainTranslation.GetChatPrefRequest{
		ChatJID: c.Params("chat_jid"),
	}

	response, err := controller.Service.GetChatPref(
		whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c)),
		request,
	)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Chat translation preferences",
		Results: response,
	})
}

// SetChatTranslationPrefs handles PUT /chat/:chat_jid/translation-prefs.
//
// Body (JSON, all fields optional but at least one must be present):
//
//	{
//	  "target_lang": "id",
//	  "auto_translate_inbound": true,
//	  "auto_translate_outbound": false
//	}
//
// Nil/missing fields are left unchanged so a client can flip a single flag
// without re-sending the whole record.
func (controller *Translation) SetChatTranslationPrefs(c *fiber.Ctx) error {
	var request domainTranslation.SetChatPrefRequest
	utils.PanicIfNeeded(c.BodyParser(&request))
	request.ChatJID = c.Params("chat_jid")

	response, err := controller.Service.SetChatPref(
		whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c)),
		request,
	)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Chat translation preferences updated",
		Results: response,
	})
}
