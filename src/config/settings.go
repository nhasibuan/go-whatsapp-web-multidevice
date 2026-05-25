package config

import (
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
)

var (
	AppVersion             = "v8.3.1"
	AppPort                = "3000"
	AppHost                = "0.0.0.0"
	AppDebug               = false
	AppOs                  = "AldinoKemal"
	AppPlatform            = waCompanionReg.DeviceProps_PlatformType(1)
	AppBasicAuthCredential []string
	AppBasePath            = ""
	AppTrustedProxies      []string // Trusted proxy IP ranges (e.g., "0.0.0.0/0" for all, or specific CIDRs)

	McpPort = "8080"
	McpHost = "localhost"

	PathQrCode    = "statics/qrcode"
	PathSendItems = "statics/senditems"
	PathMedia     = "statics/media"
	PathStorages  = "storages"

	DBURI     = "file:storages/whatsapp.db?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000"
	DBKeysURI = ""

	WhatsappAutoReplyMessage          string
	WhatsappAutoMarkRead              = false // Auto-mark incoming messages as read
	WhatsappAutoDownloadMedia         = true  // Auto-download media from incoming messages
	WhatsappWebhook                   []string
	WhatsappWebhookSecret             = "secret"
	WhatsappWebhookInsecureSkipVerify = false          // Skip TLS certificate verification for webhooks (insecure)
	WhatsappWebhookEvents             []string         // Whitelist of events to forward to webhook (empty = all events)
	WhatsappAutoRejectCall                     = false // Auto-reject incoming calls
	WhatsappLogLevel                           = "ERROR"
	WhatsappSettingMaxImageSize       int64    = 20000000  // 20MB
	WhatsappSettingMaxFileSize        int64    = 50000000  // 50MB
	WhatsappSettingMaxVideoSize       int64    = 100000000 // 100MB
	WhatsappSettingMaxDownloadSize    int64    = 500000000 // 500MB
	WhatsappTypeUser                           = "@s.whatsapp.net"
	WhatsappTypeGroup                          = "@g.us"
	WhatsappTypeLid                            = "@lid"
	WhatsappAccountValidation                  = true
	WhatsappPresenceOnConnect                  = "unavailable" // Presence to send on connect: "available", "unavailable", or "none"

	ChatStorageURI               = "file:storages/chatstorage.db"
	ChatStorageEnableForeignKeys = true
	ChatStorageEnableWAL         = true

	ChatwootEnabled   = false
	ChatwootURL       = ""
	ChatwootAPIToken  = ""
	ChatwootAccountID = 0
	ChatwootInboxID   = 0
	ChatwootDeviceID  = "" // Device ID for outbound messages (required for multi-device)

	// Chatwoot History Sync settings
	ChatwootImportMessages          = false // Enable message history import to Chatwoot
	ChatwootDaysLimitImportMessages = 3     // Days of history to import (default: 3)

	// Translation feature (Phase 1 MVP, OpenAI provider)
	// Disabled by default — enable with TRANSLATION_ENABLED=true and provide
	// an API key. When disabled, the /translate/* endpoints return a clear
	// "feature disabled" error instead of failing at boot.
	TranslationEnabled           = false
	TranslationProvider          = "openai"     // currently the only built-in provider
	TranslationOpenAIAPIKey      = ""           // required when TranslationProvider == "openai"
	TranslationOpenAIModel       = "gpt-4o-mini"
	TranslationOpenAIBaseURL     = ""           // optional override for OpenAI-compatible endpoints
	TranslationDefaultTargetLang = "en"         // BCP-47 language tag
	TranslationContextWindow     = 20           // recent messages used to condition the translation
	TranslationCacheEnabled      = true         // dedupe identical requests via the message_translations table
	TranslationTimeoutSeconds    = 30           // HTTP timeout for the provider call

	// Translation RAG (Phase 3) — semantic retrieval over per-chat history
	// and the user's own outbound messages. Off by default; turning it on
	// requires an embeddings-capable API key (the same OpenAI key works).
	//
	// When enabled, each translate call:
	//   1. Embeds the source text (one extra ~$0.00001 round-trip),
	//   2. Retrieves top-K from a bounded per-chat pool and a user-style pool,
	//   3. Lazily backfills any unindexed messages in the chat in background.
	//
	// Falls back gracefully to the Phase 1 system-context path when retrieval
	// returns empty (e.g. brand-new chat) or when any RAG dependency errors.
	TranslationRAGEnabled        = false
	TranslationEmbeddingModel    = "text-embedding-3-small" // 1536-D, cost-effective default
	TranslationRAGPerChatPool    = 200                      // recent messages from the same chat scored per call
	TranslationRAGStylePool      = 500                      // recent user outbound messages scored per call (across all chats)
	TranslationRAGPerChatK       = 8                        // top-K per-chat exemplars fed to the prompt
	TranslationRAGStyleK         = 4                        // top-K user-style exemplars fed to the prompt
	TranslationRAGBackfillLimit  = 100                      // max messages embedded per lazy backfill burst
	TranslationRAGBackfillBatch  = 32                       // messages embedded per OpenAI batch call
)
