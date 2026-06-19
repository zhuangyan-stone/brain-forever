package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"BrainForever/infra/httpx"
	"BrainForever/infra/httpx/sse"
	"BrainForever/infra/i18n"
	"BrainForever/infra/llm"
	"BrainForever/internal/local/store"
	"BrainForever/internal/remote/agent"
	"BrainForever/internal/remote/agent/toolimp"
)

// ============================================================
// main — remote-server trait extraction prototype
//
// Listens on :8088 and provides:
//   - GET /api/health          — health check
//   - GET /api/chats?db=<path> — list available chat sessions
//   - GET /api/traits?db=<path>&sn=<sn> — SSE endpoint for trait extraction
//   - /demo/                   — static files (demo page)
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

	// List available chats in a database
	mux.HandleFunc("/api/chats", handleListChats)

	// SSE endpoint for trait extraction
	mux.HandleFunc("/api/traits", handleTraitsSSE)

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
			"message": "remote-server — see /demo/ for the demo page",
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
			log.Printf("server shutdown timed out or errored: %v", err)
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

// handleListChats lists available chat sessions from a database.
//
// Query params:
//   - db: path to the SQLite database file (required)
func handleListChats(w http.ResponseWriter, r *http.Request) {
	dbPath := r.URL.Query().Get("db")
	if dbPath == "" {
		http.Error(w, `{"error":"missing 'db' query param"}`, http.StatusBadRequest)
		return
	}

	chatStore, err := store.CreateLocalChatScheme(dbPath)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"open db failed: %v"}`, err), http.StatusInternalServerError)
		return
	}
	defer chatStore.Close()

	chats, err := chatStore.ListChats(50)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"list chats failed: %v"}`, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"chats": chats,
	})
}

// handleTraitsSSE is the SSE endpoint for user trait extraction.
//
// Query params:
//   - db: path to the SQLite database file (required)
//   - sn: chat session SN to extract traits from (required)
//
// Flow:
//  1. Opens the database, reads the chat session and its messages.
//  2. Builds an LLM request with the trait extraction system prompt and tools.
//  3. Calls DeepSeek's ChatWithOptions (non-streaming) with forced tool_choice.
//  4. Sends progress status events to the frontend via SSE.
//  5. Parses the tool call response, executes the trip_traits tool,
//     and sends the result via SSE.
func handleTraitsSSE(w http.ResponseWriter, r *http.Request) {
	dbPath := r.URL.Query().Get("db")
	sn := r.URL.Query().Get("sn")

	if dbPath == "" || sn == "" {
		http.Error(w, `{"error":"missing 'db' or 'sn' query param"}`, http.StatusBadRequest)
		return
	}

	// ----------------------------------------------------------
	// 1. Open database and read chat session + messages
	// ----------------------------------------------------------
	chatStore, err := store.CreateLocalChatScheme(dbPath)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"open db failed: %v"}`, err), http.StatusInternalServerError)
		return
	}
	defer chatStore.Close()

	chat, err := chatStore.FindChatBySN(sn)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"chat not found (sn=%s): %v"}`, sn, err), http.StatusNotFound)
		return
	}

	msgs, err := chatStore.ListMessages(chat.ID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"list messages failed: %v"}`, err), http.StatusInternalServerError)
		return
	}

	if len(msgs) == 0 {
		http.Error(w, `{"error":"no messages in this chat"}`, http.StatusBadRequest)
		return
	}

	log.Printf("[traits] processing chat id=%d sn=%s with %d messages", chat.ID, sn, len(msgs))

	// Determine language for i18n system prompt
	lang := i18n.GetAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang == "" {
		lang = "zh-CN" // Default to Chinese
	}

	// ----------------------------------------------------------
	// 2. Build LLM messages
	// ----------------------------------------------------------
	llmMsgs := make([]llm.Message, 0, 1+len(msgs))
	llmMsgs = append(llmMsgs, llm.Message{
		Role:    llm.RoleSystem,
		Content: getTraitSystemPrompt(lang, chat.Title),
	})

	for _, m := range msgs {
		role := mapRole(m.Role)
		if role == "" {
			continue
		}

		// Add timestamp prefix [YYYY-MM-DD HH:MM:SS] to help the analyzing LLM
		// understand when the conversation took place (especially for user messages)
		content := m.Content
		if !m.CreateAt.IsZero() {
			content = "[" + m.CreateAt.In(time.Local).Format("2006-01-02 15:04:05") + "] " + content
		}

		// For assistant messages: truncate to 1000 runes, skip reasoning
		// (AI replies are less important than user messages for trait extraction)
		if role == llm.RoleAssistant {
			runes := []rune(content)
			if len(runes) > 1024 {
				content = string(runes[:500]) + "\n...\n" + string(runes[len(runes)-500:])
			}
		}

		msg := llm.Message{
			Role:    role,
			Content: content,
		}
		llmMsgs = append(llmMsgs, msg)
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
	// 4. Create tool and pipeline
	// ----------------------------------------------------------
	tripTool := toolimp.NewTripTraitsTool(lang)
	toolsImp := []llm.ToolIMP{tripTool}

	// Set up SSE
	sw := sse.NewSSEWriter(w)
	pipeline := agent.NewTraitPipeline(sw, toolsImp)

	// Send initial event to confirm connection
	pipeline.WriteEvent("connected", map[string]interface{}{
		"chat_title":    chat.Title,
		"message_count": len(msgs),
	})

	// Send status: preparing the LLM request
	pipeline.WriteEvent("status", "preparing_request")

	// ----------------------------------------------------------
	// 5. Build the non-streaming request with tools
	// ----------------------------------------------------------
	req := llm.ChatCompletionRequest{
		Model:    model,
		Messages: llmMsgs,
		Tools:    pipeline.GetToolDefines(),
	}

	// Force tool choice — only allow the LLM to call the trip_traits tool.
	req.ForceToolChoice(toolimp.TripTraitsToolName)

	// Debug: log tool_choice and tool names being sent to the API.
	log.Printf("[traits] sending request: model=%s, tool_choice=%s, tool_count=%d, message_count=%d",
		req.Model, string(req.ToolChoice), len(req.Tools), len(req.Messages))
	for i, td := range req.Tools {
		if td.Function.Strict != nil {
			log.Printf("[traits]   tool[%d]: name=%s, strict=%v", i, td.Function.Name, *td.Function.Strict)
		} else {
			log.Printf("[traits]   tool[%d]: name=%s, strict=nil", i, td.Function.Name)
		}
	}

	// Disable thinking to reduce latency and cost (trait extraction only needs function calling).
	req.Thinking = &llm.ThinkingConfig{Type: "disabled"}

	// ----------------------------------------------------------
	// 6. Call DeepSeek API (non-streaming) and parse response
	// ----------------------------------------------------------
	pipeline.WriteEvent("status", "calling_llm")

	resp, err := client.ChatWithOptions(r.Context(), req)
	if err != nil {
		pipeline.OnError(fmt.Errorf("LLM call failed: %w", err))
		return
	}

	// Extract usage info if available
	if resp.Usage != nil && resp.Usage.TotalTokens > 0 {
		pipeline.WriteEvent("usage", resp.Usage)
	}

	pipeline.WriteEvent("status", "processing_result")

	// ----------------------------------------------------------
	// 7. Parse tool calls from the non-streaming response
	// ----------------------------------------------------------
	if len(resp.Choices) > 0 && resp.Choices[0].FinishReason == "tool_calls" {
		msg := resp.Choices[0].Message
		for _, tc := range msg.ToolCalls {
			log.Printf("[trip_traits] toolCall: name=%q, arguments=%s", tc.Function.Name, tc.Function.Arguments)
			if err := pipeline.Pending(tc.ID, tc.Function.Name, tc.Function.Arguments); err != nil {
				pipeline.OnError(fmt.Errorf("set tool arguments failed: %w", err))
				continue
			}
			if _, err := pipeline.Call(tc.ID, tc.Function.Name); err != nil {
				pipeline.OnError(fmt.Errorf("execute tool failed: %w", err))
				continue
			}
		}

		result := tripTool.GetTraitsResult()
		pipeline.WriteEvent("tool_result", result)
	} else if len(resp.Choices) > 0 && resp.Choices[0].Message.Content != "" {
		// Fallback: if the LLM returned text instead of a tool call
		pipeline.WriteEvent("fallback_text", resp.Choices[0].Message.Content)
	}

	// Signal completion
	pipeline.WriteEvent("done", map[string]interface{}{
		"chat_id":  chat.ID,
		"chat_sn":  sn,
		"features": tripTool.GetTraitsResult().Features,
	})
}

// mapRole converts the store's role int (0=user, 1=assistant) to an LLM role string.
func mapRole(role int8) string {
	switch role {
	case 0:
		return llm.RoleUser
	case 1:
		return llm.RoleAssistant
	default:
		return ""
	}
}
