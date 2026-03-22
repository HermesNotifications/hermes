package admin

import (
	"net/http"

	"github.com/hermes-notifications/hermes/internal/httputil"
)

type notificationStatusResponse struct {
	Notification any `json:"notification"`
	Events       any `json:"events"`
}

// @Summary Get notification status and events
// @Tags notifications
// @Produce json
// @Param id path string true "Notification ID"
// @Success 200 {object} notificationStatusResponse
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /v1/notifications/{id} [get]
// @Security ApiKeyAuth
func (s *Server) handleGetNotification(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	n, err := s.store.GetNotificationByID(r.Context(), id)
	if err != nil {
		httputil.ClientError(w, http.StatusNotFound, "notification not found")
		return
	}

	events, err := s.store.GetNotificationEvents(r.Context(), id)
	if err != nil {
		httputil.ServerError(w, s.logger, err)
		return
	}

	httputil.JSON(w, http.StatusOK, notificationStatusResponse{
		Notification: n,
		Events:       events,
	})
}
