package agent

import (
	"BrainForever/infra/embedder"
	"BrainForever/internal/local/agent/toolimp"
	"BrainForever/internal/local/store"
	"context"
	"fmt"
)

// traitSearchAdapter adapts searcher.WebSearcher to implement the toolimp.TraitSearcher interface
type traitSearchAdapter struct {
	client embedder.Embedder
	store  *store.VectorStore
}

// halfLifeDisplay converts the stored half-life integer (1-4) back to its original
// English enum label. Mapping: 1=short, 2=medium, 3=long, 4=permanent.
func halfLifeDisplay(halfLife int) string {
	switch halfLife {
	case 1:
		return "short"
	case 2:
		return "medium"
	case 3:
		return "long"
	case 4:
		return "permanent"
	default:
		return "medium"
	}
}

// categoryDisplay converts the stored category integer (1-14) to a label
// combining the English category name with its numeric ID in parentheses,
// so the LLM can easily map between display text and the integer parameter.
func categoryDisplay(cat int) string {
	switch cat {
	case 1:
		return "Demographic Attributes (1)"
	case 2:
		return "External Objective Facts (2)"
	case 3:
		return "Cultural Attainment (3)"
	case 4:
		return "Hobbies (4)"
	case 5:
		return "Abilities/Skills (5)"
	case 6:
		return "Preferences/Idiosyncrasies (6)"
	case 7:
		return "Behavioral Habits (7)"
	case 8:
		return "Health & Illness (8)"
	case 9:
		return "Situational States (9)"
	case 10:
		return "Personality/Character Traits (10)"
	case 11:
		return "Values/Beliefs (11)"
	case 12:
		return "Social Relationships (12)"
	case 13:
		return "Life Experiences (13)"
	case 14:
		return "Goals/Motivations (14)"
	default:
		return "Unspecified (0)"
	}
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
			Category:   categoryDisplay(pt.Category),
			Confidence: pt.Confidence,
			HalfLife:   halfLifeDisplay(pt.HalfLife),
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
			Category:   categoryDisplay(pt.Category),
			Confidence: pt.Confidence,
			HalfLife:   halfLifeDisplay(pt.HalfLife),
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
