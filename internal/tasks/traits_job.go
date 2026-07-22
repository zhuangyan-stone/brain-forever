// Package tasks provides the global background slow-task queue singleton
// and the periodic trait extraction job.
package tasks

import (
	"context"
	"fmt"
	"time"

	"BrainForever/infra/embedder"
	"BrainForever/infra/llm"
	"BrainForever/infra/zylog"
	"BrainForever/internal/agent"
	"BrainForever/internal/config"
	"BrainForever/internal/store"
	"BrainForever/toolset"
)

// ============================================================
// Registration
// ============================================================

// RegisterPeriodicTraitExtraction registers the periodic trait extraction task
// into the global bktask queue. Must be called after InitGlobal().
func RegisterPeriodicTraitExtraction(
	cfg config.TraitTaskConfig,
	chatStore *store.ChatStore,
	brainStore *store.BrainStore,
	llmClients map[string]llm.Client,
	embedderClients map[string]embedder.Embedder,
	logger zylog.Logger,
	defaultLang string,
	dedupEnabled bool,
	dedupThreshold float64,
) {
	if !cfg.Enabled {
		logger.Infof("periodic trait extraction task disabled by config")
		return
	}

	// If RunOnStartup is enabled, schedule an immediate one-shot task
	// that runs the trait scan right after registration.
	if cfg.RunOnStartup {
		err := TheBkTaskQueue().AddOneShot("trait-extraction-startup", 0, func() error {
			logger.Infof("running initial trait extraction (run_on_startup)")
			return runPeriodicTraitExtraction(&cfg, chatStore, brainStore, llmClients, embedderClients, logger, defaultLang, dedupEnabled, dedupThreshold)
		})
		if err != nil {
			logger.Errorf("failed to register startup trait extraction task. %v", err)
		} else {
			logger.Infof("startup trait extraction task registered (run_on_startup=true)")
		}
	}

	interval := time.Duration(cfg.IntervalSeconds) * time.Second
	err := TheBkTaskQueue().AddRecurring("periodic-trait-extraction", interval, func() error {
		return runPeriodicTraitExtraction(&cfg, chatStore, brainStore, llmClients, embedderClients, logger, defaultLang, dedupEnabled, dedupThreshold)
	})
	if err != nil {
		logger.Errorf("failed to register periodic trait extraction task. %v", err)
		return
	}
	logger.Infof("periodic trait extraction task registered (interval=%v, delayHours=%d, batchLimit=%d, windows=%d)",
		interval, cfg.ExtractDelayHours, cfg.BatchLimit, len(cfg.AllowedWindows))
}

// ============================================================
// Job runner
// ============================================================

// runPeriodicTraitExtraction performs one scan-and-extract cycle.
func runPeriodicTraitExtraction(
	cfg *config.TraitTaskConfig,
	chatStore *store.ChatStore,
	brainStore *store.BrainStore,
	llmClients map[string]llm.Client,
	embedderClients map[string]embedder.Embedder,
	logger zylog.Logger,
	defaultLang string,
	dedupEnabled bool,
	dedupThreshold float64,
) error {
	// 1. Check time window constraint.
	if !cfg.IsAllowedTimePoint(time.Now()) {
		logger.Debugf("trait extraction batch: skipped (outside allowed window)")
		return nil
	}

	// 2. Query eligible chats with user settings (single JOIN).
	rows, err := chatStore.ListChatsPendingTraitExtraction(cfg.ExtractDelayHours, cfg.BatchLimit)
	if err != nil {
		return fmt.Errorf("query pending chats failed. %w", err)
	}

	if len(rows) == 0 {
		logger.Debugf("trait extraction batch: no pending chats found")
		return nil
	}

	logger.Infof("trait extraction batch: processing %d pending chats", len(rows))

	// 3. Process each chat.
	for _, row := range rows {
		processChatForExtraction(row, chatStore, brainStore, llmClients, embedderClients, logger, defaultLang, dedupEnabled, dedupThreshold)
	}

	return nil
}

// ============================================================
// Single chat processing (delegates to agent's shared functions)
// ============================================================

func processChatForExtraction(
	row store.ChatPendingTraitExtraction,
	chatStore *store.ChatStore,
	brainStore *store.BrainStore,
	llmClients map[string]llm.Client,
	embedderClients map[string]embedder.Embedder,
	logger zylog.Logger,
	defaultLang string,
	dedupEnabled bool,
	dedupThreshold float64,
) {
	// 1. Parse user settings from the JOIN result.
	var userSettings store.UserSettings
	if err := userSettings.FromString(row.Settings); err != nil {
		logger.Errorf("skip chat %d: parse user settings failed. %v", row.ID, err)
		return
	}

	// 2. Determine language: prefer user setting, fall back to server default.
	lang := userSettings.Lang
	if lang == "" {
		lang = defaultLang
	}

	// 3. Resolve LLM provider and API key.
	llmAPIKey := userSettings.APIKey.LLM.ApiKey
	llmProvider := userSettings.APIKey.LLM.Provider
	if llmProvider == "" {
		llmProvider = config.GetDefaultLLMProvider()
	}
	if llmAPIKey == "" {
		pool := config.GetApiKeysPool()
		llmAPIKey = pool.GetOne("llm", llmProvider)
	}
	if llmAPIKey == "" {
		logger.Warnf("skip chat %d: no LLM API key available for provider %s", row.ID, llmProvider)
		return
	}

	// 4. Resolve Embedder provider and API key.
	embedderAPIKey := userSettings.APIKey.Embedder.ApiKey
	embedderProvider := userSettings.APIKey.Embedder.Provider
	if embedderProvider == "" {
		embedderProvider = config.GetDefaultEmbeddingProvider()
	}
	if embedderAPIKey == "" {
		pool := config.GetApiKeysPool()
		embedderAPIKey = pool.GetOne("embedding", embedderProvider)
	}
	if embedderAPIKey == "" {
		logger.Warnf("skip chat %d: no Embedder API key available for provider %s", row.ID, embedderProvider)
		return
	}

	// 5. Fetch unextracted messages for this chat.
	messages, err := chatStore.ListUnExtractMessages(row.ID)
	if err != nil {
		logger.Errorf("skip chat %d: list unextracted messages failed. %v", row.ID, err)
		return
	}

	// 6. No new messages but extracted_at is null → mark as processed to skip future scans.
	if len(messages) == 0 {
		if row.ExtractedAt == nil {
			if err := chatStore.UpdateExtractionCountAndTime(row.ID, 0); err != nil {
				logger.Errorf("update extraction time for chat %d failed. %v", row.ID, err)
			}
		}
		return
	}

	// 7. Get the LLM and Embedder client instances for the resolved providers.
	llmClient, ok := toolset.MapGet(llmClients, llmProvider)
	if !ok {
		logger.Errorf("skip chat %d: no LLM client for provider %s", row.ID, llmProvider)
		return
	}
	embedderClient, ok := toolset.MapGet(embedderClients, embedderProvider)
	if !ok {
		logger.Errorf("skip chat %d: no Embedder client for provider %s", row.ID, embedderProvider)
		return
	}

	// 8. Call agent's shared trait extraction function (no duplicate implementation).
	ctx := context.Background()
	result := agent.CallTraitsLLMStandalone(ctx, row.Title, messages, lang, llmClient, llmAPIKey)
	if result == nil {
		logger.Errorf("skip chat %d: LLM trait extraction returned nil", row.ID)
		return
	}

	// 9. Store extracted traits via agent's shared storage function.
	//    StoreTraitsStandalone internally uses store.CompleteExtraction which
	//    atomically does A (insert traits) + B (mark messages) + C (update session)
	//    in a single database transaction. No separate B/C calls needed.
	lastMsgID := messages[len(messages)-1].ID
	if len(result.Features) > 0 {
		storedCount, err := agent.StoreTraitsStandalone(ctx, result.Features, row.ID, row.UserID, lastMsgID, embedderClient, embedderAPIKey, dedupEnabled, dedupThreshold)
		if err != nil {
			logger.Errorf("store traits for chat %d failed. %v", row.ID, err)
			return
		}
		logger.Infof("trait extraction: chat %d extracted %d new traits", row.ID, storedCount)
	} else {
		logger.Debugf("trait extraction: chat %d has no new traits to extract", row.ID)
		if _, err := brainStore.AddTraits(ctx, row.ID, lastMsgID, nil); err != nil {
			logger.Errorf("mark chat %d as processed failed. %v", row.ID, err)
		}
	}
}

// ============================================================
// Helper
// ============================================================
