package admin

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/models"
)

type getNotificationInput struct {
	ID string `path:"id" doc:"Notification ID"`
}

type notificationStatusOutput struct {
	Body struct {
		Notification models.Notification      `json:"notification" doc:"The notification record"`
		Events       []models.NotificationEvent `json:"events" doc:"Timeline of notification events"`
	}
}

func (s *Server) registerNotificationRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "get-notification",
		Method:      http.MethodGet,
		Path:        "/v1/notifications/{id}",
		Summary:     "Get notification status and events",
		Tags:        []string{"Notifications"},
	}, func(ctx context.Context, input *getNotificationInput) (*notificationStatusOutput, error) {
		n, err := s.store.GetNotificationByID(ctx, input.ID)
		if err != nil {
			return nil, huma.Error404NotFound("notification not found")
		}

		events, err := s.store.GetNotificationEvents(ctx, input.ID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		resp := &notificationStatusOutput{}
		resp.Body.Notification = *n
		resp.Body.Events = events
		return resp, nil
	})
}
