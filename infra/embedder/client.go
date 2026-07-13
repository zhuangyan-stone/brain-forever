package embedder

import "context"

// Embedder is the embedding interface: converts text to float vectors
type Embedder interface {
	// Embed converts text to a vector.
	// apiKey: optional per-request API key override; empty means use the client's default.
	Embed(ctx context.Context, text string, apiKey string) ([]float32, error)

	// Name returns the provider name (e.g. "DashScope", "ZhiPu").
	Name() string

	// Website returns the provider's official website URL.
	Website() string

	// Model returns the current model name
	Model() string

	// Dimension returns the vector dimension output by this Embedder
	// Different Embedders may output different dimensions (e.g., Alibaba 1024, Zhipu 2048),
	// but all vectors in the same database must have the same dimension.
	// Callers should use this value to initialize the vector index dimension.
	Dimension() int
}
