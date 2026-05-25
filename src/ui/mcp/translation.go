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

// TranslationHandler exposes the translation usecase as MCP tools so AI
// agents using this server get the same 3-suggestion pipeline (literal /
// natural / tone-matched) the REST API surface and Vue UI use.
type TranslationHandler struct {
	service domainTranslation.ITranslationUsecase
}

// InitMcpTranslation constructs the handler. Wired in cmd/mcp.go.
func InitMcpTranslation(service domainTranslation.ITranslationUsecase) *TranslationHandler {
	return &TranslationHandler{service: service}
}

func (h *TranslationHandler) AddTranslationTools(mcpServer *server.MCPServer) {
	mcpServer.AddTool(h.toolTranslateMessage(), h.handleTranslateMessage)
	mcpServer.AddTool(h.toolTranslateDraft(), h.handleTranslateDraft)
}

func (h *TranslationHandler) toolTranslateMessage() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_translate_message",
		mcp.WithDescription(
			"Translate a stored WhatsApp message into a target language and return three context-aware "+
				"suggestions: literal (close word-for-word), natural (idiomatic), and tone-matched "+
				"(mirrors the recent thread's register and slang). Recent messages from the same chat "+
				"are used as context. The result is cached per (message, target_lang, provider); set "+
				"force_refresh=true to bypass the cache. Requires TRANSLATION_ENABLED on the server."),
		mcp.WithTitleAnnotation("Translate WhatsApp Message"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("chat_jid",
			mcp.Required(),
			mcp.Description("Chat JID the message belongs to (e.g. 628123456789@s.whatsapp.net)."),
		),
		mcp.WithString("message_id",
			mcp.Required(),
			mcp.Description("ID of the stored message to translate."),
		),
		mcp.WithString("target_lang",
			mcp.Description(
				"Target language code (e.g. en, id, ja). When omitted, the per-chat preference is "+
					"used and falls back to the server's TRANSLATION_DEFAULT_TARGET_LANG."),
		),
		mcp.WithString("source_lang",
			mcp.Description("Optional source language hint; auto-detected when empty."),
		),
		mcp.WithBoolean("force_refresh",
			mcp.Description("If true, skip the cache and ask the provider for a fresh result."),
			mcp.DefaultBool(false),
		),
	)
}

func (h *TranslationHandler) handleTranslateMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	chatJID, err := request.RequireString("chat_jid")
	if err != nil {
		return nil, err
	}
	messageID, err := request.RequireString("message_id")
	if err != nil {
		return nil, err
	}

	forceRefresh := false
	if args := request.GetArguments(); args != nil {
		if v, ok := args["force_refresh"]; ok {
			parsed, err := toBool(v)
			if err != nil {
				return nil, err
			}
			forceRefresh = parsed
		}
	}

	resp, err := h.service.TranslateMessage(ctx, domainTranslation.TranslateMessageRequest{
		ChatJID:      chatJID,
		MessageID:    messageID,
		TargetLang:   request.GetString("target_lang", ""),
		SourceLang:   request.GetString("source_lang", ""),
		ForceRefresh: forceRefresh,
	})
	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultStructured(resp, summarizeMessageResp(resp)), nil
}

func (h *TranslationHandler) toolTranslateDraft() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_translate_draft",
		mcp.WithDescription(
			"Translate arbitrary draft text into the target language and return three context-aware "+
				"suggestions: literal, natural, and tone-matched. When chat_jid is supplied, recent "+
				"messages from that chat are used as context to keep tone consistent with the thread. "+
				"Use this for the compose-assist flow — pick one suggestion and feed it into "+
				"whatsapp_send_text. Requires TRANSLATION_ENABLED on the server."),
		mcp.WithTitleAnnotation("Translate WhatsApp Draft"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("text",
			mcp.Required(),
			mcp.Description("Draft text to translate."),
		),
		mcp.WithString("target_lang",
			mcp.Required(),
			mcp.Description("Target language code (e.g. en, id, ja)."),
		),
		mcp.WithString("source_lang",
			mcp.Description("Optional source language hint; auto-detected when empty."),
		),
		mcp.WithString("chat_jid",
			mcp.Description("Optional chat JID; when provided, recent messages are used as context for tone matching."),
		),
		mcp.WithBoolean("force_refresh",
			mcp.Description("If true, skip the cache and ask the provider for a fresh result."),
			mcp.DefaultBool(false),
		),
	)
}

func (h *TranslationHandler) handleTranslateDraft(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	text, err := request.RequireString("text")
	if err != nil {
		return nil, err
	}
	targetLang, err := request.RequireString("target_lang")
	if err != nil {
		return nil, err
	}

	forceRefresh := false
	if args := request.GetArguments(); args != nil {
		if v, ok := args["force_refresh"]; ok {
			parsed, err := toBool(v)
			if err != nil {
				return nil, err
			}
			forceRefresh = parsed
		}
	}

	resp, err := h.service.TranslateDraft(ctx, domainTranslation.TranslateDraftRequest{
		Text:         text,
		TargetLang:   targetLang,
		SourceLang:   request.GetString("source_lang", ""),
		ChatJID:      request.GetString("chat_jid", ""),
		ForceRefresh: forceRefresh,
	})
	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultStructured(resp, summarizeDraftResp(resp)), nil
}

// summarizeMessageResp builds a human-readable fallback for MCP clients that
// don't render structured payloads. It surfaces the variant texts inline so a
// chat-style client still sees usable output.
func summarizeMessageResp(resp domainTranslation.TranslateMessageResponse) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Translated message %s -> %s (provider: %s",
		resp.MessageID, resp.TargetLang, resp.Provider)
	if resp.Cached {
		sb.WriteString(", cached")
	}
	sb.WriteString("):\n")
	appendSuggestionLines(&sb, resp.Suggestions)
	return sb.String()
}

func summarizeDraftResp(resp domainTranslation.TranslateDraftResponse) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Translated draft -> %s (provider: %s", resp.TargetLang, resp.Provider)
	if resp.Cached {
		sb.WriteString(", cached")
	}
	sb.WriteString("):\n")
	appendSuggestionLines(&sb, resp.Suggestions)
	return sb.String()
}

func appendSuggestionLines(sb *strings.Builder, suggestions []domainTranslation.Suggestion) {
	if len(suggestions) == 0 {
		sb.WriteString("(no suggestions returned)")
		return
	}
	for _, s := range suggestions {
		fmt.Fprintf(sb, "- [%s] %s", s.Variant, s.Text)
		if strings.TrimSpace(s.Rationale) != "" {
			fmt.Fprintf(sb, " — %s", s.Rationale)
		}
		sb.WriteString("\n")
	}
}


// AddPrefsTools registers the per-chat preference tools. The usecase rejects
// calls when TRANSLATION_ENABLED=false with a clear error; we surface that
// as the tool error so the agent gets actionable feedback rather than a panic.
func (h *TranslationHandler) AddPrefsTools(mcpServer *server.MCPServer) {
	mcpServer.AddTool(h.toolGetChatPrefs(), h.handleGetChatPrefs)
	mcpServer.AddTool(h.toolSetChatPrefs(), h.handleSetChatPrefs)
}

func (h *TranslationHandler) toolGetChatPrefs() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_get_chat_translation_prefs",
		mcp.WithDescription(
			"Read per-chat translation preferences (target language, auto-translate toggles). "+
				"Always returns a row even when none has been saved yet — missing rows resolve to "+
				"defaults so callers can render UI without 404 handling. The effective_target_lang "+
				"field shows the value actually used (per-chat override → server default)."),
		mcp.WithTitleAnnotation("Get Chat Translation Preferences"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("chat_jid",
			mcp.Required(),
			mcp.Description("Chat JID to read preferences for (e.g. 628123456789@s.whatsapp.net)."),
		),
	)
}

func (h *TranslationHandler) handleGetChatPrefs(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}
	chatJID, err := request.RequireString("chat_jid")
	if err != nil {
		return nil, err
	}

	resp, err := h.service.GetChatPrefs(ctx, domainTranslation.GetChatPrefsRequest{ChatJID: chatJID})
	if err != nil {
		return nil, err
	}

	fallback := fmt.Sprintf(
		"Chat %s: target_lang=%q (effective %q), auto_inbound=%t, auto_outbound=%t",
		resp.ChatJID, resp.TargetLang, resp.EffectiveTargetLang,
		resp.AutoTranslateInbound, resp.AutoTranslateOutbound,
	)
	return mcp.NewToolResultStructured(resp, fallback), nil
}

func (h *TranslationHandler) toolSetChatPrefs() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_set_chat_translation_prefs",
		mcp.WithDescription(
			"Update per-chat translation preferences. Send only the fields you want to change — "+
				"omitted fields keep their current value. At least one field must be supplied. "+
				"Returns the resulting persisted state including the effective_target_lang."),
		mcp.WithTitleAnnotation("Set Chat Translation Preferences"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("chat_jid",
			mcp.Required(),
			mcp.Description("Chat JID whose preferences are being updated."),
		),
		mcp.WithString("target_lang",
			mcp.Description("Target language code (e.g. en, id, ja). Send an empty string to clear and fall back to the global default."),
		),
		mcp.WithBoolean("auto_translate_inbound",
			mcp.Description("Auto-translate inbound messages on receipt."),
		),
		mcp.WithBoolean("auto_translate_outbound",
			mcp.Description("Auto-translate outbound messages on send (reserved; UI hook ships with compose-assist)."),
		),
	)
}

func (h *TranslationHandler) handleSetChatPrefs(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}
	chatJID, err := request.RequireString("chat_jid")
	if err != nil {
		return nil, err
	}

	args := request.GetArguments()
	if args == nil {
		// Defensive: rare in practice, but the validator would reject this
		// anyway and we want a clearer error than a nil-deref panic.
		return nil, fmt.Errorf("at least one preference field must be supplied")
	}

	upd := domainTranslation.SetChatPrefsRequest{ChatJID: chatJID}

	// Pointer-or-nil semantics travel through MCP as "argument present or
	// absent". Each block below only sets the pointer when the agent
	// actually included the field, mirroring the REST partial-update contract.
	if v, ok := args["target_lang"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("target_lang must be a string")
		}
		upd.TargetLang = &s
	}
	if v, ok := args["auto_translate_inbound"]; ok {
		parsed, err := toBool(v)
		if err != nil {
			return nil, err
		}
		upd.AutoTranslateInbound = &parsed
	}
	if v, ok := args["auto_translate_outbound"]; ok {
		parsed, err := toBool(v)
		if err != nil {
			return nil, err
		}
		upd.AutoTranslateOutbound = &parsed
	}

	resp, err := h.service.SetChatPrefs(ctx, upd)
	if err != nil {
		return nil, err
	}

	fallback := fmt.Sprintf(
		"Updated %s: target_lang=%q (effective %q), auto_inbound=%t, auto_outbound=%t",
		resp.ChatJID, resp.TargetLang, resp.EffectiveTargetLang,
		resp.AutoTranslateInbound, resp.AutoTranslateOutbound,
	)
	return mcp.NewToolResultStructured(resp, fallback), nil
}
