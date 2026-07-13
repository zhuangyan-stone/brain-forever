package agent

import (
	"BrainForever/infra/embedder"
	"BrainForever/infra/i18n"
	"BrainForever/internal/agent/toolimp"
	"BrainForever/internal/store"
	"context"
	"fmt"
)

// traitSearchAdapter adapts searcher.WebSearcher to implement the toolimp.TraitSearcher interface
type traitSearchAdapter struct {
	client embedder.Embedder
	store  *store.BrainStore
	lang   string
	userID int64 // user ID for data isolation

	// apiSetting is the user's personal embedder API setting (empty ApiKey = use client default).
	apiSetting store.ApiSetting
}

// halfLifeDisplay returns the localized half-life label for the stored integer (1-4).
// Mapping: 1=short, 2=medium, 3=long, 4=permanent.
func privacyLevelDisplay(level int) string {
	switch level {
	case 0:
		return "private"
	case 1:
		return "protected"
	case 2:
		return "public"
	default:
		return "protected"
	}
}

func halfLifeDisplay(lang string, halfLife int) string {
	key := fmt.Sprintf("trait_halflife_%d", halfLife)
	return i18n.TL(lang, key)
}

// confidenceDisplay returns a localized confidence label for the stored integer (1-10).
// Levels: 1-3=low, 4-7=medium, 8-10=high.
func confidenceDisplay(lang string, confidence int) string {
	var level string
	switch {
	case confidence >= 8:
		level = "high"
	case confidence >= 4:
		level = "medium"
	default:
		level = "low"
	}
	label := i18n.TL(lang, "trait_confidence_"+level)
	return fmt.Sprintf("%s (%d)", label, confidence)
}

// categoryDisplay converts the stored category integer (0-14) to a localized label
// combining the translated category name with its numeric ID in parentheses,
// so the LLM can easily map between display text and the integer parameter.
func categoryDisplay(lang string, cat int) string {
	key := fmt.Sprintf("trait_category_%d", cat)
	return i18n.TL(lang, key)
}

// SearchByText finds matching personal trait descriptions by the given text.
func (a *traitSearchAdapter) SearchByText(ctx context.Context, queryText string, category int, topK int) ([]toolimp.TraitSource, error) {
	// 1. Use the client (embedder.Embedder) to compute the vector for queryText
	vector, err := a.client.Embed(ctx, queryText, a.apiSetting.ApiKey)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query text. %w", err)
	}

	// 2. Call store.SearchByVector() to perform the query, get []store.PersonalTrait, then convert to []toolimp.TraitSource
	traits, err := a.store.SearchByVector(a.userID, vector, category, topK)
	if err != nil {
		return nil, fmt.Errorf("failed to search traits. %w", err)
	}

	// 3. During []store.PersonalTrait -> []toolimp.TraitSource conversion, filter out traits with similarity < 0.58
	var result []toolimp.TraitSource
	for _, pt := range traits {
		if pt.Score < 0.58 {
			continue
		}
		result = append(result, toolimp.TraitSource{
			ID:           pt.ID,
			Trait:        pt.Trait,
			Category:     categoryDisplay(a.lang, pt.Category),
			Confidence:   confidenceDisplay(a.lang, pt.Confidence),
			HalfLife:     halfLifeDisplay(a.lang, pt.HalfLife),
			PrivacyLevel: privacyLevelDisplay(pt.PrivacyLevel),
			CreateAt:     pt.CreateAt,
			UpdateAt:     pt.UpdateAt,
		})
	}

	return result, nil
}

func (a *traitSearchAdapter) SearchByKeyword(ctx context.Context, queryKeyword string, queryType int) ([]toolimp.TraitSource, error) {
	// 1. Try exact match first
	traits, err := a.store.SearchByKeyword(a.userID, queryKeyword, queryType, 20)
	if err != nil {
		return nil, fmt.Errorf("failed to search traits by keyword. %w", err)
	}

	// 2. If exact match returns no results, fall back to fuzzy LIKE %keyword% search
	if len(traits) == 0 {
		traits, err = a.store.SearchByKeywordFuzzy(a.userID, queryKeyword, queryType, 20)
		if err != nil {
			return nil, fmt.Errorf("failed to fuzzy search traits by keyword. %w", err)
		}
	}

	var result []toolimp.TraitSource
	for _, pt := range traits {
		result = append(result, toolimp.TraitSource{
			ID:           pt.ID,
			Trait:        pt.Trait,
			Category:     categoryDisplay(a.lang, pt.Category),
			Confidence:   confidenceDisplay(a.lang, pt.Confidence),
			HalfLife:     halfLifeDisplay(a.lang, pt.HalfLife),
			PrivacyLevel: privacyLevelDisplay(pt.PrivacyLevel),
			CreateAt:     pt.CreateAt,
			UpdateAt:     pt.UpdateAt,
		})
	}

	return result, nil
}

// Close releases any underlying resources held by the searcher.
func (a *traitSearchAdapter) Close() error {
	a.store.Close()
	return nil
}
