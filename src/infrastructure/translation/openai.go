package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	domainTranslation "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/translation"
)

// PromptVersion bumps when the system prompt or schema changes. Persisted in
// the cache so a prompt change doesn't poison old rows.
const PromptVersion = "v1"

// OpenAIProvider calls the OpenAI Chat Completions API with structured JSON
// output to get all 3 suggestions in a single round-trip.
type OpenAIProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// OpenAIConfig configures the provider.
type OpenAIConfig struct {
	APIKey  string
	Model   string // e.g. "gpt-4o-mini"
	BaseURL string // optional override, e.g. for compatible endpoints
	Timeout time.Duration
}

// NewOpenAIProvider returns a Provider backed by OpenAI Chat Completions.
func NewOpenAIProvider(cfg OpenAIConfig) *OpenAIProvider {
	model := cfg.Model
	if model == "" {
		model = "gpt-4o-mini"
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &OpenAIProvider{
		apiKey:  cfg.APIKey,
		model:   model,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: timeout},
	}
}

// Name identifies the provider in cache rows ("openai:<model>").
func (p *OpenAIProvider) Name() string {
	return "openai:" + p.model
}

// openai chat-completion wire types (subset)
type oaiMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type oaiResponseFormat struct {
	Type string `json:"type"`
}
type oaiRequest struct {
	Model          string             `json:"model"`
	Messages       []oaiMsg           `json:"messages"`
	Temperature    float32            `json:"temperature"`
	ResponseFormat *oaiResponseFormat `json:"response_format,omitempty"`
}
type oaiChoice struct {
	Message oaiMsg `json:"message"`
}
type oaiResponse struct {
	Choices []oaiChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// modelOutput is the JSON shape we ask the model to produce. Keeping it tight
// makes parsing deterministic and avoids hallucinated extra fields.
type modelOutput struct {
	DetectedSourceLang string                      `json:"detected_source_lang"`
	Suggestions        []domainTranslation.Suggestion `json:"suggestions"`
}

// GenerateSuggestions calls the model and parses 3 candidates.
func (p *OpenAIProvider) GenerateSuggestions(ctx context.Context, in domainTranslation.ProviderRequest) ([]domainTranslation.Suggestion, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("openai provider: missing API key")
	}
	if strings.TrimSpace(in.SourceText) == "" {
		return nil, fmt.Errorf("openai provider: empty source text")
	}
	if strings.TrimSpace(in.TargetLang) == "" {
		return nil, fmt.Errorf("openai provider: missing target_lang")
	}

	system := buildSystemPrompt(in.TargetLang)
	user := buildUserPrompt(in)

	reqBody, err := json.Marshal(oaiRequest{
		Model:          p.model,
		Temperature:    0.4,
		ResponseFormat: &oaiResponseFormat{Type: "json_object"},
		Messages: []oaiMsg{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("openai provider: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("openai provider: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai provider: http: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai provider: read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai provider: status %d: %s", resp.StatusCode, string(rawBody))
	}

	var oaiResp oaiResponse
	if err := json.Unmarshal(rawBody, &oaiResp); err != nil {
		return nil, fmt.Errorf("openai provider: unmarshal response: %w", err)
	}
	if oaiResp.Error != nil {
		return nil, fmt.Errorf("openai provider: api error: %s", oaiResp.Error.Message)
	}
	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("openai provider: empty choices")
	}

	content := strings.TrimSpace(oaiResp.Choices[0].Message.Content)
	var parsed modelOutput
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, fmt.Errorf("openai provider: unmarshal model output: %w (raw=%q)", err, content)
	}

	suggestions := normalizeSuggestions(parsed.Suggestions)
	if len(suggestions) == 0 {
		return nil, fmt.Errorf("openai provider: model returned no suggestions")
	}
	return suggestions, nil
}

// normalizeSuggestions enforces the contract: exactly 3 entries, one per
// variant, in a stable order (literal, natural, tone_matched). If the model
// missed a variant it's filled with the first non-empty fallback so the UI
// always renders 3 cards.
func normalizeSuggestions(raw []domainTranslation.Suggestion) []domainTranslation.Suggestion {
	byVariant := map[string]domainTranslation.Suggestion{}
	for _, s := range raw {
		v := strings.ToLower(strings.TrimSpace(s.Variant))
		switch v {
		case domainTranslation.VariantLiteral,
			domainTranslation.VariantNatural,
			domainTranslation.VariantToneMatched:
			s.Variant = v
			byVariant[v] = s
		}
	}
	order := []string{
		domainTranslation.VariantLiteral,
		domainTranslation.VariantNatural,
		domainTranslation.VariantToneMatched,
	}
	var fallback domainTranslation.Suggestion
	for _, v := range order {
		if s, ok := byVariant[v]; ok && strings.TrimSpace(s.Text) != "" {
			fallback = s
			break
		}
	}
	out := make([]domainTranslation.Suggestion, 0, 3)
	for _, v := range order {
		if s, ok := byVariant[v]; ok && strings.TrimSpace(s.Text) != "" {
			out = append(out, s)
			continue
		}
		// fill missing variant with fallback text but tag with the slot variant
		out = append(out, domainTranslation.Suggestion{
			Variant:   v,
			Text:      fallback.Text,
			Rationale: "fallback (model omitted variant)",
		})
	}
	return out
}

func buildSystemPrompt(targetLang string) string {
	return strings.Join([]string{
		"You are a WhatsApp message translator.",
		fmt.Sprintf("Translate the user's message into %s.", targetLang),
		"Always return JSON of the form:",
		`{"detected_source_lang":"<bcp47>","suggestions":[`,
		`  {"variant":"literal","text":"...","rationale":"...","confidence":0.0},`,
		`  {"variant":"natural","text":"...","rationale":"...","confidence":0.0},`,
		`  {"variant":"tone_matched","text":"...","rationale":"...","confidence":0.0}`,
		`]}`,
		"`literal` should be a faithful word-for-word translation.",
		"`natural` should be the most idiomatic translation a native speaker would write.",
		"`tone_matched` should match the register, slang, and emoji usage of the surrounding chat context.",
		"Keep names, @mentions, URLs, and emoji unchanged. Do not include any prose outside the JSON.",
	}, "\n")
}

func buildUserPrompt(in domainTranslation.ProviderRequest) string {
	var b strings.Builder
	if len(in.Context) > 0 {
		b.WriteString("Recent thread (oldest first):\n")
		for _, m := range in.Context {
			who := m.Sender
			if m.IsFromMe {
				who = "me"
			}
			if who == "" {
				who = "them"
			}
			line := strings.ReplaceAll(strings.TrimSpace(m.Content), "\n", " ")
			if line == "" {
				continue
			}
			fmt.Fprintf(&b, "- %s: %s\n", who, line)
		}
		b.WriteString("\n")
	}
	if in.SourceLang != "" {
		fmt.Fprintf(&b, "Source language hint: %s\n", in.SourceLang)
	}
	fmt.Fprintf(&b, "Target language: %s\n", in.TargetLang)
	fmt.Fprintf(&b, "Message to translate:\n%s\n", in.SourceText)
	return b.String()
}
