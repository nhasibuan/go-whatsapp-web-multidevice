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

// OpenAIEmbeddingProvider calls the OpenAI embeddings API. Used by the RAG
// pipeline to embed both incoming queries (the source text the user is about
// to translate) and corpus messages backfilled from chatstorage.
type OpenAIEmbeddingProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// OpenAIEmbeddingConfig configures the embeddings provider. Defaults match the
// most cost-effective OpenAI option as of writing (text-embedding-3-small,
// 1536-D, ~$0.02 per 1M tokens).
type OpenAIEmbeddingConfig struct {
	APIKey  string
	Model   string // e.g. "text-embedding-3-small"
	BaseURL string // optional override for compatible endpoints
	Timeout time.Duration
}

// NewOpenAIEmbeddingProvider returns an EmbeddingProvider backed by OpenAI.
func NewOpenAIEmbeddingProvider(cfg OpenAIEmbeddingConfig) *OpenAIEmbeddingProvider {
	model := cfg.Model
	if model == "" {
		model = "text-embedding-3-small"
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &OpenAIEmbeddingProvider{
		apiKey:  cfg.APIKey,
		model:   model,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: timeout},
	}
}

// Model returns the configured model identifier. Persisted with each row.
func (p *OpenAIEmbeddingProvider) Model() string {
	return p.model
}

// Wire types for the OpenAI embeddings endpoint. We only model the fields we
// actually consume so additional response fields don't break unmarshal.
type oaiEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}
type oaiEmbedItem struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}
type oaiEmbedResponse struct {
	Data  []oaiEmbedItem `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// Embed batches all inputs into a single API call and returns vectors in the
// same order. Empty inputs are not allowed by the OpenAI API; callers are
// expected to filter out empty strings before invoking.
func (p *OpenAIEmbeddingProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if p == nil || p.apiKey == "" {
		return nil, fmt.Errorf("openai embeddings: missing API key")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	for i, t := range texts {
		if strings.TrimSpace(t) == "" {
			return nil, fmt.Errorf("openai embeddings: input[%d] is empty", i)
		}
	}

	body, err := json.Marshal(oaiEmbedRequest{Model: p.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("openai embeddings: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai embeddings: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai embeddings: http: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai embeddings: read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai embeddings: status %d: %s", resp.StatusCode, string(raw))
	}

	var parsed oaiEmbedResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("openai embeddings: unmarshal: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("openai embeddings: api error: %s", parsed.Error.Message)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("openai embeddings: expected %d vectors, got %d", len(texts), len(parsed.Data))
	}

	// The API documents that `index` corresponds to the input position, but it
	// can return out-of-order in some edge cases. Sort defensively into a
	// position-aligned slice.
	out := make([][]float32, len(texts))
	for _, item := range parsed.Data {
		if item.Index < 0 || item.Index >= len(out) {
			return nil, fmt.Errorf("openai embeddings: response index %d out of range", item.Index)
		}
		out[item.Index] = item.Embedding
	}
	for i, vec := range out {
		if len(vec) == 0 {
			return nil, fmt.Errorf("openai embeddings: missing vector for input[%d]", i)
		}
	}
	return out, nil
}

// Compile-time assertion: *OpenAIEmbeddingProvider satisfies the domain interface.
var _ domainTranslation.EmbeddingProvider = (*OpenAIEmbeddingProvider)(nil)
