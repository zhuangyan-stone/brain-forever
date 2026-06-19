package main

import (
	"time"

	"BrainForever/infra/i18n"
	"BrainForever/internal/remote/agent/toolimp"
)

// ============================================================
// System prompt for trait extraction (i18n-backed)
// ============================================================

// getTraitSystemPrompt returns the localized system prompt for trip_traits extraction.
// The prompt content is stored in lang/remote/{lang}/system_prompt.toml under key "trip_trait".
// It injects the current local time and chat title into the {{.CurrentLocalTime}} and {{.ChatTitle}}
// template placeholders within the system prompt.
func getTraitSystemPrompt(lang string, chatTitle string) string {
	return i18n.SystemPrompt.TL(lang, "trip_trait", map[string]interface{}{
		"CurrentLocalTime": time.Now().In(time.Local).Format("2006-01-02 15:04:05 (MST)"),
		"ChatTitle":        chatTitle,
	})
}

// tripTraitsToolName is re-exported for convenience.
const tripTraitsToolName = toolimp.TripTraitsToolName
