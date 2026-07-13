package user

import (
	"encoding/json"
	"net/http"

	"BrainForever/internal/config"
	"BrainForever/internal/session"
	"BrainForever/internal/store"
	"BrainForever/internal/store/cache"
)

// ============================================================
// API-Key settings handlers — GET + POST /api/user/settings/apikey
// ============================================================

// OnGetApiKeySettings handles GET /api/user/settings/apikey.
// It reads the user's current API key settings from the session,
// desensitizes them, and returns to the frontend for display.
// Authentication required — the caller should wrap this with RequireAuth.
func (h *Handler) OnGetApiKeySettings(w http.ResponseWriter, r *http.Request) {
	sessionID := session.ResolveSessionID(w, r, h.cookieName)
	sess := h.sessionManager.GetOrCreate(sessionID)

	if sess.User.ID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Copy session's API key settings (value type, safe to modify)
	apiKeys := sess.User.Settings.APIKey

	// Desensitize before sending to frontend:
	//   - Private=true,  key!="" → replace with "****"
	//   - Private=true,  key=="" → keep empty (not yet set)
	//   - Private=false          → clear to "" (system key, not exposed)
	apiKeys.LLM.Desensitize()
	apiKeys.Embedder.Desensitize()
	apiKeys.Search.Desensitize()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(apiKeys)
}

// OnSaveApiKeySettings handles POST /api/user/settings/apikey.
// It saves the user's API key settings (LLM, Search, Embedder) to the database.
// Authentication required — the caller should wrap this with RequireAuth.
func (h *Handler) OnSaveApiKeySettings(w http.ResponseWriter, r *http.Request) {
	sessionID := session.ResolveSessionID(w, r, h.cookieName)
	sess := h.sessionManager.GetOrCreate(sessionID)

	if sess.User.ID == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var apis store.UserSettingsAPIKey
	if err := json.NewDecoder(r.Body).Decode(&apis); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// ============================================================
	// Restore real keys for unchanged Private services.
	//
	// The frontend sends desensitized values ("****") for Private keys
	// that the user did not modify. If we saved "****" to the session
	// or database, the real key would be lost forever.
	//
	// For each Private service where the incoming api_key is starified
	// AND the session already has a real key, we restore the session's
	// real key into the incoming data before any further processing.
	// ============================================================
	sess.Mu.Lock()
	existing := sess.User.Settings.APIKey
	sess.Mu.Unlock()

	if apis.LLM.Private && apis.LLM.IsPseudo() && existing.LLM.ApiKey != "" {
		apis.LLM.ApiKey = existing.LLM.ApiKey
	}
	if apis.Embedder.Private && apis.Embedder.IsPseudo() && existing.Embedder.ApiKey != "" {
		apis.Embedder.ApiKey = existing.Embedder.ApiKey
	}
	if apis.Search.Private && apis.Search.IsPseudo() && existing.Search.ApiKey != "" {
		apis.Search.ApiKey = existing.Search.ApiKey
	}

	// ============================================================
	// Persist to database — strip system keys before saving.
	// Private=false services have their ApiKey cleared so system pool
	// keys are never written to the database.
	// ============================================================
	dbApis := apis
	if !dbApis.LLM.Private {
		dbApis.LLM.ApiKey = ""
	}
	if !dbApis.Embedder.Private {
		dbApis.Embedder.ApiKey = ""
	}
	if !dbApis.Search.Private {
		dbApis.Search.ApiKey = ""
	}
	if err := store.TheUserStore().UpdateUserSettingsAPIKey(sess.User.ID, &dbApis); err != nil {
		h.logger.Errorf("failed to save API key settings for user %d: %v", sess.User.ID, err)
		http.Error(w, "failed to save settings", http.StatusInternalServerError)
		return
	}

	// ============================================================
	// Fill system-shared API keys for non-private services (in-memory only).
	// This ensures the session always has a usable key without writing
	// system keys to the database.
	// ============================================================
	pool := config.GetApiKeysPool()
	if !apis.LLM.Private && apis.LLM.ApiKey == "" {
		if apis.LLM.Provider == "" {
			apis.LLM.Provider = config.GetDefaultLLMProvider()
		}
		if k := pool.GetOne("llm", apis.LLM.Provider); k != "" {
			apis.LLM.ApiKey = k
		}
	}
	if !apis.Embedder.Private && apis.Embedder.ApiKey == "" {
		if apis.Embedder.Provider == "" {
			apis.Embedder.Provider = config.GetDefaultEmbeddingProvider()
		}
		if k := pool.GetOne("embedding", apis.Embedder.Provider); k != "" {
			apis.Embedder.ApiKey = k
		}
	}
	if !apis.Search.Private && apis.Search.ApiKey == "" {
		if apis.Search.Provider == "" {
			apis.Search.Provider = config.GetDefaultWebSearchProvider()
		}
		if k := pool.GetOne("websearch", apis.Search.Provider); k != "" {
			apis.Search.ApiKey = k
		}
	}

	// Update the session's in-memory settings (with restored/injected keys)
	sess.Mu.Lock()
	sess.User.Settings.APIKey = apis
	sess.Mu.Unlock()

	// Update Redis login session if available
	if h.sessionManager.HasRedis() {
		settingsStr := sess.User.Settings.ToString()
		if err := h.sessionManager.Redis().SetLoginSession(
			h.sessionManager.Ctx, sessionID,
			&cache.LoginSessionData{
				UserID:   sess.User.ID,
				UserSN:   sess.User.SN,
				No:       sess.User.No,
				Nickname: sess.User.Nickname,
				Settings: settingsStr,
			},
		); err != nil {
			h.logger.Warnf("failed to update Redis login session after API key save: %v", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
