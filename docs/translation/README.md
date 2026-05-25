# In‑Chat Translation — Feature Documentation

A single document that covers the product, the architecture, every file that ships with the feature, and a hands‑on user guide. Anything not in this file is incidental scaffolding inherited from the host project.

The feature is implemented entirely as an additive layer on top of `aldinokemal/go-whatsapp-web-multidevice` — no main‑branch code is rewritten and no existing schemas are altered.

---

## Section 1 — Product Requirements (PRD)

### 1.1 Problem statement

WhatsApp Web users routinely correspond across language barriers. Generic machine translation produces text that is grammatically correct but tonally flat: it doesn't know whether this thread is a casual chat with a friend or a formal business reply, and it doesn't remember the user's own writing voice. The result is messages that read like a translator, not like the user.

### 1.2 Goals

- Inside the existing WhatsApp Web UI, every text message exposes a one‑click translate action that returns three context‑aware suggestions: literal, natural, and tone‑matched.
- Compose path: when the user writes a reply in their own language, "Translate before send" produces three candidates in the recipient's language and replaces the typed text with the chosen one before sending.
- Per‑chat preferences: each conversation can have its own target language and an "auto‑translate inbound" toggle so the user doesn't reset the picker every time.
- All capabilities are also exposed as MCP tools so AI agents using this server reach the same pipeline.

### 1.3 Non‑goals

- Real‑time per‑keystroke translation (debounced batch only).
- Generating audio, image, or video translations.
- Acting as a public translation API for third parties — every call is device‑scoped.

### 1.4 Functional requirements

| Surface | Behaviour |
|---|---|
| Message bubble | Globe icon opens an inline panel with three suggestions, copy buttons, provider/cache chips. |
| Auto‑translate inbound | When enabled, after `loadMessages` succeeds the UI prefetches the natural variant for inbound text bubbles and renders it under each bubble. |
| Compose draft | Toggle + dropdown + "Get 3 suggestions" button + "Use this" picker that swaps the textarea content. |
| Per‑chat prefs | Inline panel with target‑lang dropdown ("Use global default" sentinel + 12 named languages), inbound/outbound toggles, effective‑lang chip in the modal header. |
| REST API | `POST /message/{id}/translate`, `POST /translate/draft`, `GET/PUT /chat/{jid}/translation-prefs`. |
| MCP tools | `whatsapp_translate_message`, `whatsapp_translate_draft`, `whatsapp_get_chat_translation_prefs`, `whatsapp_set_chat_translation_prefs`. |

### 1.5 Non‑functional requirements

- **Device scoped.** Every translation, cache row, and embedding is keyed by `device_id`. Cross‑device leak attempts return 404.
- **Graceful degradation.** When `TRANSLATION_ENABLED=false`, the feature is invisible. When enabled but missing an API key, a built‑in mock provider keeps the UI rendering. RAG failures fall back to system context cleanly.
- **Cache isolation.** Cache rows are keyed by `(provider, prompt_version, mode)` so a prompt change or a switch between system‑context and RAG mode never serves stale results.
- **No new Go dependencies.** Uses standard library + the project's existing toolkit (`gofiber`, `mark3labs/mcp-go`, `ozzo-validation`, `logrus`).

### 1.6 Configuration

```env
# Core
TRANSLATION_ENABLED=true
TRANSLATION_PROVIDER=openai                # openai | mock
TRANSLATION_OPENAI_API_KEY=sk-...
TRANSLATION_OPENAI_BASE_URL=               # optional proxy / Azure endpoint
TRANSLATION_OPENAI_MODEL=gpt-4o-mini
TRANSLATION_DEFAULT_TARGET_LANG=en
TRANSLATION_CONTEXT_WINDOW=20              # 0 disables system context
TRANSLATION_CACHE_TTL=86400                # seconds; 0 disables expiry
TRANSLATION_REQUEST_TIMEOUT_SEC=30

# Phase 3 — RAG
TRANSLATION_RAG_ENABLED=false
TRANSLATION_OPENAI_EMBEDDING_MODEL=text-embedding-3-small
TRANSLATION_OPENAI_EMBEDDING_API_KEY=      # falls back to TRANSLATION_OPENAI_API_KEY when empty
```

The same flags exist as CLI options on `rest` and `mcp` subcommands (`--translation-enabled`, `--translation-openai-api-key`, etc.).

### 1.7 Success criteria

- Build green: `go build ./...` and `go vet ./...` clean across all platforms.
- Translation tests pass: `go test ./usecase ./infrastructure/translation`.
- A user with a valid OpenAI key can flip `TRANSLATION_ENABLED=true`, open a chat, click the globe icon on any message, and see three suggestions within ~1 second on a warm cache.

---

## Section 2 — Blueprint

### 2.1 Architectural layering

The feature follows the host project's clean‑architecture convention: `domains/` define contracts, `usecase/` orchestrates, `infrastructure/` wraps adapters, `ui/` exposes the surface, `views/` is the Vue front‑end.

```
┌───────────────────────── views/components ─────────────────────────┐
│ ChatMessages.js · SendMessage.js                                   │
│           ↓ uses                                                   │
│ generic/TranslationApi.js · TranslationLanguages.js                │
│ generic/translationMixin.js · TranslationSuggestionCard.js         │
│ generic/TranslationSettingsPanel.js                                │
└──────────────────────────────┬─────────────────────────────────────┘
                               │ HTTP (Fiber) / SSE (MCP)
┌──────────────────────────────┴─────────────────────────────────────┐
│ ui/rest/translation.go            ui/mcp/translation.go            │
└──────────────────────────────┬─────────────────────────────────────┘
                               │
┌──────────────────────────────┴─────────────────────────────────────┐
│ usecase/translation.go (constructor + Translate methods)           │
│   ├── translation_context.go (system / RAG decision point)         │
│   ├── translation_cache.go (lookup, persist, key)                  │
│   ├── translation_prefs.go (per‑chat CRUD)                         │
│   └── translation_rag.go (Phase 3 retrieval + lazy backfill)       │
└──────────────────────────────┬─────────────────────────────────────┘
                               │
┌──────────────────────────────┴─────────────────────────────────────┐
│ domains/translation       infrastructure/translation               │
│   interfaces.go             factory.go                             │
│   translation.go            openai.go                              │
│                             embeddings_openai.go                   │
│                             repository.go                          │
│                             mock.go                                │
│                                                                    │
│ pkg/error/translation_error.go    validations/translation_*.go     │
└────────────────────────────────────────────────────────────────────┘
```

### 2.2 Request flow

```
Client
  │ POST /message/:id/translate {chat_jid, target_lang, force_refresh}
  ▼
ui/rest/translation.go              ─ parses, calls usecase
  ▼
usecase.TranslateMessage           ─ validates, loads message, scopes by device
  │
  ├─ resolveTargetLang             ─ request → per‑chat → global default
  │
  ├─ tryCacheLookup                ─ key: (device, chat, msg, lang, hash, providerKey)
  │     hit → return cached
  │     miss ↓
  │
  ├─ buildProviderInput            ─ DECISION POINT
  │     RAG enabled & embedder?
  │       yes → kickBackfill (async) + retrieveSimilarMessages
  │             rag.Used? yes: use rag.Context + rag.StyleExamples
  │                       no:  fall through to loadContext
  │       no  → loadContext (last‑N from chatstorage)
  │
  ├─ provider.Translate            ─ OpenAI chat completion (or mock)
  │     openai.go.parseSuggestions → NormalizeSuggestions
  │     enforces literal/natural/tone_matched contract
  │
  ├─ persistCache                  ─ best‑effort; failures are logged not returned
  │
  ▼
TranslateMessageResponse
  { message_id, chat_jid, source_text, source_lang, target_lang,
    suggestions: [{variant, text, rationale, confidence}], provider, cached }
```

### 2.3 Cache isolation key

Cache rows live in `message_translations` (sqlite). The provider field on each row is **not** the raw provider name — it is a synthetic key:

```
{providerName}/{PromptVersion}/{mode}
e.g.  openai/v2/rag    or   openai/v2/sys    or   mock/v2/sys
```

This guarantees:
- Bumping `PromptVersion` invalidates the cache without a schema change (v1 entries simply never match).
- RAG‑backed translations and system‑context translations don't collide; switching `TRANSLATION_RAG_ENABLED` is safe.
- Different deployments (openai vs mock) coexist without polluting each other's cache.

### 2.4 Data dictionary

All tables live in the chatstorage SQLite database. Migrations are append‑only — translation added migrations 23‑28.

#### `message_translations` (mig 23)

| Column | Type | Purpose |
|---|---|---|
| `device_id` | varchar | Device scoping. |
| `chat_jid` | varchar | Empty for drafts. |
| `message_id` | varchar | Empty for drafts. |
| `target_lang` | varchar(16) | Lower‑cased BCP‑47‑ish code. |
| `source_lang` | varchar(16) | Optional source hint. |
| `source_hash` | varchar(64) | sha256 hex of trimmed source text — invalidates on edits. |
| `provider` | varchar(64) | `{name}/{PromptVersion}/{mode}` synthetic key. |
| `suggestions` | text | JSON array of `{variant, text, rationale, confidence}`. |
| `created_at` | int64 | Unix seconds. |
| `expires_at` | int64 | Unix seconds; `0` means no expiry. |
| `PRIMARY KEY` | | (device_id, chat_jid, message_id, target_lang, source_hash, provider) |

#### `chat_translation_prefs` (mig 24)

| Column | Type | Purpose |
|---|---|---|
| `device_id` | varchar | Device scoping. |
| `chat_jid` | varchar | Conversation identifier. |
| `target_lang` | varchar(16) | Per‑chat override; empty string = "use global default". |
| `auto_translate` | bool | Surfaced as `auto_translate_inbound` on the API. |
| `translation_opt_in` | bool | Surfaced as `auto_translate_outbound` on the API (reserved for the compose hook). |
| `updated_at` | int64 | Unix seconds. |
| `PRIMARY KEY` | | (device_id, chat_jid) |

#### `message_embeddings` (mig 25 + 26 + 27 + 28)

| Column | Type | Purpose |
|---|---|---|
| `device_id` | varchar | Device scoping. |
| `chat_jid` | varchar | Conversation that produced the message. |
| `message_id` | varchar | Joins back to `messages.id`. |
| `model` | varchar(128) | e.g. `text-embedding-3-small`. |
| `vector` | blob | Float32 little‑endian (legacy column). |
| `vector_json` | text | JSON‑encoded float32 array (1536‑D ≈ 10 KB). |
| `created_at` | int64 | Unix seconds. |
| `PRIMARY KEY` | | (device_id, message_id, model) |
| Index `idx_message_embeddings_chat_lookup` | | (device_id, chat_jid, model, created_at) |
| Index `idx_message_embeddings_device_model` | | (device_id, model, created_at) |

### 2.5 File catalogue — used by · used for · pattern

#### Backend — Go

##### `src/pkg/error/translation_error.go`
- **Used by**: `usecase/translation.go`, `usecase/translation_prefs.go`. Indirectly: every REST handler that calls those usecases (Fiber's panic‑recover middleware reads `ErrCode()` and `StatusCode()`).
- **Used for**: typed errors with stable `ErrCode` and proper HTTP status codes — `TranslationDisabledError` (503), `TranslationDeviceMissingError` (400), `TranslationMessageNotFoundError` (404), `TranslationEmptyMessageError` (422), `TranslationProviderError` (502).
- **Pattern**: **Typed sentinel errors with method‑set** — same trio (`Error()`, `ErrCode()`, `StatusCode()`) used by every other `pkg/error` type in the host project. Idiomatic Go alternative to throwing strings.

##### `src/infrastructure/translation/factory.go`
- **Used by**: `cmd/root.go` during DI wiring.
- **Used for**: selecting the chat‑completion provider (`BuildProvider`) and the embedding provider (`BuildEmbedder`) from config. Returns the mock when keys are missing so the feature degrades instead of failing closed.
- **Pattern**: **Simple Factory** + **Null Object** (mock provider as graceful fallback). Keeps `cmd/root.go` thin and puts adapter wiring next to the adapters themselves.

##### `src/usecase/translation.go`
- **Used by**: `cmd/root.go` (constructed via `NewTranslationService`), `ui/rest/translation.go`, `ui/mcp/translation.go`.
- **Used for**: the public `TranslateMessage` and `TranslateDraft` orchestrations — validation, device scoping, source loading, cache check, provider call, error wrapping.
- **Pattern**: **Functional Options** (`WithEmbedder`, `WithClock`) for the constructor — keeps the signature small and lets tests inject a deterministic clock. **Single Responsibility** — file is now ~250 lines and only does orchestration; helpers live in dedicated files.

##### `src/usecase/translation_context.go`
- **Used by**: `usecase/translation.go` (both translate methods), via the `serviceTranslation` receiver.
- **Used for**: the single decision point `buildProviderInput` that picks between Phase 1 system context and Phase 3 RAG, plus `loadContext` for the system‑context fallback.
- **Pattern**: **Strategy** (system vs. RAG) selected at runtime; **Graceful Degradation** — every RAG failure falls through to system context. Keeping it in its own file documents the policy: "this is the one place where the choice is made."

##### `src/usecase/translation_cache.go`
- **Used by**: `usecase/translation.go` and `usecase/translation_prefs.go` (via `resolveTargetLang`).
- **Used for**: target‑language resolution, cache lookup with consistent warn‑and‑fallback semantics, persistence, and the synthetic provider key construction.
- **Pattern**: **Repository pattern** consumer (calls `domainTranslation.ITranslationRepository`), **Cache‑Aside**, **Versioned cache key** to handle prompt evolution without schema changes.

##### `src/usecase/translation_prefs.go`
- **Used by**: `ui/rest/translation.go` (the `GET/PUT /chat/:jid/translation-prefs` handlers), `ui/mcp/translation.go` (the prefs MCP tools).
- **Used for**: per‑chat preferences CRUD with read‑modify‑write semantics so partial updates don't accidentally clear unset fields. Maps API field names (`auto_translate_inbound` / `auto_translate_outbound`) to the existing storage column names (`auto_translate` / `translation_opt_in`).
- **Pattern**: **Partial Update / PATCH semantics** with pointer‑typed request fields — present pointer means "change this", nil means "leave alone". Validated server‑side so an empty body is rejected.

##### `src/usecase/translation_test.go`
- **Used by**: `go test ./usecase`.
- **Used for**: table‑driven tests for the deterministic `hashSource` helper — covers identical strings, whitespace tolerance, distinct content, empty input, and asserts the 64‑hex‑char output length.
- **Pattern**: **Table‑Driven Tests** + `t.Parallel()` matching the host project's existing test conventions. Uses `testify/assert`.

##### `src/usecase/translation_rag_test.go`
- **Used by**: `go test ./usecase`.
- **Used for**: pure‑math tests for `cosineSimilarity` (identical, orthogonal, opposite, zero‑vector, empty input edge cases), plus `topKByCosine` ranking + dedupe + exclusion behaviour, plus `candidatesToContext` metadata preservation.
- **Pattern**: Same as above. Critical safety check: `cosineSimilarity` must never return `NaN` when given a zero vector — the test asserts that explicitly.

##### `src/infrastructure/translation/openai_test.go`
- **Used by**: `go test ./infrastructure/translation`.
- **Used for**: `NormalizeSuggestions` contract enforcement — three variants in canonical order, missing variants get filler with explicit rationale, duplicates collapse to first‑wins, casing is normalized. Plus a `FloatsToBytes`/`bytesToFloats` round‑trip test that protects the BLOB↔JSON dual storage.
- **Pattern**: **Contract Tests** for the public `NormalizeSuggestions` helper — guarantees the 3‑card UI invariant regardless of what the LLM returns.

#### Frontend — Vue 3 (Options API, no SFC, no build step)

##### `src/views/components/generic/TranslationLanguages.js`
- **Used by**: `ChatMessages.js`, `SendMessage.js`, `generic/TranslationSettingsPanel.js`.
- **Used for**: the canonical 12‑language list, `languageLabel(code)` pretty‑printer, and `loadStoredTargetLang` / `storeTargetLang` helpers wrapping `localStorage`.
- **Pattern**: **Single Source of Truth** — adding a language is a one‑file change that propagates to every dropdown and chip in the UI. `Object.freeze` on the array prevents accidental mutation.

##### `src/views/components/generic/TranslationApi.js`
- **Used by**: `ChatMessages.js`, `SendMessage.js`.
- **Used for**: a tiny façade around the four REST endpoints. Returns the unwrapped `results` payload so callers don't keep typing `response.data.results`.
- **Pattern**: **Facade** + **DRY**. Components no longer hand‑roll URLs; a route rename is one edit. Internal `unwrap()` helper enforces the response shape contract.

##### `src/views/components/generic/translationMixin.js`
- **Used by**: `ChatMessages.js`, `SendMessage.js`, `TranslationSuggestionCard.js`.
- **Used for**: shared helpers — `variantLabel`, `variantColor`, `copyTranslationText` (with a clipboard‑API → textarea fallback for older browsers / non‑secure contexts).
- **Pattern**: **Mixin** (Vue 3 Options‑API style). Consistent with the rest of the host codebase, no build step required. Composables would force `<script setup>` and a bundler.

##### `src/views/components/generic/TranslationSuggestionCard.js`
- **Used by**: `ChatMessages.js` (manual translation panel), `SendMessage.js` (compose‑assist picker).
- **Used for**: a single suggestion card — variant chip, text, rationale, optional copy button, optional primary action ("Use this").
- **Pattern**: **Stateless presentational component** + **Props‑down/Events‑up**. No internal state. Emits `copy` and `select`. The parent owns selection and "use this" semantics, so the card can be reused in either a read‑only panel or a radio‑list picker without coupling.

##### `src/views/components/generic/TranslationSettingsPanel.js`
- **Used by**: `ChatMessages.js`.
- **Used for**: the inline per‑chat preferences editor — target‑lang dropdown ("Use global default" sentinel + 12 named languages), inbound/outbound toggles, effective‑lang label.
- **Pattern**: **Stateless presentational component** + **Controlled component**. Doesn't call the API itself; emits a single `update` event with a partial patch. The parent handles persistence and side effects (e.g. clearing auto‑translations on lang change). Two‑way bindings on the toggles use the `get/set` computed pattern that emits on `set`.

##### `src/views/components/ChatMessages.js`
- **Used by**: registered in `views/index.html`; opened from the "Chat Messages" card on the dashboard.
- **Used for**: the main chat reading surface. Owns message list state plus the manual‑translate flow, the auto‑translate inbound display, and the per‑chat preferences UI.
- **Pattern**: **Container component** (owns state, fetches data) wrapping presentational children. **Concurrency latch** (`autoTranslateBatchInFlight`) prevents Vue re‑renders from re‑firing the same per‑message calls. **Lang‑change invalidation** clears the inline auto display before refetching so stale‑language results never flash.

##### `src/views/components/SendMessage.js`
- **Used by**: registered in `views/index.html`; opened from the "Send Message" card on the dashboard.
- **Used for**: the existing send dialog plus the "Translate before send" flow — toggle → dropdown → "Get 3 suggestions" → `<TranslationSuggestionCard>` picker → swap text → existing `/send/message` call.
- **Pattern**: same container/presentational split as `ChatMessages.js`. **Original‑draft snapshot** (`originalDraft`) plus an Undo button so the user can recover their typed text after a pick. **Optimistic UX** — the chosen suggestion replaces the textarea immediately; sending is the unchanged existing pipeline.

### 2.6 Cross‑cutting design patterns

| Pattern | Where | Why it earned its keep |
|---|---|---|
| Typed errors with `ErrCode/StatusCode` | `pkg/error/translation_error.go` | Matches host project convention; Fiber middleware renders consistently. |
| Functional Options | `usecase.NewTranslationService` | Embedder is genuinely optional; clock is overridable for tests. |
| Strategy | `buildProviderInput` (system vs RAG) | One decision point, easy to audit. |
| Cache‑Aside + Versioned Key | `translation_cache.go` | Prompt evolution without schema migration. |
| Repository | `domainTranslation.ITranslationRepository` | Clean adapter boundary; SQLite today, Redis tomorrow. |
| Facade (frontend) | `TranslationApi.js` | Components don't know about HTTP. |
| Single Source of Truth | `TranslationLanguages.js` | Language list, storage keys, label format in one file. |
| Mixin | `translationMixin.js` | DRY helpers across multiple components, no build step needed. |
| Container/Presentational | All Vue components | `ChatMessages` and `SendMessage` own state; cards/panels are pure props‑in events‑out. |
| Graceful Degradation | Mock provider, RAG fallback, swallowed cache errors | The user never sees a 500 because of an internal hiccup. |

---

## Section 3 — Step‑by‑step user guide

### 3.1 Enable the feature

1. Open `src/.env` (copy from `.env.example` if needed).
2. Set the four core variables:
   ```env
   TRANSLATION_ENABLED=true
   TRANSLATION_PROVIDER=openai
   TRANSLATION_OPENAI_API_KEY=sk-...
   TRANSLATION_DEFAULT_TARGET_LANG=en
   ```
3. (Optional) Pick a model:
   ```env
   TRANSLATION_OPENAI_MODEL=gpt-4o-mini
   ```
4. Start the REST server: `cd src && go run . rest`. The dashboard opens on `http://localhost:3000`.

> If you skip the API key but leave `TRANSLATION_ENABLED=true`, the server boots with a mock provider so you can still click around and verify the wiring. The fake suggestions are obvious — they prefix the source text with `[lang|variant]`.

### 3.2 Translate a single inbound message

1. From the dashboard, open **Chat Messages**.
2. Type a JID (e.g. `628123456789`) and click **Load Messages**.
3. On any text message bubble, click the small **Translate** button (globe icon). The bubble expands into three cards:
   - **Literal** — close to word‑for‑word.
   - **Natural** — idiomatic phrasing in the target language.
   - **Tone‑matched** — mirrors the register/style of the recent thread (or your own writing style if RAG is on).
4. Click **Copy** on whichever card you want.
5. To change the language, open the **Translation Settings (Optional)** accordion above and pick from the dropdown — the open panel re‑translates immediately.

### 3.3 Set per‑chat preferences

1. Inside the same modal, click the **Translation settings** button next to **Reset**.
2. The inline panel shows:
   - **Per‑chat target language** — pick one of the 12 languages or leave "Use global default".
   - **Effective: …** — the value actually used after fallback (saved override → global default).
   - **Auto‑translate incoming messages** — when on, opening this chat prefetches translations for all inbound text bubbles and renders the natural variant in a small blue panel under each one.
   - **Auto‑translate outgoing drafts** — reserved; the preference is persisted but the compose‑assist hook ships separately.
3. Each toggle saves immediately. A success toast confirms the write.

### 3.4 Translate a reply before sending

1. From the dashboard, open **Send Message**.
2. Fill in the recipient and type your message in your own language.
3. Toggle **Translate before send** on.
4. Pick a target language from the dropdown.
5. Click **Get 3 suggestions**. The three cards render with provider + cache chips.
6. Click **Use this** on the card you prefer. The textarea is replaced with the chosen translation. An **Undo** button appears so you can restore the original draft.
7. Click **Send**. The existing `/send/message` pipeline carries the translated text exactly as if you'd typed it.

### 3.5 Use the REST API directly

```bash
# Translate a stored message
curl -X POST http://localhost:3000/message/3EB0288F008D32FCD0A424/translate \
  -H 'X-Device-Id: my-device' \
  -H 'Content-Type: application/json' \
  -d '{"chat_jid":"628123456789@s.whatsapp.net","target_lang":"en"}'

# Translate a draft (chat_jid optional)
curl -X POST http://localhost:3000/translate/draft \
  -H 'X-Device-Id: my-device' \
  -H 'Content-Type: application/json' \
  -d '{"text":"Sampai jumpa besok jam 9","target_lang":"en","chat_jid":"628123456789@s.whatsapp.net"}'

# Read per-chat prefs (always 200; missing rows resolve to defaults)
curl http://localhost:3000/chat/628123456789@s.whatsapp.net/translation-prefs \
  -H 'X-Device-Id: my-device'

# Partial update — flip a single flag without resending the whole record
curl -X PUT http://localhost:3000/chat/628123456789@s.whatsapp.net/translation-prefs \
  -H 'X-Device-Id: my-device' \
  -H 'Content-Type: application/json' \
  -d '{"auto_translate_inbound":true}'
```

Every endpoint requires the `X-Device-Id` header (or `?device_id=` query) — same scoping rule as the rest of the project.

### 3.6 Use the MCP tools

Start the MCP server (`go run . mcp`). The four translation tools register automatically:

| Tool | Purpose |
|---|---|
| `whatsapp_translate_message` | Translate a stored message by `message_id`. |
| `whatsapp_translate_draft` | Translate arbitrary draft text; `chat_jid` is optional but enables tone matching. |
| `whatsapp_get_chat_translation_prefs` | Read per‑chat preferences. |
| `whatsapp_set_chat_translation_prefs` | Partial update — pass only the fields to change. |

All four return structured payloads plus a human‑readable fallback string for clients that don't render structured output.

### 3.7 Turn on RAG (Phase 3)

RAG biases the **tone‑matched** variant toward how this user actually writes by retrieving similar messages from the chat (top 8) and the user's own outbound writing across all chats (top 4).

1. In `.env`, set:
   ```env
   TRANSLATION_RAG_ENABLED=true
   TRANSLATION_OPENAI_EMBEDDING_MODEL=text-embedding-3-small
   ```
   (The embedding API key falls back to `TRANSLATION_OPENAI_API_KEY` if you don't set `TRANSLATION_OPENAI_EMBEDDING_API_KEY` separately.)
2. Restart the server.
3. The first time you translate inside a fresh chat, retrieval will be empty and the response uses system context — exactly like Phase 1. The server kicks an asynchronous backfill burst (~100 messages, batched 32 per OpenAI call, debounced 60 s per chat).
4. Subsequent translations in the same chat surface tone‑matched variants conditioned on retrieved examples. The cache key includes a `rag` mode marker so you never see a stale system‑context result after enabling RAG.

If you ever need to flip RAG off, set `TRANSLATION_RAG_ENABLED=false` and restart. Existing RAG cache rows simply stop matching the new key (`...sys` instead of `...rag`) and age out on their TTL — no migration required.

### 3.8 Troubleshoot

| Symptom | Cause | Fix |
|---|---|---|
| `503 TRANSLATION_DISABLED` | `TRANSLATION_ENABLED` is false | Set the flag, restart. |
| `400 TRANSLATION_DEVICE_REQUIRED` | No `X-Device-Id` header | Add it; multi‑device middleware needs it. |
| `404 TRANSLATION_MESSAGE_NOT_FOUND` | Wrong message id, or the message belongs to another device | Check `chat_jid` and the active device. |
| `422 TRANSLATION_NO_TEXT_CONTENT` | Targeting a media‑only message | Translate is text‑only; pick a different message. |
| `502 TRANSLATION_PROVIDER_ERROR` | OpenAI quota / network / parse | Check your key and the server logs. The cache absorbs retries — re‑clicking is free. |
| Suggestions look like `[en\|literal] hi` | Mock provider is in use | Set `TRANSLATION_OPENAI_API_KEY`, restart. |
| RAG flag is on but tone variant doesn't change | Fresh chat — backfill still running, or fewer than ~10 messages exist | Wait a few seconds and retry; the cache will refresh. |
| `cached: true` after a server restart | This is correct — the cache is persisted in SQLite | Pass `force_refresh: true` in the body to bypass. |
