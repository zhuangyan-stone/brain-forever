package agent

import (
	"BrainForever/infra/embedder"
	"BrainForever/internal/local/agent/toolimp"
	"BrainForever/internal/local/store"
	"context"
	"fmt"
	"strconv"
)

// traitSearchAdapter adapts searcher.WebSearcher to implement the toolimp.TraitSearcher interface
type traitSearchAdapter struct {
	client embedder.Embedder
	store  *store.VectorStore
}

// SearchByText finds matching personal trait descriptions by the given text.
func (a *traitSearchAdapter) SearchByText(ctx context.Context, queryText string, category int, topK int) ([]toolimp.TraitSource, error) {
	// 1. Use the client (embedder.Embedder) to compute the vector for queryText
	vector, err := a.client.Embed(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query text: %w", err)
	}

	// 2. Call store.Search() to perform the query, get []store.PersonalTrait, then convert to []toolimp.TraitSource
	traits, err := a.store.Search(vector, category, topK)
	if err != nil {
		return nil, fmt.Errorf("failed to search traits: %w", err)
	}

	// 3. During []store.PersonalTrait -> []toolimp.TraitSource conversion, filter out traits with similarity < 6
	var result []toolimp.TraitSource
	for _, pt := range traits {
		if pt.Score < 6 {
			continue
		}
		result = append(result, toolimp.TraitSource{
			ID:         pt.ID,
			Trait:      pt.Trait,
			Category:   strconv.Itoa(pt.Category),
			Confidence: pt.Confidence,
			HalfLife:   strconv.Itoa(pt.HalfLife),
			ChatSN:     pt.ChatSN,
			ChatTitle:  nil, // ChatTitle requires a separate lookup from chat_sessions table
			CreateAt:   pt.CreateAt,
			UpdateAt:   pt.UpdateAt,
		})
	}

	return result, nil
}

func (a *traitSearchAdapter) SearchByKeyword(ctx context.Context, queryKeyword string, queryType int) ([]toolimp.TraitSource, error) {
	traits, err := a.store.SearchByKeyword(queryKeyword, queryType, 20)
	if err != nil {
		return nil, fmt.Errorf("failed to search traits by keyword: %w", err)
	}

	var result []toolimp.TraitSource
	for _, pt := range traits {
		result = append(result, toolimp.TraitSource{
			ID:         pt.ID,
			Trait:      pt.Trait,
			Category:   strconv.Itoa(pt.Category),
			Confidence: pt.Confidence,
			HalfLife:   strconv.Itoa(pt.HalfLife),
			ChatSN:     pt.ChatSN,
			ChatTitle:  nil,
			CreateAt:   pt.CreateAt,
			UpdateAt:   pt.UpdateAt,
		})
	}

	return result, nil
}

// Close releases any underlying resources held by the searcher.
func (a *traitSearchAdapter) Close() error {
	a.store.Close()
	return nil
}
