package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"BrainForever/infra/httpx"
)

// ============================================================
// ZhipuEmbedder -calls Zhipu GLM Embedding API
// ============================================================

// ZhipuEmbedder converts text to vectors via the Zhipu API
type ZhipuEmbedder struct {
	apiKey    string
	model     string
	dimension int
	client    *http.Client
}

// NewZhipuEmbedder creates a Zhipu Embedder.
// apiKey: API key for Zhipu service (if empty, the client has no default key;
// callers must provide an apiKey per-request).
func NewZhipuEmbedder(apiKey string, dimension int) *ZhipuEmbedder {
	return &ZhipuEmbedder{
		apiKey:    apiKey,
		model:     "embedding-3",
		dimension: dimension,
		client:    httpx.NewHTTPClient(60 * time.Second),
	}
}

// Model returns the current model name
func (z *ZhipuEmbedder) Model() string {
	return z.model
}

// Dimension returns the vector dimension (Zhipu embedding-3 supports 256/512/1024/2048)
func (z *ZhipuEmbedder) Dimension() int {
	return z.dimension
}

// zhipuRequest is the Zhipu Embedding API request body (OpenAI compatible format)
type zhipuRequest struct {
	Model      string `json:"model"`
	Input      string `json:"input"`
	Dimensions int    `json:"dimensions,omitempty"`
}

// zhipuResponse is the Zhipu Embedding API response body (OpenAI compatible format)
type zhipuResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// Embed converts text to a vector (implements the embedder.Embedder interface)
// apiKey: if non-empty, overrides the client's default API key for this request.
func (z *ZhipuEmbedder) Embed(ctx context.Context, text string, apiKey string) ([]float32, error) {
	if apiKey == "" {
		apiKey = z.apiKey
	}
	if apiKey == "" {
		return nil, fmt.Errorf("API client not initialized (API key may be missing)")
	}

	reqBody := zhipuRequest{
		Model:      z.model,
		Input:      text,
		Dimensions: z.dimension,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://open.bigmodel.cn/api/paas/v4/embeddings",
		bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request. %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := z.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed. %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned error [%d]. %s", resp.StatusCode, string(body))
	}

	var apiResp zhipuResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response. %w", err)
	}

	if len(apiResp.Data) == 0 {
		return nil, fmt.Errorf("API returned empty data")
	}

	// Zhipu returns float64, convert to float32
	raw := apiResp.Data[0].Embedding
	vec := make([]float32, len(raw))
	for i, v := range raw {
		vec[i] = float32(v)
	}

	fmt.Printf("  ->Embedding complete: %d dims, %d tokens used\n", len(vec), apiResp.Usage.TotalTokens)
	return vec, nil
}
