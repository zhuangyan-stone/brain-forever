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
// It injects the current local time into the {{.CurrentLocalTime}} template placeholder
// within the system prompt (e.g., "最后，当前系统的本地时间为：2026-06-19 22:52:00 (CST)").
func getTraitSystemPrompt(lang string) string {
	return i18n.SystemPrompt.TL(lang, "trip_trait", map[string]interface{}{
		"CurrentLocalTime": time.Now().In(time.Local).Format("2006-01-02 15:04:05 (MST)"),
	})
}

// tripTraitsToolName is re-exported for convenience.
const tripTraitsToolName = toolimp.TripTraitsToolName
