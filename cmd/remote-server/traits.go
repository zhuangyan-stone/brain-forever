package main

import (
	"BrainForever/infra/i18n"
	"BrainForever/internal/remote/agent/toolimp"
)

// ============================================================
// System prompt for trait extraction (i18n-backed)
// ============================================================

// getTraitSystemPrompt returns the localized system prompt for trip_traits extraction.
// The prompt content is stored in lang/remote/{lang}/system_prompt.toml under key "trip_trait".
func getTraitSystemPrompt(lang string) string {
	return i18n.SystemPrompt.TL(lang, "trip_trait")
}

// tripTraitsToolName is re-exported for convenience.
const tripTraitsToolName = toolimp.TripTraitsToolName
