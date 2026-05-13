package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"BrainForever/infra/httpx"
)

// ============================================================
// ZhipuEmbedder — calls Zhipu GLM Embedding API
// ============================================================

// ZhipuEmbedder converts text to vectors via the Zhipu API
type ZhipuEmbedder struct {
	apiKey    string
	model     string
	dimension int
	client    *http.Client
}

// NewZhipuEmbedder creates a Zhipu Embedder
// apiKey: Zhipu API Key, if empty reads from the env variable specified by envKey
// model: model name, defaults to embedding-3 if empty
func NewZhipuEmbedder(apiKey, envKey string, dimension int) *ZhipuEmbedder {
	if apiKey == "" {
		apiKey = os.Getenv(envKey)
	}

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

// Dimension returns the vector dimension (Zhipu embedding-3 fixed at 2048)
func (z *ZhipuEmbedder) Dimension() int {
	return z.dimension
}

// zhipuRequest is the Zhipu Embedding API request body (OpenAI compatible format)
type zhipuRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
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
func (z *ZhipuEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if z.apiKey == "" {
		return nil, fmt.Errorf("ZHIPUAI_API_KEY not set (please set the ZHIPUAI_API_KEY environment variable)")
	}

	reqBody := zhipuRequest{
		Model: z.model,
		Input: text,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://open.bigmodel.cn/api/paas/v4/embeddings",
		bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request. %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+z.apiKey)

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

	fmt.Printf("  → Embedding complete: %d dims, %d tokens used\n", len(vec), apiResp.Usage.TotalTokens)
	return vec, nil
}
