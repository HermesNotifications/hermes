package inbox

import (
	"net/http"
	"strconv"

	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/httputil"
)

type listInboxResponse struct {
	Data        any    `json:"data"`
	UnreadCount int    `json:"unread_count"`
	Cursor      string `json:"cursor,omitempty"`
}

// @Summary List inbox notifications
// @Tags inbox
// @Produce json
// @Param archived query bool false "Filter archived notifications"
// @Param cursor query string false "Pagination cursor"
// @Param limit query int false "Page size (default 20)"
// @Success 200 {object} listInboxResponse
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /v1/inbox [get]
// @Security BearerAuth
func (s *Server) handleListInbox(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		httputil.ClientError(w, http.StatusUnauthorized, "missing user")
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
		httputil.ServerError(w, s.logger, err)
		return
	}

	// Ensure we return [] not null in JSON
	var data any = notifications
	if notifications == nil {
		data = []struct{}{}
	}

	httputil.JSON(w, http.StatusOK, listInboxResponse{
		Data:        data,
		UnreadCount: unreadCount,
		Cursor:      nextCursor,
	})
}
