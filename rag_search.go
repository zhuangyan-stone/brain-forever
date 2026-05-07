package main

import (
	"context"

	"BrainOnline/infra/3rdapi/searcher"
	"BrainOnline/internal/agent"
	"BrainOnline/internal/store"
)

// ============================================================
// Adapter: converts store.VectorStore's SearchResult to agent.TraitSource
// ============================================================

// traitSearchAdapter adapts VectorStore to implement the agent.PersonalTraitSearcher interface
type traitSearchAdapter struct {
	store *store.VectorStore
}

func (ps *traitSearchAdapter) SearchByText(ctx context.Context, queryText string, topK int) ([]agent.TraitSource, error) {
	results, err := ps.store.SearchByText(ctx, queryText, topK)
	if err != nil {
		return nil, err
	}

	sources := make([]agent.TraitSource, 0, len(results))
	for _, r := range results {
		if r.Score <= 0.6 {
			continue
		}
		sources = append(sources, agent.TraitSource{
			Title:   r.Document.Title,
			Content: r.Document.Content,
			Score:   r.Score,
		})
	}
	return sources, nil
}

// ============================================================
// Adapter: converts search.WebSearcher to agent.WebSearcher
// ============================================================

// webSearchAdapter adapts searcher.WebSearcher to implement the agent.WebSearcher interface
type webSearchAdapter struct {
	client searcher.WebSearcher
}

func (w *webSearchAdapter) SearchForLLM(ctx context.Context, query string, freshness string, count int) (string, []agent.WebSource, error) {
	req := searcher.WebSearchRequest{
		Query:              []string{query},
		Freshness:          freshness,
		Count:              count,
		Summary:            true,
		FamilyFriendlyOnly: true,
	}
	resp, llmText, err := w.client.SearchForLLM(ctx, req, 10240)
	if err != nil {
		return "", nil, err
	}

	// Extract web page results for frontend display
	var webPages []agent.WebSource
	if resp != nil {
		for _, p := range resp.Pages {
			content := p.Summary

			if p.Snippet != "" {
				content += "\n\n" + p.Snippet
			}

			if content != "" {
				webPages = append(webPages, agent.WebSource{
					Title:   p.Name,
					Content: content,
					URL:     p.URL,
				})
			}
		}
	}
	return llmText, webPages, nil
}
