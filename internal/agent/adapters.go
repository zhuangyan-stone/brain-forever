package agent

import (
	"context"

	"BrainOnline/infra/searcher"
	"BrainOnline/internal/agent/toolcalls"
	"BrainOnline/internal/store"
)

// ============================================================
// Adapter: converts store.VectorStore's SearchResult to toolcalls.TraitSource
// ============================================================

// traitSearchAdapter adapts VectorStore to implement the toolcalls.TraitSearcher interface
type traitSearchAdapter struct {
	store *store.VectorStore
}

func (ps *traitSearchAdapter) SearchByText(ctx context.Context, queryText string, topK int) ([]toolcalls.TraitSource, error) {
	results, err := ps.store.SearchByText(ctx, queryText, topK)
	if err != nil {
		return nil, err
	}

	sources := make([]toolcalls.TraitSource, 0, len(results))
	for _, r := range results {
		if r.Score <= 0.6 {
			continue
		}
		sources = append(sources, toolcalls.TraitSource{
			Title:   r.Document.Title,
			Content: r.Document.Content,
			Score:   r.Score,
		})
	}
	return sources, nil
}

// ============================================================
// Adapter: converts searcher.WebSearcher to toolcalls.WebSearcher
// ============================================================

// webSearchAdapter adapts searcher.WebSearcher to implement the toolcalls.WebSearcher interface
type webSearchAdapter struct {
	client searcher.WebSearcher
}

func (w *webSearchAdapter) SearchForLLM(ctx context.Context, query string, freshness string, count int) (string, []toolcalls.WebSource, error) {
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
	var webPages []toolcalls.WebSource
	if resp != nil {
		for _, p := range resp.Pages {
			content := p.Summary

			if p.Snippet != "" {
				content += "\n\n" + p.Snippet
			}

			if content != "" {
				var publishDateStr string
				if p.PublishDate != nil && !p.PublishDate.IsZero() {
					publishDateStr = p.PublishDate.Format("2006-01-02")
				}
				webPages = append(webPages, toolcalls.WebSource{
					Title:       p.Title,
					Content:     content,
					URL:         p.URL,
					SiteName:    p.SiteName,
					SiteIcon:    p.SiteIcon,
					PublishDate: publishDateStr,
				})
			}
		}
	}
	return llmText, webPages, nil
}
