package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"BrainForever/infra/httpx"
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/remote/agent/toolimp"
)

// ============================================================
// main -remote-server trait extraction service
//
// Listens on :8088 and provides:
//   - GET  /api/health       -health check
//   - POST /api/traits       -trait extraction (JSON in/out)
//   - /demo/                 -static files (demo page)
// ============================================================

func main() {
	// ============================================================
	// Initialize i18n with remote language resources
	// ============================================================
	i18n.Init("lang/remote")

	// ============================================================
	// Create a signal-aware context
	// ============================================================
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ============================================================
	// Setup routes
	// ============================================================
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/api/health", handleHealth)

	// JSON trait extraction endpoint (POST)
	mux.HandleFunc("/api/traits", handleTraitsJSON)

	// Serve demo static files
	mux.Handle("/demo/", http.StripPrefix("/demo/", http.FileServer(http.Dir("cmd/remote-server/demo"))))

	// Catch-all for unimplemented endpoints
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/demo/", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "not found",
			"path":    r.URL.Path,
			"message": "remote-server -see /demo/ for the demo page",
		})
	})

	// Wrap with CORS middleware
	handler := httpx.UseCORSMiddleware(mux)

	// ============================================================
	// Start HTTP Server on :8088
	// ============================================================
	addr := ":8088"
	if envAddr := os.Getenv("REMOTE_ADDR"); envAddr != "" {
		addr = envAddr
	}

	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	// ============================================================
	// Graceful shutdown
	// ============================================================
	go func() {
		<-ctx.Done()
		fmt.Println("\nShutting down remote-server...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			server.Close()
		}
	}()

	fmt.Printf("remote-server listening on: http://%s\n", addr)
	fmt.Println("demo page: http://localhost:8088/demo/")
	fmt.Println("press Ctrl+C to stop the server")

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "server failed to start: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("remote-server shut down gracefully")
}

// ============================================================
// Request / Response types
// ============================================================

// traitsRequest is the JSON body for POST /api/traits.
type traitsRequest struct {
	SN       string      `json:"sn"`
	Title    string      `json:"title"`
	Messages []traitsMsg `json:"messages"`
}

// traitsMsg represents a single message in the request.
type traitsMsg struct {
	Role     string `json:"role"` // "user" or "assistant"
	Content  string `json:"content"`
	CreateAt string `json:"create_at"` // RFC3339 or "2006-01-02 15:04:05" format
}

// traitsResponse is the JSON response for POST /api/traits.
type traitsResponse struct {
	Features []toolimp.TripTraitsFeature `json:"features,omitempty"`
	Usage    *llm.Usage                  `json:"usage,omitempty"`
	Error    string                      `json:"error,omitempty"`
}

// ============================================================
// Handlers
// ============================================================

// handleHealth responds with a simple health check JSON.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"server":  "remote-server",
		"version": "1.0.0",
	})
}

// handleTraitsJSON is the JSON trait extraction endpoint.
//
// Request: POST /api/traits
// Body (JSON):
//
//	{
//	  "sn": "chat-sn-xxx",
//	  "title": "chat title",
//	  "messages": [
//	    {"role": "user", "content": "...", "create_at": "2026-06-20 10:00:00"},
//	    {"role": "assistant", "content": "...", "create_at": "2026-06-20 10:01:00"}
//	  ]
//	}
//
// Response (JSON):
//
//	{
//	  "features": [...],
//	  "usage": {"prompt_tokens":..., "completion_tokens":..., "total_tokens":...}
//	}
func handleTraitsJSON(w http.ResponseWriter, r *http.Request) {
	// Only accept POST
	if r.Method != http.MethodPost {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// ----------------------------------------------------------
	// 1. Parse request body
	// ----------------------------------------------------------
	var req traitsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, fmt.Sprintf("invalid JSON body: %v", err), http.StatusBadRequest)
		return
	}

	if req.SN == "" {
		writeJSONError(w, "missing 'sn' field", http.StatusBadRequest)
		return
	}
	if len(req.Messages) == 0 {
		writeJSONError(w, "missing 'messages' field (empty array)", http.StatusBadRequest)
		return
	}

	// ----------------------------------------------------------
	// 2. Build LLM messages from request data
	// ----------------------------------------------------------
	// Determine language for i18n system prompt
	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = "zh-CN"
	}

	systemContent := getTraitSystemPrompt(lang, req.Title)

	llmMsgs := make([]llm.Message, 0, 1+len(req.Messages))
	llmMsgs = append(llmMsgs, llm.Message{
		Role:    llm.RoleSystem,
		Content: systemContent,
	})

	for _, m := range req.Messages {
		role := m.Role
		if role != llm.RoleUser && role != llm.RoleAssistant {
			continue
		}

		// Add timestamp prefix [YYYY-MM-DD HH:MM:SS] to help the analyzing LLM
		content := m.Content
		if m.CreateAt != "" {
			// Try to parse and reformat the timestamp
			if t, err := parseCreateTime(m.CreateAt); err == nil {
				content = "[" + t.Format("2006-01-02 15:04:05") + "] " + content
			}
		}

		// For assistant messages: truncate to 1000 runes, skip reasoning
		if role == llm.RoleAssistant {
			runes := []rune(content)
			if len(runes) > 1024 {
				content = string(runes[:500]) + "\n...\n" + string(runes[len(runes)-500:])
			}
		}

		llmMsgs = append(llmMsgs, llm.Message{
			Role:    role,
			Content: content,
		})
	}

	if len(llmMsgs) <= 1 {
		writeJSONError(w, "no valid messages after filtering", http.StatusBadRequest)
		return
	}

	// ----------------------------------------------------------
	// 3. Create DeepSeek client
	// ----------------------------------------------------------
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	baseURL := os.Getenv("DEEPSEEK_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com/"
	}
	model := os.Getenv("DEEPSEEK_MODEL")
	if model == "" {
		model = "deepseek-chat"
	}

	client := llm.NewDeepSeekClient(baseURL, apiKey, "DEEPSEEK_API_KEY", model)

	// ----------------------------------------------------------
	// 4. Create tool and request
	// ----------------------------------------------------------
	tripTool := toolimp.NewTripTraitsTool(lang)

	reqBody := llm.ChatCompletionRequest{
		Model:    model,
		Messages: llmMsgs,
		Tools:    []llm.ToolDefinition{tripTool.GetDefinition()},
	}

	// Force tool choice -only allow the LLM to call the trip_traits tool.
	reqBody.ForceToolChoice(toolimp.TripTraitsToolName)

	// Disable thinking to reduce latency and cost
	reqBody.Thinking = &llm.ThinkingConfig{Type: "disabled"}

	// ----------------------------------------------------------
	// 5. Call DeepSeek API (non-streaming)
	// ----------------------------------------------------------
	resp, err := client.ChatWithOptions(r.Context(), reqBody)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("LLM call failed: %v", err), http.StatusInternalServerError)
		return
	}

	// ----------------------------------------------------------
	// 6. Parse tool calls from the non-streaming response
	// ----------------------------------------------------------
	result := traitsResponse{}

	// Store usage info
	if resp.Usage != nil && resp.Usage.TotalTokens > 0 {
		result.Usage = resp.Usage
	}

	if len(resp.Choices) > 0 && resp.Choices[0].FinishReason == "tool_calls" {
		msg := resp.Choices[0].Message
		for _, tc := range msg.ToolCalls {
			if err := tripTool.SetArgument(tc.Function.Arguments); err != nil {
				continue
			}
			if _, err := tripTool.Execute(); err != nil {
				continue
			}
		}

		traitsResult := tripTool.GetTraitsResult()
		result.Features = traitsResult.Features
	} else if len(resp.Choices) > 0 && resp.Choices[0].Message.Content != "" {
		// Fallback: try to parse JSON from the text response
		result.Error = "LLM returned text instead of tool call"
	}

	// ----------------------------------------------------------
	// 7. Write response
	// ----------------------------------------------------------
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// ============================================================
// Helpers
// ============================================================

// writeJSONError writes a JSON error response with the given status code.
func writeJSONError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(traitsResponse{Error: msg})
}

// parseCreateTime tries to parse a timestamp string in various formats.
func parseCreateTime(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05 -07:00",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	// If trimming whitespace, try again
	s = strings.TrimSpace(s)
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time: %s", s)
}
