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
