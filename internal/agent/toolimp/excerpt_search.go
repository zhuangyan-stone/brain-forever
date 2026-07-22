package toolimp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
)

// ============================================================
// excerpts_search -Tool for retrieving user quote excerpts
//
// excerpts_search supports two retrieval modes:
//  1. Value-based filtering: filter by value_types (e.g., "insight", "humor")
//     Uses GIN index on excerpts.values — zero additional cost.
//  2. Semantic search: provide a natural language query text.
//     Uses pgvector similarity search via embedder.
//
// LLM can combine both: value_types + query for hybrid retrieval.
// ============================================================

// ExcerptSearchToolName is the name of the excerpt search tool.
const ExcerptSearchToolName = "excerpts_search"

// defaultExcerptTopK is the default number of results returned.
const defaultExcerptTopK = 5

// maxExcerptTopK is the maximum number of results per query.
const maxExcerptTopK = 10

// ExcerptSource represents a single excerpt entry returned to the LLM.
type ExcerptSource struct {
	ID             int64      `json:"id"`
	Content        string     `json:"content"`
	ContextSummary string     `json:"context_summary"`
	Reason         string     `json:"reason"`
	ValueTypes     []string   `json:"value_types"`
	MsgTime        time.Time  `json:"msg_time"`
	RefCount       int        `json:"ref_count"`   // number of times referenced
	LastRefAt      *time.Time `json:"last_ref_at"` // last referenced time (nil = never)
}

// ExcerptSearcher is the interface for searching user quote excerpts.
type ExcerptSearcher interface {
	// SearchByValues finds excerpts by value type labels.
	// valueTypes: list of value type strings (e.g., ["insight", "humor"]).
	// Empty list means no filter — return most recent excerpts.
	SearchByValues(ctx context.Context, userID int64, valueTypes []string, limit int) ([]ExcerptSource, error)

	// SearchByText finds excerpts by semantic similarity to queryText.
	// If valueTypes is non-empty, also filters by those labels.
	SearchByText(ctx context.Context, userID int64, queryText string, valueTypes []string, limit int) ([]ExcerptSource, error)

	// MarkAsReferenced optimistically updates last_ref_at for the given excerpt IDs.
	MarkAsReferenced(ctx context.Context, ids []int64) error

	// Close releases any underlying resources.
	Close() error
}

// ExcerptSearchParams matches the tool call arguments from the LLM.
type ExcerptSearchParams struct {
	ValueTypes []string `json:"value_types"`
	Query      string   `json:"query"`
	Limit      int      `json:"limit"`
}

// SearchExcerptToolImp implements llm.ToolIMP for the excerpts_search tool.
type SearchExcerptToolImp struct {
	ctx      context.Context
	def      llm.ToolDefinition
	searcher ExcerptSearcher
	lang     string
	userID   int64
	params   ExcerptSearchParams

	// lastResults holds excerpts from the most recent query execution,
	// used for optimistic last_ref_at updates.
	lastResults []ExcerptSource
}

// Compile-time interface check.
var _ llm.ToolIMP = (*SearchExcerptToolImp)(nil)

// searchExcerptsToolDefinition builds the ToolDefinition with localized descriptions.
func searchExcerptsToolDefinition(lang string) llm.ToolDefinition {
	strict := true

	return llm.ToolDefinition{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        ExcerptSearchToolName,
			Strict:      &strict,
			Description: i18n.Tools.TL(lang, ExcerptSearchToolName, "description"),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"value_types": map[string]any{
						"type":        "array",
						"description": i18n.Tools.TL(lang, ExcerptSearchToolName, "param_value_types_desc"),
						"items": map[string]any{
							"type": "string",
							"enum": []string{
								"insight", "humor", "vent", "methodology", "rule",
								"confession", "nostalgia", "regret", "self_discovery",
								"conviction", "touching", "deed", "privacy", "literary",
							},
						},
					},
					"query": map[string]any{
						"type":        "string",
						"description": i18n.Tools.TL(lang, ExcerptSearchToolName, "param_query_desc"),
					},
					"limit": map[string]any{
						"type":        "number",
						"description": i18n.Tools.TL(lang, ExcerptSearchToolName, "param_limit_desc"),
					},
				},
				"required":             []string{"value_types", "query", "limit"},
				"additionalProperties": false,
			},
		},
	}
}

// MakeExcerptSearchTool creates a new SearchExcerptToolImp.
// Panics if searcher is nil (fail-fast for missing dependencies).
func MakeExcerptSearchTool(ctx context.Context, searcher ExcerptSearcher, lang string) *SearchExcerptToolImp {
	if searcher == nil {
		panic(fmt.Sprintf("%s: searcher must not be nil", ExcerptSearchToolName))
	}
	return &SearchExcerptToolImp{
		ctx:      ctx,
		def:      searchExcerptsToolDefinition(lang),
		searcher: searcher,
		lang:     lang,
	}
}

// WithUserID sets the user ID for data isolation.
func (imp *SearchExcerptToolImp) WithUserID(userID int64) *SearchExcerptToolImp {
	imp.userID = userID
	return imp
}

// GetName returns the tool name.
func (imp *SearchExcerptToolImp) GetName() string { return ExcerptSearchToolName }

// GetDefinition returns the tool definition for the LLM.
func (imp *SearchExcerptToolImp) GetDefinition() llm.ToolDefinition { return imp.def }

// SetArgument parses and stores the JSON arguments from the LLM tool call.
func (imp *SearchExcerptToolImp) SetArgument(arguments string) error {
	if err := json.Unmarshal([]byte(arguments), &imp.params); err != nil {
		return fmt.Errorf("parse excerpts_search arguments failed. %w", err)
	}
	return nil
}

// GetPendingText returns a human-readable description shown while the tool is pending.
func (imp *SearchExcerptToolImp) GetPendingText() string {
	return i18n.Tools.TL(imp.lang, ExcerptSearchToolName, "pending")
}

// Execute performs the excerpt search and returns formatted results for the LLM.
func (imp *SearchExcerptToolImp) Execute() (string, error) {
	if imp.searcher == nil {
		return "", errors.New(i18n.TL(imp.lang, "excerpt_search_error_searcher_not_init", nil))
	}

	// Require at least one filtering criterion.
	hasValueTypes := len(imp.params.ValueTypes) > 0
	hasQuery := imp.params.Query != ""
	if !hasValueTypes && !hasQuery {
		return "", errors.New(i18n.TL(imp.lang, "excerpt_search_error_no_criteria", nil))
	}

	limit := imp.params.Limit
	if limit <= 0 {
		limit = defaultExcerptTopK
	}
	if limit > maxExcerptTopK {
		limit = maxExcerptTopK
	}

	var excerpts []ExcerptSource
	var err error

	if hasQuery {
		// Semantic search mode (optionally combined with value_types filter).
		excerpts, err = imp.searcher.SearchByText(imp.ctx, imp.userID, imp.params.Query, imp.params.ValueTypes, limit)
	} else {
		// Value-based filtering mode (zero-cost, requires non-empty value_types).
		excerpts, err = imp.searcher.SearchByValues(imp.ctx, imp.userID, imp.params.ValueTypes, limit)
	}

	if err != nil {
		return "", fmt.Errorf("%s. %w", i18n.TL(imp.lang, "excerpt_search_error_search_failed", nil), err)
	}

	imp.lastResults = excerpts

	// Optimistically mark as referenced to promote diversity on subsequent searches.
	if len(excerpts) > 0 {
		ids := make([]int64, 0, len(excerpts))
		for _, e := range excerpts {
			ids = append(ids, e.ID)
		}
		_ = imp.searcher.MarkAsReferenced(imp.ctx, ids)
	}

	return formatExcerptSources(imp.lang, excerpts), nil
}

// GetLastResults returns the excerpts from the most recent query execution.
func (imp *SearchExcerptToolImp) GetLastResults() []ExcerptSource {
	return imp.lastResults
}

// formatExcerptSources formats a slice of ExcerptSource into a human-readable string
// that can be returned to the LLM as the tool call result.
func formatExcerptSources(lang string, excerpts []ExcerptSource) string {
	if len(excerpts) == 0 {
		return i18n.TL(lang, "excerpt_search_no_results", nil)
	}

	var b strings.Builder
	b.Grow(len(excerpts) * 512)

	for i, e := range excerpts {
		var lastRef string
		if e.LastRefAt != nil {
			lastRef = i18n.Tools.TL(lang, ExcerptSearchToolName, "format_last_ref",
				map[string]any{"Date": e.LastRefAt.Format("2006-01-02")})
		} else {
			lastRef = i18n.Tools.TL(lang, ExcerptSearchToolName, "format_never_ref")
		}

		line := i18n.Tools.TL(lang, ExcerptSearchToolName, "format_item", map[string]any{
			"Index":   i + 1,
			"Content": e.Content,
			"Context": e.ContextSummary,
			"Tags":    strings.Join(e.ValueTypes, ", "),
			"Refs":    e.RefCount,
			"LastRef": lastRef,
			"Reason":  e.Reason,
		})
		b.WriteString(line)
		b.WriteByte('\n')
	}

	return b.String()
}
