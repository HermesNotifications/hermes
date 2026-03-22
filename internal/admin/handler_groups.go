package admin

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/models"
)

type createGroupInput struct {
	Body struct {
		Slug            string   `json:"slug" required:"true" minLength:"1" doc:"URL-friendly identifier"`
		Name            string   `json:"name" required:"true" minLength:"1" doc:"Human-readable name"`
		DefaultChannels []string `json:"default_channels,omitempty" doc:"Default delivery channels for this group"`
	}
}

type updateGroupInput struct {
	ID string `path:"id" doc:"Group ID"`
	Body struct {
		Name            string   `json:"name" doc:"Human-readable name"`
		DefaultChannels []string `json:"default_channels" doc:"Default delivery channels for this group"`
	}
}

type groupOutput struct {
	Body models.NotificationGroup
}

type groupListOutput struct {
	Body []models.NotificationGroup
}

func (s *Server) registerGroupRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "list-groups",
		Method:      http.MethodGet,
		Path:        "/v1/groups",
		Summary:     "List notification groups",
		Tags:        []string{"Groups"},
	}, func(ctx context.Context, input *struct{}) (*groupListOutput, error) {
		groups, err := s.store.ListGroups(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &groupListOutput{Body: groups}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "create-group",
		Method:        http.MethodPost,
		Path:          "/v1/groups",
		Summary:       "Create a notification group",
		Tags:          []string{"Groups"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *createGroupInput) (*groupOutput, error) {
		g, err := s.store.CreateGroup(ctx, input.Body.Slug, input.Body.Name, input.Body.DefaultChannels)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &groupOutput{Body: *g}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "update-group",
		Method:      http.MethodPut,
		Path:        "/v1/groups/{id}",
		Summary:     "Update a notification group",
		Tags:        []string{"Groups"},
	}, func(ctx context.Context, input *updateGroupInput) (*groupOutput, error) {
		g, err := s.store.UpdateGroup(ctx, input.ID, input.Body.Name, input.Body.DefaultChannels)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &groupOutput{Body: *g}, nil
	})
}
