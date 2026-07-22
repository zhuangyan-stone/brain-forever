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

// DashScopeEmbedder converts text to vectors via Alibaba Cloud DashScope API
type DashScopeEmbedder struct {
	apiKey    string
	model     string
	dimension int
	client    *http.Client
}

// NewDashScopeEmbedder creates an Alibaba DashScope Embedder.
// apiKey: API key for DashScope service (if empty, the client has no default key;
// callers must provide an apiKey per-request).
func NewDashScopeEmbedder(apiKey string, dimension int) *DashScopeEmbedder {
	return &DashScopeEmbedder{
		apiKey:    apiKey,
		model:     "text-embedding-v4",
		dimension: dimension,
		client:    httpx.NewHTTPClient(60 * time.Second),
	}
}

// Name returns the provider name.
func (d *DashScopeEmbedder) Name() string {
	return "DashScope"
}

// Website returns the provider's official website URL.
func (d *DashScopeEmbedder) Website() string {
	return "https://dashscope.aliyun.com"
}

// Model returns the current model name
func (d *DashScopeEmbedder) Model() string {
	return d.model
}

// Dimension returns the vector dimension (Alibaba text-embedding-v4 supports specifying dimension via API parameter)
func (d *DashScopeEmbedder) Dimension() int {
	return d.dimension
}

// dashScopeRequest is the DashScope Embedding API request body
type dashScopeRequest struct {
	Model string `json:"model"`
	Input struct {
		Texts []string `json:"texts"`
	} `json:"input"`
	Params struct {
		Dimension int    `json:"dimension"`
		TextType  string `json:"text_type"`
	} `json:"parameters"`
}

// dashScopeResponse is the DashScope Embedding API response body
type dashScopeResponse struct {
	Output struct {
		Embeddings []struct {
			TextIndex int       `json:"text_index"`
			Embedding []float64 `json:"embedding"`
		} `json:"embeddings"`
	} `json:"output"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// Embed converts text to a vector (implements the Embedder interface)
// apiKey: if non-empty, overrides the client's default API key for this request.
func (d *DashScopeEmbedder) Embed(ctx context.Context, text string, apiKey string) ([]float32, error) {
	if apiKey == "" {
		apiKey = d.apiKey
	}
	if apiKey == "" {
		return nil, fmt.Errorf("aPI client not initialized (API key may be missing)")
	}

	reqBody := dashScopeRequest{
		Model: d.model,
		Input: struct {
			Texts []string `json:"texts"`
		}{
			Texts: []string{text},
		},
		Params: struct {
			Dimension int    `json:"dimension"`
			TextType  string `json:"text_type"`
		}{
			Dimension: d.dimension,
			TextType:  "document",
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://dashscope.aliyuncs.com/api/v1/services/embeddings/text-embedding/text-embedding",
		bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request. %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aPI request failed. %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("aPI returned error [%d]. %s", resp.StatusCode, string(body))
	}

	var apiResp dashScopeResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response. %w", err)
	}

	if len(apiResp.Output.Embeddings) == 0 {
		return nil, fmt.Errorf("aPI returned empty data")
	}

	// DashScope returns float64, convert to float32
	raw := apiResp.Output.Embeddings[0].Embedding
	vec := make([]float32, len(raw))
	for i, v := range raw {
		vec[i] = float32(v)
	}

	fmt.Printf("  -> Embedding complete: %d dims, %d tokens used\n", len(vec), apiResp.Usage.TotalTokens)
	return vec, nil
}
