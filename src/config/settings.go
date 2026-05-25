package config

import (
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
)

var (
	AppVersion             = "v8.6.0"
	AppPort                = "3000"
	AppHost                = "0.0.0.0"
	AppDebug               = false
	AppOs                  = "GOWA"
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

	DBURI     = "file:storages/whatsapp.db"
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

	// In-chat translation settings.
	// Defaults are tuned so the feature is OFF unless the operator opts in.
	// When enabled but no provider/key is configured, the mock provider is
	// used so the UI keeps working without external calls.
	TranslationEnabled           = false
	TranslationProvider          = "openai" // "openai" | "mock"
	TranslationOpenAIAPIKey      = ""       // Required for the openai provider
	TranslationOpenAIBaseURL     = ""       // Optional override (e.g. proxy or Azure endpoint)
	TranslationOpenAIModel       = ""       // e.g. "gpt-4o-mini" (provider default if empty)
	TranslationDefaultTargetLang = "en"     // BCP-47-ish, e.g. "en", "id", "ja"
	TranslationContextWindow     = 20       // Recent messages used as context (0 disables)
	TranslationCacheTTLSeconds   = 86400    // 24h; 0 disables persistence-time TTL
	TranslationRequestTimeoutSec = 30       // Per-call provider timeout in seconds
	TranslationRAGEnabled        = false    // Phase 3 — when true, retrieval-augmented context replaces system context.

	// RAG embedding model. Defaults to OpenAI's text-embedding-3-small —
	// 1536 dimensions, low cost (~$0.000003/call), high enough quality for
	// in-chat retrieval. The embedding API key falls back to TranslationOpenAIAPIKey.
	TranslationOpenAIEmbeddingModel  = "text-embedding-3-small"
	TranslationOpenAIEmbeddingAPIKey = ""
)
