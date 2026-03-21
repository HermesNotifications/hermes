package admin

import "net/http"

type notificationStatusResponse struct {
	Notification any `json:"notification"`
	Events       any `json:"events"`
}

func (s *Server) handleGetNotification(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	n, err := s.store.GetNotificationByID(r.Context(), id)
	if err != nil {
		s.clientError(w, http.StatusNotFound, "notification not found")
		return
	}

	events, err := s.store.GetNotificationEvents(r.Context(), id)
	if err != nil {
		s.serverError(w, err)
		return
	}

	s.jsonResponse(w, http.StatusOK, notificationStatusResponse{
		Notification: n,
		Events:       events,
	})
}
