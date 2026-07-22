package toolimp

import (
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ============================================================
// Personal Trait -RAG retrieval for personal knowledge base
// ============================================================

const TraitSearchByTextToolName = "trait_search_by_text"

// TraitSearchByKeywordToolName is the name of the tool used for keyword-based trait search.
// The LLM can call this tool when it already knows the keyword and keyword kind
// and wants to perform an exact match retrieval from the user's personal trait collection.
const TraitSearchByKeywordToolName = "trait_search_by_keyword"

// defaultTopK is the default number of top-K results returned by the trait RAG search.
const defaultTopK = 10

// TraitSource represents a personal knowledge source (RAG retrieval).
// Used for knowledge base references with similarity score.
type TraitSource struct {
	ID int64 `json:"id"`

	Trait    string `json:"trait"`
	Category string `json:"category"`

	Confidence string `json:"confidence"`

	HalfLife string `json:"half_life"`

	PrivacyLevel string `json:"privacy_level"`

	CreateAt time.Time `json:"create_at"`
	UpdateAt time.Time `json:"update_at"`
}

// TraitSearcher is the interface for searching personal traits
// from the user's knowledge base (vector search).
type TraitSearcher interface {
	SearchByText(ctx context.Context, queryText string, category int, topK int) ([]TraitSource, error)
	SearchByKeyword(ctx context.Context, queryKeyword string, queryType int) ([]TraitSource, error)

	// Close releases any underlying resources held by the searcher.
	Close() error
}

// traitSearchByKeywordToolDefinition returns the ToolDefinition for keyword-based trait search
// using llm types, with translated descriptions.
// The LLM can call this tool when it already knows the keyword and keyword kind
// and wants to perform an exact match retrieval from the user's personal trait collection.
// keyword: the exact keyword text to match
// kind: the keyword kind letter (A=Time, B=Location, C=Person, D=Thing, E=Relationship, F=Action)
func traitSearchByKeywordToolDefinition(lang string) llm.ToolDefinition {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"keyword": map[string]any{
				"type":        "string",
				"description": i18n.Tools.TL(lang, TraitSearchByKeywordToolName, "param_keyword_desc"),
			},
			"kind": map[string]any{
				"type":        "string",
				"description": i18n.Tools.TL(lang, TraitSearchByKeywordToolName, "param_kind_desc"),
			},
		},
		"required":             []string{"keyword", "kind"},
		"additionalProperties": false,
	}

	strict := true
	return llm.ToolDefinition{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        TraitSearchByKeywordToolName,
			Description: i18n.Tools.TL(lang, TraitSearchByKeywordToolName, "description"),
			Parameters:  schema,
			Strict:      &strict,
		},
	}
}

// traitSearchByTextToolDefinition returns the ToolDefinition for traits search
// using llm types, with translated descriptions.
// One of two methods for querying personal traits:
// The LLM can directly search the user's personal trait collection by specifying
// the content it cares about (a specific question or concept) as the 'text' parameter,
// and optionally a category (1-14) as the 'category' parameter.
// See the 14 categories in: lang\zh-CN\system_prompt.toml
// If category is set to 0, all categories are searched.
func traitSearchByTextToolDefinition(lang string) llm.ToolDefinition {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{
				"type":        "string",
				"description": i18n.Tools.TL(lang, TraitSearchByTextToolName, "param_text_desc"),
			},
			"category": map[string]any{
				"type":        "integer",
				"description": i18n.Tools.TL(lang, TraitSearchByTextToolName, "param_category_desc"),
			},
		},
		"required":             []string{"text", "category"},
		"additionalProperties": false,
	}

	strict := true
	return llm.ToolDefinition{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        TraitSearchByTextToolName,
			Description: i18n.Tools.TL(lang, TraitSearchByTextToolName, "description"),
			Parameters:  schema,
			Strict:      &strict,
		},
	}
}

// traitSearchByTextArguments parses the JSON arguments from the LLM tool call.
func traitSearchByTextArguments(arguments string) (text string, category int, err error) {
	var result struct {
		Text     string `json:"text"`
		Category int    `json:"category"`
	}
	if err := json.Unmarshal([]byte(arguments), &result); err != nil {
		return "", 0, fmt.Errorf("unmarshal arguments. %w", err)
	}
	return result.Text, result.Category, nil
}

// traitSearchByKeywordArguments parses the JSON arguments from the LLM tool call.
func traitSearchByKeywordArguments(arguments string) (keyword string, kind string, err error) {
	var result struct {
		Keyword string `json:"keyword"`
		Kind    string `json:"kind"`
	}
	if err := json.Unmarshal([]byte(arguments), &result); err != nil {
		return "", "", fmt.Errorf("unmarshal arguments. %w", err)
	}
	return result.Keyword, result.Kind, nil
}

// kindLetterToInt converts a keyword kind letter (A-F, case-insensitive, or '*') to its integer representation.
// A=1, B=2, C=3, D=4, E=5, F=6.
// Returns 0 for '*' (match all kinds), empty string, or if the letter is invalid.
// Use isWildcardKind to distinguish between a valid wildcard and an invalid letter.
func kindLetterToInt(kind string) int {
	if len(kind) == 0 {
		return 0
	}
	// '*' means match all kinds (no kind filter)
	if kind[0] == '*' {
		return 0
	}
	// Normalize to uppercase
	ch := kind[0]
	if ch >= 'a' && ch <= 'z' {
		ch -= 32
	}
	if ch >= 'A' && ch <= 'F' {
		return int(ch-'A') + 1
	}
	return 0
}

// isWildcardKind returns true if the kind string represents a valid wildcard
// ("*" or empty), meaning "match all keyword kinds".
func isWildcardKind(kind string) bool {
	return kind == "*" || kind == ""
}

// TraitSearchToolImpBase holds common state for trait search tool implementations.
type TraitSearchToolImpBase struct {
	ctx context.Context
	def llm.ToolDefinition

	searcher TraitSearcher
	lang     string

	// topK is the maximum number of results returned from the RAG search.
	topK int

	// result holds accumulated trait search results across multiple tool calls
	// within the same LLM reasoning cycle.
	Traits []TraitSource
}

// ResetTraits clears the accumulated traits.
// This should be called before a new round of tool calls begins.
func (imp *TraitSearchToolImpBase) ResetTraits() {
	imp.Traits = nil
}

// TraitSearchByTextToolImp
type TraitSearchByTextToolImp struct {
	TraitSearchToolImpBase

	q        string
	category int
}

type TraitSearchByKeywordToolImp struct {
	TraitSearchToolImpBase

	keyword string
	kind    string
}

var _ llm.ToolIMP = (*TraitSearchByTextToolImp)(nil)
var _ llm.ToolIMP = (*TraitSearchByKeywordToolImp)(nil)

// MakeTraitSearchByKeywordTool creates a new TraitSearchByKeywordToolImp.
// It panics if searcher is nil (fail-fast for missing dependencies).
func MakeTraitSearchByKeywordTool(ctx context.Context, searcher TraitSearcher, lang string) *TraitSearchByKeywordToolImp {
	if searcher == nil {
		panic(fmt.Sprintf("%s: searcher must not be nil", TraitSearchByKeywordToolName))
	}
	return &TraitSearchByKeywordToolImp{
		TraitSearchToolImpBase: TraitSearchToolImpBase{
			ctx:      ctx,
			def:      traitSearchByKeywordToolDefinition(lang),
			searcher: searcher,
			lang:     lang,
			topK:     defaultTopK,
		},
	}
}

// MakeTraitSearchByTextTool creates a new TraitSearchByTextToolImp.
// It panics if searcher is nil (fail-fast for missing dependencies).
// Use WithTopK to configure a non-default result count.
func MakeTraitSearchByTextTool(ctx context.Context, searcher TraitSearcher, lang string) *TraitSearchByTextToolImp {
	if searcher == nil {
		panic(fmt.Sprintf("%s: searcher must not be nil", TraitSearchByTextToolName))
	}
	return &TraitSearchByTextToolImp{
		TraitSearchToolImpBase: TraitSearchToolImpBase{
			ctx:      ctx,
			def:      traitSearchByTextToolDefinition(lang),
			searcher: searcher,
			lang:     lang,
			topK:     defaultTopK,
		},
	}
}

func (imp *TraitSearchByTextToolImp) GetName() string {
	return TraitSearchByTextToolName
}

func (imp *TraitSearchByTextToolImp) GetDefinition() llm.ToolDefinition {
	return imp.def
}

func (imp *TraitSearchByTextToolImp) GetPendingText() string {
	return fmt.Sprintf("%s %s", i18n.Tools.TL(imp.lang, TraitSearchByTextToolName, "pending"), imp.q)
}

func (imp *TraitSearchByTextToolImp) SetArgument(arguments string) (err error) {
	imp.q, imp.category, err = traitSearchByTextArguments(arguments)
	if err != nil {
		return fmt.Errorf("%s. %w", i18n.TL(imp.lang, "trait_search_error_unmarshal_args", nil), err)
	}
	return
}

func (imp *TraitSearchByTextToolImp) Execute() (result string, err error) {
	if imp.q == "" {
		return "", errors.New(i18n.TL(imp.lang, "trait_search_error_empty_query", nil))
	}
	if imp.searcher == nil {
		return "", errors.New(i18n.TL(imp.lang, "trait_search_error_searcher_not_init", nil))
	}

	var traits []TraitSource
	traits, err = imp.searcher.SearchByText(imp.ctx, imp.q, imp.category, imp.topK)
	if err != nil {
		return "", fmt.Errorf("%s. %w", i18n.TL(imp.lang, "trait_search_error_search_failed", nil), err)
	}

	// Accumulate results across multiple tool calls in the same reasoning cycle.
	imp.Traits = append(imp.Traits, traits...)

	result = formatTraitSources(imp.lang, traits)
	return
}

func (imp *TraitSearchByKeywordToolImp) GetName() string {
	return TraitSearchByKeywordToolName
}

func (imp *TraitSearchByKeywordToolImp) GetDefinition() llm.ToolDefinition {
	return imp.def
}

func (imp *TraitSearchByKeywordToolImp) GetPendingText() string {
	return fmt.Sprintf("%s %s(%s)", i18n.Tools.TL(imp.lang, TraitSearchByKeywordToolName, "pending"), imp.keyword, imp.kind)
}

func (imp *TraitSearchByKeywordToolImp) SetArgument(arguments string) (err error) {
	imp.keyword, imp.kind, err = traitSearchByKeywordArguments(arguments)
	if err != nil {
		return fmt.Errorf("%s. %w", i18n.TL(imp.lang, "trait_search_error_unmarshal_args", nil), err)
	}
	return
}

func (imp *TraitSearchByKeywordToolImp) Execute() (result string, err error) {
	if imp.keyword == "" {
		return "", errors.New(i18n.TL(imp.lang, "trait_search_error_empty_keyword", nil))
	}
	if imp.searcher == nil {
		return "", errors.New(i18n.TL(imp.lang, "trait_search_error_searcher_not_init", nil))
	}

	// Convert kind letter (A-F/*) to integer for the searcher interface.
	// kindLetterToInt returns:
	//   - 1-6 for valid letters A-F
	//   - 0 for "*" (match all kinds, no filter) or empty string
	//   - 0 also for truly invalid letters — we distinguish by checking the raw string.
	kindInt := kindLetterToInt(imp.kind)
	if kindInt == 0 && !isWildcardKind(imp.kind) {
		return "", errors.New(i18n.TL(imp.lang, "trait_search_error_invalid_kind_letter", map[string]any{"Kind": imp.kind}))
	}

	var traits []TraitSource
	traits, err = imp.searcher.SearchByKeyword(imp.ctx, imp.keyword, kindInt)
	if err != nil {
		return "", fmt.Errorf("%s. %w", i18n.TL(imp.lang, "trait_search_error_search_failed", nil), err)
	}

	// Accumulate results across multiple tool calls in the same reasoning cycle.
	imp.Traits = append(imp.Traits, traits...)

	result = formatTraitSources(imp.lang, traits)
	return
}

// formatTraitSources formats a slice of TraitSource into a human-readable string
// that can be returned to the LLM as the tool call result.
func formatTraitSources(lang string, traits []TraitSource) string {
	if len(traits) == 0 {
		return i18n.TL(lang, "trait_search_error_no_results", nil)
	}

	var b strings.Builder
	b.Grow(len(traits) * 256) // Pre-allocate approximate capacity

	for i, t := range traits {
		b.WriteString(fmt.Sprintf("%d. ", i+1))
		b.WriteString(fmt.Sprintf("Trait: %s | ", t.Trait))
		b.WriteString(fmt.Sprintf("Category: %s | ", t.Category))
		b.WriteString(fmt.Sprintf("Confidence: %s | ", t.Confidence))
		b.WriteString(fmt.Sprintf("HalfLife: %s | ", t.HalfLife))
		b.WriteString(fmt.Sprintf("PrivacyLevel: %s | ", t.PrivacyLevel))
		if !t.CreateAt.IsZero() {
			b.WriteString(fmt.Sprintf("Created: %s", t.CreateAt.Format("2006-01-02 15:04:05")))
		} else {
			b.WriteString("Created: N/A")
		}
		b.WriteByte('\n')
	}

	return b.String()
}
