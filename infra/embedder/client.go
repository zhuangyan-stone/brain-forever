package embedder

import "context"

// Embedder is the embedding interface: converts text to float vectors
type Embedder interface {
	// Embed converts text to a vector
	Embed(ctx context.Context, text string) ([]float32, error)

	// Model returns the current model name
	Model() string

	// Dimension returns the vector dimension output by this Embedder
	// Different Embedders may output different dimensions (e.g., Alibaba 1024, Zhipu 2048),
	// but all vectors in the same database must have the same dimension.
	// Callers should use this value to initialize the vector index dimension.
	Dimension() int
}
