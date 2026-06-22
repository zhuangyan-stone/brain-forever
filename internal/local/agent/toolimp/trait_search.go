package toolimp

import (
	"BrainForever/infra/embedder"
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ============================================================
// Personal Trait -RAG retrieval for personal knowledge base
// ============================================================

const TraitSearchToolName = "trait_search"

// TraitSource represents a personal knowledge source (RAG retrieval).
// Used for knowledge base references with similarity score.
type TraitSource struct {
	ID int64 `json:"id"`

	Trait    string `json:"trait"`
	Category string `json:"category"`

	Confidence int `json:"confidence"`

	HalfLife string `json:"half_life"`

	ChatSN    string  `json:"chat_sn"`
	ChatTitle *string `json:"chat_title"`

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

// traitSearchToolDefinition returns the ToolDefinition for traits search
// using llm types, with translated descriptions.
// One of two methods for querying personal traits:
// The LLM can directly search the user's personal trait collection by specifying
// the content it cares about (a specific question or concept) as the 'text' parameter,
// and optionally a category (1-14) as the 'category' parameter.
// See the 14 categories in: lang\remote\zh-CN\system_prompt.toml
// If category is set to 0, all categories are searched.
func traitSearchToolDefinition(lang string) llm.ToolDefinition {
	// Build the schema as a Go map and marshal it to JSON.
	// Using json.Marshal ensures the description string is properly escaped
	// (e.g., double quotes, newlines, etc.), so any translation content is safe.
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{
				"type":        "string",
				"description": i18n.Tools.TL(lang, TraitSearchToolName, "param_text_desc"),
			},
			"category": map[string]any{
				"type":        "integer",
				"description": i18n.Tools.TL(lang, TraitSearchToolName, "param_category_desc"),
			},
		},
		"required":             []string{"text"},
		"additionalProperties": false,
	}

	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal trait search tool schema: %v", err))
	}

	var paramsMap map[string]any
	if err := json.Unmarshal(schemaBytes, &paramsMap); err != nil {
		panic(fmt.Sprintf("failed to parse trait search tool schema: %v", err))
	}

	strict := true
	return llm.ToolDefinition{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        TraitSearchToolName,
			Description: i18n.Tools.TL(lang, TraitSearchToolName, "description"),
			Parameters:  paramsMap,
			Strict:      &strict,
		},
	}
}

// traitSearchArguments parses the JSON arguments from the LLM.
func traitSearchArguments(arguments string) (text string, category int, err error) {
	var result struct {
		Text     string `json:"text"`
		Category int    `json:"category"`
	}
	if err := json.Unmarshal([]byte(arguments), &result); err != nil {
		return "", 0, fmt.Errorf("json unmarshal fail. %w", err)
	}
	return result.Text, result.Category, nil
}

// TraitSearchToolImp
type TraitSearchToolImp struct {
	ctx context.Context

	def llm.ToolDefinition

	searcher TraitSearcher
	lang     string

	q        string
	category int

	// result
	Traits []TraitSource
}

// ResetTraits clears the accumulated traits.
// This should be called before a new round of tool calls begins.
func (imp *TraitSearchToolImp) ResetTraits() {
	imp.Traits = nil
}

var _ llm.ToolIMP = (*TraitSearchToolImp)(nil)

func MakeTraitSearchTool(ctx context.Context, searcher TraitSearcher, lang string) *TraitSearchToolImp {
	return &TraitSearchToolImp{
		ctx:      ctx,
		def:      traitSearchToolDefinition(lang),
		searcher: searcher,
		lang:     lang,
	}
}

func (imp *TraitSearchToolImp) GetName() string {
	return TraitSearchToolName
}

func (imp *TraitSearchToolImp) GetDefinition() llm.ToolDefinition {
	return imp.def
}

func (imp *TraitSearchToolImp) GetPendingText() string {
	return fmt.Sprintf("%s %s", i18n.Tools.TL(imp.lang, TraitSearchToolName, "pending"), imp.q)
}

func (imp *TraitSearchToolImp) SetArgument(arguments string) (err error) {
	imp.q, imp.category, err = traitSearchArguments(arguments)
	return
}

func (imp *TraitSearchToolImp) Execute() (result string, err error) {
	if imp.q == "" {
		return "", fmt.Errorf("call %s with empty query", TraitSearchToolName)
	}

	// Execute the trait search via RAG vector search
	var traits []TraitSource
	traits, err = imp.searcher.SearchByText(imp.ctx, imp.q, imp.category, 10)
	if err != nil {
		return "", err
	}

	// Accumulate results across multiple tool calls in the same thinking process
	imp.Traits = append(imp.Traits, traits...)
	return
}

type TraitSearcherIMP struct {
	embedder *embedder.Embedder
}
