package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// ============================================================
// DeleteMessageHandler -DELETE /api/chat/messages
// ============================================================

// DeleteMessageRequest is the request body for deleting a message pair
type DeleteMessageRequest struct {
	MsgID int64 `json:"msg_id"` // Unique message ID of the user message to delete (along with its AI reply)
}

// OnDeleteMessage handles DELETE /api/chat/messages -deletes a user+assistant message pair by msg_id
func (h *ChatAgent) OnDeleteMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := h.resolveSessionID(w, r)

	var req DeleteMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to parse request. %v", err), http.StatusBadRequest)
		return
	}

	if req.MsgID == 0 {
		http.Error(w, "msg_id is required", http.StatusBadRequest)
		return
	}

	if err := h.sessionManager.DeleteMessage(sessionID, req.MsgID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}
