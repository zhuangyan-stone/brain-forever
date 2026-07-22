// Package tasks provides the global background slow-task queue singleton
// and the periodic excerpt generation job.
package tasks

import (
	"context"
	"time"

	"BrainForever/infra/embedder"
	"BrainForever/infra/llm"
	"BrainForever/infra/zylog"
	"BrainForever/internal/agent"
	"BrainForever/internal/config"
	"BrainForever/internal/store"
	"BrainForever/internal/store/cache"
	"BrainForever/toolset"
)

// ============================================================
// Global ExcerptValueDictCache (injected at registration time)
// ============================================================

var excerptVDCache *cache.ExcerptValueDictCache

// ============================================================
// Registration
// ============================================================

// RegisterPeriodicExcerptGeneration registers the periodic excerpt extraction
// task into the global bktask queue. Must be called after InitGlobal().
func RegisterPeriodicExcerptGeneration(
	cfg config.ExcerptTaskConfig,
	excerptStore *store.ExcerptStore,
	llmClients map[string]llm.Client,
	embedderClients map[string]embedder.Embedder,
	defaultLang string,
	defaultEmbedderProvider string,
	vdCache *cache.ExcerptValueDictCache,
	logger zylog.Logger,
) {
	if !cfg.Enabled {
		logger.Infof("✓ periodic excerpt generation task disabled by config")
		return
	}

	// Store the value dict cache globally for use in the task runner.
	excerptVDCache = vdCache

	// If RunOnStartup is enabled, schedule an immediate one-shot task
	// that runs the excerpt scan right after registration.
	if cfg.RunOnStartup {
		err := TheBkTaskQueue().AddOneShot("excerpt-generation-startup", 0, func() error {
			logger.Infof("running initial excerpt generation (run_on_startup)")
			return runPeriodicExcerptGeneration(&cfg, excerptStore, llmClients, embedderClients, defaultLang, defaultEmbedderProvider, logger)
		})
		if err != nil {
			logger.Errorf("failed to register startup excerpt generation task. %v", err)
		} else {
			logger.Infof("✓ startup excerpt generation task registered (run_on_startup=true)")
		}
	}

	interval := time.Duration(cfg.IntervalSeconds) * time.Second
	err := TheBkTaskQueue().AddRecurring("periodic-excerpt-generation", interval, func() error {
		return runPeriodicExcerptGeneration(&cfg, excerptStore, llmClients, embedderClients, defaultLang, defaultEmbedderProvider, logger)
	})
	if err != nil {
		logger.Errorf("failed to register periodic excerpt generation task. %v", err)
		return
	}
	logger.Infof("✓ periodic excerpt generation task registered (interval=%v, batchLimit=%d, windows=%d, delayHours=%d)",
		interval, cfg.BatchLimit, len(cfg.AllowedWindows), cfg.ExtractDelayHours)
}

// ============================================================
// Job runner
// ============================================================

// runPeriodicExcerptGeneration performs one scan-and-generate cycle.
func runPeriodicExcerptGeneration(
	cfg *config.ExcerptTaskConfig,
	excerptStore *store.ExcerptStore,
	llmClients map[string]llm.Client,
	embedderClients map[string]embedder.Embedder,
	defaultLang string,
	defaultEmbedderProvider string,
	logger zylog.Logger,
) error {
	// 1. Check time window constraint.
	if !cfg.IsAllowedTimePoint(time.Now()) {
		logger.Debugf("excerpt generation batch: skipped (outside allowed window)")
		return nil
	}

	// 2. Query eligible chats with user settings.
	rows, err := excerptStore.ListChatsPendingExcerpt(cfg.ExtractDelayHours, cfg.BatchLimit)
	if err != nil {
		logger.Errorf("query pending excerpt chats failed. %v", err)
		return err
	}

	if len(rows) == 0 {
		logger.Debugf("excerpt generation batch: no pending chats found")
		return nil
	}

	logger.Infof("excerpt generation batch: processing %d pending chats", len(rows))

	// 3. Process each chat.
	for _, row := range rows {
		processChatForExcerpt(row, excerptStore, llmClients, embedderClients, defaultLang, defaultEmbedderProvider, logger)
	}

	return nil
}

// ============================================================
// Single chat processing
// ============================================================

func processChatForExcerpt(
	row store.ChatPendingExcerpt,
	excerptStore *store.ExcerptStore,
	llmClients map[string]llm.Client,
	embedderClients map[string]embedder.Embedder,
	defaultLang string,
	defaultEmbedderProvider string,
	logger zylog.Logger,
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

	// 4. Fetch messages: incremental if lastMsgID > 0, else full.
	chatStore := agent.GetChatStore()
	const contextWindow = 5 // include 5 preceding messages for context
	messages, err := chatStore.ListMessagesAfter(row.ID, row.LastMsgID, contextWindow)
	if err != nil {
		logger.Errorf("skip chat %d: list messages failed. %v", row.ID, err)
		return
	}
	if len(messages) == 0 {
		// No messages — still mark as processed to avoid re-scanning.
		if err := excerptStore.UpsertExcerptProgress(row.ID, row.LastMsgID); err != nil {
			logger.Errorf("upsert excerpt progress for chat %d failed. %v", row.ID, err)
		}
		return
	}

	// Compute the max message ID in this batch for progress tracking.
	maxMsgID := messages[len(messages)-1].ID
	// If we already processed up to lastMsgID and no new messages beyond it, skip.
	if row.LastMsgID > 0 && maxMsgID <= row.LastMsgID {
		logger.Debugf("excerpt generation: chat %d has no new messages beyond %d", row.ID, row.LastMsgID)
		if err := excerptStore.UpsertExcerptProgress(row.ID, row.LastMsgID); err != nil {
			logger.Errorf("upsert excerpt progress for chat %d failed. %v", row.ID, err)
		}
		return
	}

	// 5. Get the LLM client instance for the resolved provider.
	llmClient, ok := toolset.MapGet(llmClients, llmProvider)
	if !ok {
		logger.Errorf("skip chat %d: no LLM client for provider %s", row.ID, llmProvider)
		return
	}

	// 6. Resolve embedder for generating excerpt embeddings.
	embProvider := userSettings.APIKey.Embedder.Provider
	if embProvider == "" {
		embProvider = defaultEmbedderProvider
	}
	embAPIKey := userSettings.APIKey.Embedder.ApiKey
	embClient, ok := toolset.MapGet(embedderClients, embProvider)
	if !ok {
		logger.Warnf("skip chat %d: no embedder client for provider %s, excerpts will be stored without vectors", row.ID, embProvider)
		embClient = nil
	}

	// 7. Call LLM for excerpt extraction.
	ctx := context.Background()
	result := agent.CallExcerptLLMStandalone(ctx, row.Title, messages, lang, llmClient, llmAPIKey)
	if result == nil || len(result.Excerpts) == 0 {
		logger.Debugf("excerpt generation: chat %d has no excerpts to store", row.ID)
		if err := excerptStore.UpsertExcerptProgress(row.ID, maxMsgID); err != nil {
			logger.Errorf("upsert excerpt progress for chat %d failed. %v", row.ID, err)
		}
		return
	}

	// 8. Build a map of msg_id -> CreateAt for quick lookup.
	msgTimeMap := make(map[int64]time.Time, len(messages))
	for _, m := range messages {
		msgTimeMap[m.ID] = m.CreateAt
	}

	// 9. Process each excerpt: insert DB record, generate embedding, store vector.
	storedCount := 0
	for _, item := range result.Excerpts {
		// Skip excerpts from already-processed messages (incremental mode).
		if row.LastMsgID > 0 && item.MsgID <= row.LastMsgID {
			continue
		}

		// Truncate string fields to fit DB column limits.
		agent.TruncateExcerptItem(&item)
		valueIDs := resolveValueTypeIDs(item.ValueTypes)
		if len(valueIDs) == 0 {
			continue
		}
		msgTime := msgTimeMap[item.MsgID]

		insertion := &store.ExcerptInsertion{
			UserID:         row.UserID,
			ChatID:         row.ID,
			MsgID:          item.MsgID,
			MsgTime:        msgTime,
			Values:         valueIDs,
			Content:        item.ExcerptText,
			ContextSummary: item.ContextSummary,
			Reason:         item.Reason,
		}

		// Insert the excerpt record.
		excerpt, err := excerptStore.InsertExcerpt(insertion)
		if err != nil {
			logger.Errorf("store excerpt for chat %d failed. %v", row.ID, err)
			continue
		}
		storedCount++

		// Generate and store embedding vector if embedder is available.
		if embClient != nil {
			embeddingText := item.ExcerptText + " " + item.ContextSummary
			vector, err := embClient.Embed(ctx, embeddingText, embAPIKey)
			if err != nil {
				logger.Errorf("embed excerpt %d failed. %v", excerpt.ID, err)
				continue
			}
			if err := excerptStore.InsertExcerptVector(excerpt.ID, vector); err != nil {
				logger.Errorf("store vector for excerpt %d failed. %v", excerpt.ID, err)
			}
		}
	}

	if storedCount > 0 {
		logger.Infof("excerpt generation: chat %d extracted %d new excerpts (last_msg_id=%d)", row.ID, storedCount, maxMsgID)
	}

	// 10. Mark the chat as processed with the max message ID.
	if err := excerptStore.UpsertExcerptProgress(row.ID, maxMsgID); err != nil {
		logger.Errorf("upsert excerpt progress for chat %d failed. %v", row.ID, err)
	}
}

// ============================================================
// Helper: resolve value type strings to DB IDs
// ============================================================

// resolveValueTypeIDs converts LLM-returned value type strings (e.g. ["insight", "literary"])
// to their corresponding SMALLINT IDs from the excerpt_value_dict table.
// Unknown values are silently skipped.
func resolveValueTypeIDs(valueTypes []string) []int16 {
	if excerptVDCache == nil {
		return nil
	}
	ids := make([]int16, 0, len(valueTypes))
	for _, vt := range valueTypes {
		id := excerptVDCache.GetIDByValue(vt)
		if id != 0 {
			ids = append(ids, id)
		}
	}
	return ids
}
