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

// openAIEmbeddingProvider implements the EmbeddingProvider interface against
// OpenAI's /v1/embeddings endpoint. The default model is
// text-embedding-3-small — small dimension (1536), low cost
// (~$0.000003/call at typical message length), and high enough quality for
// in-chat retrieval.
//
// The provider is intentionally separate from the chat-completion
// translation provider: embedding models update on a different cadence and
// the cache key includes the model name so older vectors don't get mixed in.
type openAIEmbeddingProvider struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

// OpenAIEmbeddingConfig configures the OpenAI embedding provider. BaseURL
// and Model are optional — sensible defaults are applied.
type OpenAIEmbeddingConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

// NewOpenAIEmbeddingProvider constructs an embedding provider. Returns nil
// when the API key is missing — callers should treat a nil provider as
// "RAG retrieval unavailable" and fall back to system context.
func NewOpenAIEmbeddingProvider(cfg OpenAIEmbeddingConfig) domainTranslation.EmbeddingProvider {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "text-embedding-3-small"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &openAIEmbeddingProvider{
		baseURL:    base,
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (p *openAIEmbeddingProvider) Name() string  { return "openai-embeddings" }
func (p *openAIEmbeddingProvider) Model() string { return p.model }

type openAIEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openAIEmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// embeddingBatchSize matches OpenAI's recommended batching limit. Going
// higher works but cuts into the per-request timeout.
const embeddingBatchSize = 32

func (p *openAIEmbeddingProvider) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	out := make([][]float32, len(inputs))

	for start := 0; start < len(inputs); start += embeddingBatchSize {
		end := start + embeddingBatchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		batch := inputs[start:end]
		vectors, err := p.embedBatch(ctx, batch)
		if err != nil {
			return nil, err
		}
		if len(vectors) != len(batch) {
			return nil, fmt.Errorf("openai-embeddings: expected %d vectors, got %d", len(batch), len(vectors))
		}
		copy(out[start:end], vectors)
	}
	return out, nil
}

func (p *openAIEmbeddingProvider) embedBatch(ctx context.Context, batch []string) ([][]float32, error) {
	body := openAIEmbeddingRequest{Model: p.model, Input: batch}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai-embeddings: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/embeddings", bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("openai-embeddings: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai-embeddings: request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai-embeddings: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai-embeddings: status %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}

	var parsed openAIEmbeddingResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("openai-embeddings: decode response: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("openai-embeddings: %s", parsed.Error.Message)
	}
	if len(parsed.Data) == 0 {
		return nil, fmt.Errorf("openai-embeddings: empty data")
	}

	// OpenAI returns vectors in `index` order; defensive about gaps.
	out := make([][]float32, len(batch))
	for _, item := range parsed.Data {
		if item.Index < 0 || item.Index >= len(batch) {
			continue
		}
		out[item.Index] = item.Embedding
	}
	for i := range out {
		if out[i] == nil {
			return nil, fmt.Errorf("openai-embeddings: provider skipped index %d", i)
		}
	}
	return out, nil
}
