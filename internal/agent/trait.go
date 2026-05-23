package agent

import (
	"context"
	"fmt"
	"log"

	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/agent/toolimp"
)

// ============================================================
// TraitAgent — dedicated to extracting user personal traits from conversation history
//
// TraitAgent uses a separate traitLLMClient, performs only Tool Calls,
// and does not return actual text replies. Currently, extraction results
// are only output to the console via log.Printf.
// ============================================================

// TraitAgent is responsible for extracting user personal traits from conversation history.
type TraitAgent struct {
	llmClient llm.Client
}

// NewTraitAgent creates a TraitAgent.
func NewTraitAgent(llmClient llm.Client) *TraitAgent {
	return &TraitAgent{llmClient: llmClient}
}

// ============================================================
// Tool parameter types
// ============================================================

// TraitsExtractedParams is the input parameters for the traits_extracted tool.
type TraitsExtractedParams struct {
	Traits []TraitItem `json:"traits"`
}

// TraitItem represents a single personal trait.
type TraitItem struct {
	Topic           string  `json:"topic"`            // The topic the user is currently discussing
	InferenceMethod string  `json:"inference_method"` // Inference mode: explicit-traits / implicit-traits
	Nature          string  `json:"nature"`           // Trait nature: objective-traits / subjectivity-traits
	Conclusion      string  `json:"conclusion"`       // A short sentence describing a trait
	Scenario        string  `json:"scenario"`         // Application scenario: casual/work/study/life/health/other
	Domain          string  `json:"domain"`           // The content domain of the trait itself
	Category        string  `json:"category"`         // Top-level category: 9 categories
	Source          string  `json:"source"`           // Source citation or summary
	Confidence      float64 `json:"confidence"`       // Confidence level 0.1 ~ 1.0
	HalfLife        string  `json:"half_life"`        // Half-life: short/medium/long
}

// ============================================================
// Core extraction methods
// ============================================================

// ExtractTraits analyzes the untraited conversation history and extracts user traits.
//
// This is a blocking call, recommended to be executed in a separate goroutine.
// Parameters:
//   - ctx: Context
//   - lang: Language code for i18n system prompt
//   - untraitedMsgs: List of messages that have not yet been processed (contiguous, in forward order)
//   - previousSummary: Summary of previously extracted traits (appended to the end of the system prompt)
//
// Current implementation: extraction results are only output to the console via log.Printf.
func (ta *TraitAgent) ExtractTraits(
	ctx context.Context,
	lang string,
	untraitedMsgs []Message,
	previousSummary string,
) error {
	// 1. Build the system prompt
	systemContent := i18n.SystemPrompt.TL(lang, "trait")
	if previousSummary != "" {
		systemContent += "\n\nPreviously extracted trait summary:\n" + previousSummary
	}

	// 2. Convert untraitedMsgs to llm.Message
	llmMsgs := make([]llm.Message, 0, 1+len(untraitedMsgs))
	llmMsgs = append(llmMsgs, llm.Message{Role: llm.RoleSystem, Content: systemContent})
	for _, m := range untraitedMsgs {
		role := m.Role
		if role != llm.RoleUser && role != llm.RoleAssistant && role != llm.RoleSystem && role != llm.RoleTool {
			continue
		}

		llmMsgs = append(llmMsgs, llm.Message{Role: role, Content: m.Content})
	}

	// 3. Build tools (currently only includes traits_extracted)
	traitsExtractedToolImp := toolimp.MakeTraitsExtractedTool(lang)
	toolsImp := []llm.ToolIMP{traitsExtractedToolImp}

	// 4. Create trait tool caller
	caller := newTraitToolCaller(toolsImp)

	// 5. Build the request, forcing tool_choice = "required"
	req := llm.ChatCompletionRequest{
		Messages: llmMsgs,
		Tools:    caller.GetToolDefines(),
		Stream:   false,
	}
	req.ForceToolChoice(toolimp.TraitsExtractedToolName)
	// Trait extraction only needs tool calls, not text reasoning;
	// disable thinking to save tokens and reduce latency.
	req.Thinking = &llm.ThinkingConfig{Type: "disabled"}

	// 6. Call LLM (non-streaming)
	resp, err := ta.llmClient.ChatWithOptions(ctx, req)
	if err != nil {
		return fmt.Errorf("trait llm call failed: %w", err)
	}

	// 7. Process tool_calls
	if len(resp.Choices) == 0 {
		log.Printf("[TraitExtract] LLM returned no choices")
		return nil
	}

	msg := resp.Choices[0].Message
	for _, tc := range msg.ToolCalls {
		// Set arguments on the tool
		if err := caller.Pending(tc.ID, tc.Function.Name, tc.Function.Arguments); err != nil {
			log.Printf("[TraitExtract] failed to set arguments for tool '%s': %v", tc.Function.Name, err)
			continue
		}
		// Execute the tool
		if _, err := caller.Call(tc.ID, tc.Function.Name); err != nil {
			log.Printf("[TraitExtract] failed to execute tool '%s': %v", tc.Function.Name, err)
			continue
		}
	}

	return nil
}
