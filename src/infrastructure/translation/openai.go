package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
)

// openAIProvider talks to any OpenAI-compatible /v1/chat/completions endpoint.
// It works with OpenAI proper, Azure OpenAI (with a tweaked base URL), and
// drop-in alternatives like Together, Groq, OpenRouter, or a self-hosted
// LiteLLM proxy. One round-trip returns three variants in a structured JSON.
type openAIProvider struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

// OpenAIConfig is the minimal configuration for the OpenAI-compatible provider.
type OpenAIConfig struct {
	BaseURL string // e.g. "https://api.openai.com/v1" or a compatible proxy
	APIKey  string
	Model   string        // e.g. "gpt-4o-mini" — defaults to "gpt-4o-mini"
	Timeout time.Duration // request timeout — defaults to 30s
}

// NewOpenAIProvider constructs an OpenAI-compatible chat-completion provider.
func NewOpenAIProvider(cfg OpenAIConfig) domainTranslation.Provider {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "gpt-4o-mini"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &openAIProvider{
		baseURL:    base,
		apiKey:     strings.TrimSpace(cfg.APIKey),
		model:      model,
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (p *openAIProvider) Name() string { return "openai" }

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatRequest struct {
	Model          string              `json:"model"`
	Messages       []openAIChatMessage `json:"messages"`
	Temperature    float32             `json:"temperature"`
	ResponseFormat map[string]string   `json:"response_format,omitempty"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIChatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

type translationPayload struct {
	Suggestions []struct {
		Variant    string  `json:"variant"`
		Text       string  `json:"text"`
		Rationale  string  `json:"rationale"`
		Confidence float32 `json:"confidence"`
	} `json:"suggestions"`
}

func (p *openAIProvider) Translate(ctx context.Context, in domainTranslation.ProviderRequest) ([]domainTranslation.Suggestion, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("openai provider: missing API key")
	}

	prompt := buildPrompt(in)
	body := openAIChatRequest{
		Model: p.model,
		Messages: []openAIChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: prompt},
		},
		Temperature:    0.4,
		ResponseFormat: map[string]string{"type": "json_object"},
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai provider: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("openai provider: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai provider: request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai provider: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai provider: status %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}

	var parsed openAIChatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("openai provider: decode response: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("openai provider: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("openai provider: empty choices in response")
	}

	content := parsed.Choices[0].Message.Content
	suggestions, err := parseSuggestions(content)
	if err != nil {
		return nil, fmt.Errorf("openai provider: %w", err)
	}
	return suggestions, nil
}

const systemPrompt = `You are a translation assistant for a WhatsApp client (prompt v2). ` +
	`You will receive a source message and may receive two distinct context blocks: ` +
	`"Thread context" (recent messages from the same chat) and "User style examples" ` +
	`(messages the user themselves authored across all chats). Treat the latter as ` +
	`evidence of how this user writes — slang, contractions, emoji, formality — and ` +
	`bias the tone_matched variant toward that voice while still translating into the target language.` +
	"\n\nProduce exactly three translations." +
	"\n\nReturn STRICT JSON only, matching this schema:" +
	"\n{\"suggestions\":[{\"variant\":\"literal|natural|tone_matched\",\"text\":\"...\",\"rationale\":\"...\",\"confidence\":0.0}]}" +
	"\n\nExactly one suggestion per variant in this order: literal, natural, tone_matched. " +
	`"literal" should be a close word-for-word translation, "natural" should sound idiomatic, ` +
	`"tone_matched" should mirror the register and style — favoring the user style examples ` +
	`when present, otherwise the thread context. ` +
	`Keep rationales under 80 characters. Confidence is a float in [0,1]. Do not output anything outside the JSON object.`

func buildPrompt(in domainTranslation.ProviderRequest) string {
	var sb strings.Builder
	sb.WriteString("Target language: ")
	sb.WriteString(in.TargetLang)
	sb.WriteString("\n")
	if strings.TrimSpace(in.SourceLang) != "" {
		sb.WriteString("Source language: ")
		sb.WriteString(in.SourceLang)
		sb.WriteString("\n")
	}
	if len(in.Context) > 0 {
		sb.WriteString("\nThread context (oldest -> newest):\n")
		for _, m := range in.Context {
			writeContextLine(&sb, m)
		}
	}
	if len(in.StyleExamples) > 0 {
		sb.WriteString("\nUser style examples (messages this user has written; mimic this voice):\n")
		for _, m := range in.StyleExamples {
			writeContextLine(&sb, m)
		}
	}
	sb.WriteString("\nMessage to translate:\n\"")
	sb.WriteString(in.SourceText)
	sb.WriteString("\"\n")
	return sb.String()
}

// writeContextLine renders a single ContextMessage as "- role: text" with
// the same shaping rules used everywhere — long messages are clipped at
// 240 characters and newlines are flattened so the prompt stays scannable.
func writeContextLine(sb *strings.Builder, m domainTranslation.ContextMessage) {
	role := m.Sender
	if m.IsFromMe {
		role = "me"
	}
	if strings.TrimSpace(role) == "" {
		role = "them"
	}
	text := strings.ReplaceAll(m.Content, "\n", " ")
	if len(text) > 240 {
		text = text[:240] + "..."
	}
	sb.WriteString("- ")
	sb.WriteString(role)
	sb.WriteString(": ")
	sb.WriteString(text)
	sb.WriteString("\n")
}

// jsonObjectPattern lazily extracts the first JSON object in a string. Some
// providers wrap JSON in code fences or prose despite response_format being
// set; this keeps us resilient.
var jsonObjectPattern = regexp.MustCompile(`(?s)\{.*\}`)

// NormalizeSuggestions enforces the 3-card contract — exactly one suggestion
// per [literal, natural, tone_matched] variant, in that order. Provider
// outputs that drop or duplicate variants are reshaped here so the rest of
// the system never has to defensively check for missing slots.
//
// When a variant is missing the function fills the gap with the closest
// available suggestion (rather than failing the whole call), but tags the
// rationale so callers can surface that the result is a fallback.
func NormalizeSuggestions(in []domainTranslation.Suggestion) []domainTranslation.Suggestion {
	want := []string{
		domainTranslation.VariantLiteral,
		domainTranslation.VariantNatural,
		domainTranslation.VariantToneMatched,
	}
	byVariant := make(map[string]domainTranslation.Suggestion, 3)
	for _, s := range in {
		v := strings.ToLower(strings.TrimSpace(s.Variant))
		if _, exists := byVariant[v]; exists {
			continue
		}
		byVariant[v] = domainTranslation.Suggestion{
			Variant:    v,
			Text:       strings.TrimSpace(s.Text),
			Rationale:  strings.TrimSpace(s.Rationale),
			Confidence: s.Confidence,
		}
	}

	out := make([]domainTranslation.Suggestion, 0, 3)
	var fallback *domainTranslation.Suggestion
	for _, s := range in {
		v := strings.ToLower(strings.TrimSpace(s.Variant))
		if v == "" {
			continue
		}
		copy := s
		copy.Variant = v
		fallback = &copy
		break
	}
	for _, variant := range want {
		if s, ok := byVariant[variant]; ok {
			out = append(out, s)
			continue
		}
		if fallback != nil {
			out = append(out, domainTranslation.Suggestion{
				Variant:    variant,
				Text:       strings.TrimSpace(fallback.Text),
				Rationale:  "Provider did not return this variant; reused closest match.",
				Confidence: fallback.Confidence,
			})
		}
	}
	return out
}

func parseSuggestions(content string) ([]domainTranslation.Suggestion, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("empty content")
	}
	if match := jsonObjectPattern.FindString(content); match != "" {
		content = match
	}

	var payload translationPayload
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return nil, fmt.Errorf("parse suggestions JSON: %w", err)
	}
	if len(payload.Suggestions) == 0 {
		return nil, fmt.Errorf("no suggestions in response")
	}

	raw := make([]domainTranslation.Suggestion, 0, len(payload.Suggestions))
	for _, s := range payload.Suggestions {
		raw = append(raw, domainTranslation.Suggestion{
			Variant:    strings.ToLower(strings.TrimSpace(s.Variant)),
			Text:       strings.TrimSpace(s.Text),
			Rationale:  strings.TrimSpace(s.Rationale),
			Confidence: s.Confidence,
		})
	}
	out := NormalizeSuggestions(raw)
	if len(out) == 0 {
		return nil, fmt.Errorf("provider returned no usable suggestions")
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
