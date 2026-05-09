package toolcalls

import "context"

// ============================================================
// Personal Trait — RAG retrieval for personal knowledge base
// ============================================================

// TraitSource represents a personal knowledge source (RAG retrieval).
// Used for knowledge base references with similarity score.
type TraitSource struct {
	Title   string  `json:"title"`
	Content string  `json:"content,omitempty"`
	Score   float64 `json:"score"`
}

// TraitSearcher is the interface for searching personal traits
// from the user's knowledge base (vector search).
type TraitSearcher interface {
	SearchByText(ctx context.Context, queryText string, topK int) ([]TraitSource, error)
}
