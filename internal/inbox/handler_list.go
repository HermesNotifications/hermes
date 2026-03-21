package inbox

import (
	"net/http"
	"strconv"

	"github.com/hermes-notifications/hermes/internal/auth"
)

type listInboxResponse struct {
	Data        any    `json:"data"`
	UnreadCount int    `json:"unread_count"`
	Cursor      string `json:"cursor,omitempty"`
}

func (s *Server) handleListInbox(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		s.clientError(w, http.StatusUnauthorized, "missing user")
		return
	}

	archived := r.URL.Query().Get("archived") == "true"
	cursor := r.URL.Query().Get("cursor")
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	notifications, unreadCount, nextCursor, err := s.store.ListInbox(r.Context(), userID, archived, cursor, limit)
	if err != nil {
		s.serverError(w, err)
		return
	}

	// Ensure we return [] not null in JSON
	var data any = notifications
	if notifications == nil {
		data = []struct{}{}
	}

	s.jsonResponse(w, http.StatusOK, listInboxResponse{
		Data:        data,
		UnreadCount: unreadCount,
		Cursor:      nextCursor,
	})
}
