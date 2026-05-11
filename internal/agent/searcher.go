package agent

import (
	"context"

	"BrainOnline/infra/searcher"
	"BrainOnline/internal/agent/toolimp"
	"BrainOnline/internal/store"
)

// ============================================================
// Adapter: converts store.VectorStore's SearchResult to toolimp.TraitSource
// ============================================================

// traitSearchAdapter adapts VectorStore to implement the toolimp.TraitSearcher interface
type traitSearchAdapter struct {
	store *store.VectorStore
}

// Close closes the underlying VectorStore database.
func (ps *traitSearchAdapter) Close() error {
	return ps.store.Close()
}

func (ps *traitSearchAdapter) SearchByText(ctx context.Context, queryText string, topK int) ([]toolimp.TraitSource, error) {
	results, err := ps.store.SearchByText(ctx, queryText, topK)
	if err != nil {
		return nil, err
	}

	sources := make([]toolimp.TraitSource, 0, len(results))
	for _, r := range results {
		if r.Score <= 0.6 {
			continue
		}
		sources = append(sources, toolimp.TraitSource{
			Title:   r.Document.Title,
			Content: r.Document.Content,
			Score:   r.Score,
		})
	}
	return sources, nil
}

// ============================================================
// Adapter: converts searcher.WebSearcher to toolimp.WebSearcher
// ============================================================

// webSearchAdapter adapts searcher.WebSearcher to implement the toolimp.WebSearcher interface
type webSearchAdapter struct {
	client searcher.WebSearcher
}

func (w *webSearchAdapter) SearchForLLM(ctx context.Context, query string, freshness string, count int) (string, []toolimp.WebSource, error) {
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
	var webPages []toolimp.WebSource
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
				webPages = append(webPages, toolimp.WebSource{
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
