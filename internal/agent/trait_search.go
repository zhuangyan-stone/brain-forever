package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
)

// ============================================================
// Personal Trait — RAG retrieval for personal knowledge base
// ============================================================

// TraitSearcher is the interface for searching personal traits
// from the user's knowledge base (vector search).
type TraitSearcher interface {
	SearchByText(ctx context.Context, queryText string, topK int) ([]TraitSource, error)
}

// injectTraitToSystemPrompt appends RAG knowledge context to the given system message.
// It modifies the message in-place (no return value).
func injectTraitToSystemPrompt(systemMsg *openai.ChatCompletionSystemMessageParam, sources []TraitSource) {
	if len(sources) == 0 {
		return
	}

	var sb strings.Builder
	sb.WriteString("\n\n---\nThe following is content related to the user, for your reference when answering. If it is irrelevant to the question, it can be ignored:\n")
	for i, src := range sources {
		sb.WriteString(fmt.Sprintf("\n[Knowledge %d] Title: %s\n", i+1, src.Title))
		sb.WriteString(fmt.Sprintf("Content: %s\n", src.Content))
	}

	// Append to the system message content
	if systemMsg.Content.OfString.Valid() {
		systemMsg.Content.OfString.Value += sb.String()
	}
}
