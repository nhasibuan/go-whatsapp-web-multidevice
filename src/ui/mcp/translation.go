package mcp

import (
	"context"
	"fmt"
	"strings"

	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
	mcpHelpers "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/helpers"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// TranslationHandler exposes the in-chat translation feature over MCP so AI
// agents can use the same 3-suggestion pipeline as the REST API.
type TranslationHandler struct {
	translationService domainTranslation.ITranslationUsecase
}

// InitMcpTranslation wires the translation handler. Pass nil when the feature
// is disabled — the tools then surface a clear error to the caller.
func InitMcpTranslation(translationService domainTranslation.ITranslationUsecase) *TranslationHandler {
	return &TranslationHandler{translationService: translationService}
}

// AddTranslationTools registers translate_message and translate_draft tools.
func (h *TranslationHandler) AddTranslationTools(mcpServer *server.MCPServer) {
	if h == nil || mcpServer == nil {
		return
	}
	mcpServer.AddTool(h.toolTranslateMessage(), h.handleTranslateMessage)
	mcpServer.AddTool(h.toolTranslateDraft(), h.handleTranslateDraft)
}

// --- translate_message --------------------------------------------------

func (h *TranslationHandler) toolTranslateMessage() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_translate_message",
		mcp.WithDescription(
			"Translate a stored WhatsApp message into a target language. Returns "+
				"three context-aware suggestions: 'literal', 'natural', and "+
				"'tone_matched' (which is conditioned on the recent thread context).",
		),
		mcp.WithTitleAnnotation("Translate Message"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("message_id",
			mcp.Description("ID of the stored message to translate."),
			mcp.Required(),
		),
		mcp.WithString("chat_jid",
			mcp.Description("Optional. When provided, the message must belong to this chat."),
		),
		mcp.WithString("target_lang",
			mcp.Description("BCP-47 target language (e.g., 'en', 'id'). Falls back to the configured default."),
		),
		mcp.WithString("source_lang",
			mcp.Description("Optional source language hint. Auto-detected when omitted."),
		),
	)
}

func (h *TranslationHandler) handleTranslateMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if h == nil || h.translationService == nil {
		return nil, fmt.Errorf("translation feature is not configured on this server")
	}

	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	messageID, err := request.RequireString("message_id")
	if err != nil {
		return nil, err
	}

	req := domainTranslation.TranslateMessageRequest{
		MessageID:  messageID,
		ChatJID:    strings.TrimSpace(request.GetString("chat_jid", "")),
		TargetLang: strings.TrimSpace(request.GetString("target_lang", "")),
		SourceLang: strings.TrimSpace(request.GetString("source_lang", "")),
	}

	resp, err := h.translationService.TranslateMessage(ctx, req)
	if err != nil {
		return nil, err
	}

	fallback := summarizeTranslation(resp)
	return mcp.NewToolResultStructured(resp, fallback), nil
}

// --- translate_draft ----------------------------------------------------

func (h *TranslationHandler) toolTranslateDraft() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_translate_draft",
		mcp.WithDescription(
			"Translate arbitrary user-supplied text (compose-assist) into a target "+
				"language. When chat_jid is provided, the recent messages of that chat "+
				"are used as context so the tone-matched suggestion fits the thread.",
		),
		mcp.WithTitleAnnotation("Translate Draft"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("text",
			mcp.Description("Source text to translate (1–4096 characters)."),
			mcp.Required(),
		),
		mcp.WithString("chat_jid",
			mcp.Description("Optional. Provides conversation context for the tone-matched suggestion."),
		),
		mcp.WithString("target_lang",
			mcp.Description("BCP-47 target language (e.g., 'en', 'id'). Falls back to the configured default."),
		),
		mcp.WithString("source_lang",
			mcp.Description("Optional source language hint. Auto-detected when omitted."),
		),
	)
}

func (h *TranslationHandler) handleTranslateDraft(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if h == nil || h.translationService == nil {
		return nil, fmt.Errorf("translation feature is not configured on this server")
	}

	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	text, err := request.RequireString("text")
	if err != nil {
		return nil, err
	}

	req := domainTranslation.TranslateDraftRequest{
		Text:       text,
		ChatJID:    strings.TrimSpace(request.GetString("chat_jid", "")),
		TargetLang: strings.TrimSpace(request.GetString("target_lang", "")),
		SourceLang: strings.TrimSpace(request.GetString("source_lang", "")),
	}

	resp, err := h.translationService.TranslateDraft(ctx, req)
	if err != nil {
		return nil, err
	}

	fallback := summarizeTranslation(resp)
	return mcp.NewToolResultStructured(resp, fallback), nil
}

// summarizeTranslation builds a short text fallback so MCP clients that don't
// render structured payloads still see something useful.
func summarizeTranslation(resp domainTranslation.TranslateResponse) string {
	cache := ""
	if resp.CacheHit {
		cache = " (cached)"
	}
	return fmt.Sprintf(
		"%d translation(s) into %s via %s%s",
		len(resp.Suggestions),
		resp.TargetLang,
		resp.Provider,
		cache,
	)
}
