package agent

import (
	"context"
	"fmt"

	"BrainForever/infra/embedder"
	"BrainForever/internal/agent/toolimp"
	"BrainForever/internal/store"
	"BrainForever/internal/store/cache"
)

// excerptSearchAdapter adapts store.ExcerptStore + embedder to implement
// the toolimp.ExcerptSearcher interface.
type excerptSearchAdapter struct {
	store          *store.ExcerptStore
	vdCache        *cache.ExcerptValueDictCache
	embedder       embedder.Embedder
	embedderAPIKey string
	lang           string
	userID         int64
}

// SearchByValues finds excerpts by value type labels using GIN index filtering.
// If valueTypes is empty, returns the most recent excerpts.
func (a *excerptSearchAdapter) SearchByValues(ctx context.Context, userID int64, valueTypes []string, limit int) ([]toolimp.ExcerptSource, error) {
	// Convert value type strings to DB IDs.
	valueIDs := make([]int16, 0, len(valueTypes))
	for _, vt := range valueTypes {
		if id := a.vdCache.GetIDByValue(vt); id != 0 {
			valueIDs = append(valueIDs, id)
		}
	}

	excerpts, err := a.store.ListExcerptsByValues(userID, valueIDs, limit)
	if err != nil {
		return nil, fmt.Errorf("search excerpts by values failed. %w", err)
	}

	return convertExcerptsToSources(excerpts, a.vdCache), nil
}

// SearchByText finds excerpts by semantic similarity using embedder + pgvector.
// If valueTypes is non-empty, also filters by those labels.
func (a *excerptSearchAdapter) SearchByText(ctx context.Context, userID int64, queryText string, valueTypes []string, limit int) ([]toolimp.ExcerptSource, error) {
	if queryText == "" {
		return a.SearchByValues(ctx, userID, valueTypes, limit)
	}

	// Compute embedding for the query text.
	vector, err := a.embedder.Embed(ctx, queryText, a.embedderAPIKey)
	if err != nil {
		return nil, fmt.Errorf("embed query text failed. %w", err)
	}

	// Convert value type strings to DB IDs.
	valueIDs := make([]int16, 0, len(valueTypes))
	for _, vt := range valueTypes {
		if id := a.vdCache.GetIDByValue(vt); id != 0 {
			valueIDs = append(valueIDs, id)
		}
	}

	excerpts, err := a.store.SearchByVector(userID, vector, valueIDs, limit)
	if err != nil {
		return nil, fmt.Errorf("vector search excerpts failed. %w", err)
	}

	return convertExcerptsToSources(excerpts, a.vdCache), nil
}

// MarkAsReferenced optimistically updates last_ref_at for the given excerpt IDs.
func (a *excerptSearchAdapter) MarkAsReferenced(ctx context.Context, ids []int64) error {
	return a.store.UpdateLastRefAt(ids)
}

// Close releases any underlying resources.
func (a *excerptSearchAdapter) Close() error {
	return nil
}

// convertExcerptsToSources converts []store.Excerpt to []toolimp.ExcerptSource,
// mapping value IDs back to their string representations via the cache.
func convertExcerptsToSources(excerpts []store.Excerpt, vdCache *cache.ExcerptValueDictCache) []toolimp.ExcerptSource {
	result := make([]toolimp.ExcerptSource, 0, len(excerpts))
	for _, e := range excerpts {
		types := make([]string, 0, len(e.Values))
		for _, vid := range e.Values {
			if v := vdCache.GetValueByID(vid); v != "" {
				types = append(types, v)
			}
		}
		result = append(result, toolimp.ExcerptSource{
			ID:             e.ID,
			Content:        e.Content,
			ContextSummary: e.ContextSummary,
			Reason:         e.Reason,
			ValueTypes:     types,
			MsgTime:        e.MsgTime,
			RefCount:       e.RefCount,
			LastRefAt:      e.LastRefAt,
		})
	}
	return result
}
