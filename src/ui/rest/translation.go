package rest

import (
	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

// Translation wires HTTP endpoints for in-chat translation.
type Translation struct {
	Service domainTranslation.ITranslationUsecase
}

// InitRestTranslation registers translation routes on the device-scoped
// API group. Routes follow the existing message namespace
// (POST /message/:message_id/translate) so the feature lives next to
// other per-message actions (revoke, react, etc.).
func InitRestTranslation(app fiber.Router, service domainTranslation.ITranslationUsecase) Translation {
	rest := Translation{Service: service}

	// Translate an existing stored message identified by its ID.
	app.Post("/message/:message_id/translate", rest.TranslateMessage)
	// Translate a free-form draft (compose-assist flow).
	app.Post("/translate/draft", rest.TranslateDraft)
	// Per-chat translation preferences (target lang + auto-translate toggles).
	app.Get("/chat/:chat_jid/translation-prefs", rest.GetChatPrefs)
	app.Put("/chat/:chat_jid/translation-prefs", rest.SetChatPrefs)

	return rest
}

func (controller *Translation) TranslateMessage(c *fiber.Ctx) error {
	var request domainTranslation.TranslateMessageRequest

	// Body is optional — chat_jid/target_lang/source_lang/force_refresh travel
	// through it but the message_id always comes from the URL.
	_ = c.BodyParser(&request)
	request.MessageID = c.Params("message_id")

	response, err := controller.Service.TranslateMessage(
		whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c)),
		request,
	)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  fiber.StatusOK,
		Code:    "SUCCESS",
		Message: "Translation suggestions generated",
		Results: response,
	})
}

func (controller *Translation) TranslateDraft(c *fiber.Ctx) error {
	var request domainTranslation.TranslateDraftRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  fiber.StatusBadRequest,
			Code:    "BAD_REQUEST",
			Message: "Invalid request body",
			Results: nil,
		})
	}

	response, err := controller.Service.TranslateDraft(
		whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c)),
		request,
	)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  fiber.StatusOK,
		Code:    "SUCCESS",
		Message: "Draft translation suggestions generated",
		Results: response,
	})
}


func (controller *Translation) GetChatPrefs(c *fiber.Ctx) error {
	request := domainTranslation.GetChatPrefsRequest{
		ChatJID: c.Params("chat_jid"),
	}

	response, err := controller.Service.GetChatPrefs(
		whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c)),
		request,
	)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  fiber.StatusOK,
		Code:    "SUCCESS",
		Message: "Chat translation preferences",
		Results: response,
	})
}

func (controller *Translation) SetChatPrefs(c *fiber.Ctx) error {
	var request domainTranslation.SetChatPrefsRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  fiber.StatusBadRequest,
			Code:    "BAD_REQUEST",
			Message: "Invalid request body",
			Results: nil,
		})
	}
	// Path param is authoritative — body chat_jid (if any) is ignored.
	request.ChatJID = c.Params("chat_jid")

	response, err := controller.Service.SetChatPrefs(
		whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c)),
		request,
	)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  fiber.StatusOK,
		Code:    "SUCCESS",
		Message: "Chat translation preferences updated",
		Results: response,
	})
}
