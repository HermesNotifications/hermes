package userservice

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/models"
)

type groupIDInput struct {
	GroupID string `path:"group_id" doc:"Group ID"`
}

type setPreferenceInput struct {
	GroupID string `path:"group_id" doc:"Group ID"`
	Body    struct {
		Channels []string `json:"channels" required:"true" minItems:"1" doc:"Preferred delivery channels"`
	}
}

type preferenceListOutput struct {
	Body struct {
		Data []models.UserPreference `json:"data" doc:"List of notification preferences"`
	}
}

type preferenceOutput struct {
	Body models.UserPreference
}

type statusOutput struct {
	Body struct {
		Status string `json:"status" example:"ok" doc:"Operation result"`
	}
}

func (s *Server) registerPreferenceRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "list-preferences",
		Method:      http.MethodGet,
		Path:        "/v1/users/me/preferences",
		Summary:     "List notification preferences",
		Tags:        []string{"Preferences"},
	}, func(ctx context.Context, input *struct{}) (*preferenceListOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		prefs, err := s.store.GetUserPreferences(ctx, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		if prefs == nil {
			prefs = []models.UserPreference{}
		}

		resp := &preferenceListOutput{}
		resp.Body.Data = prefs
		return resp, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "set-preference",
		Method:      http.MethodPut,
		Path:        "/v1/users/me/preferences/{group_id}",
		Summary:     "Set notification preference for a group",
		Tags:        []string{"Preferences"},
	}, func(ctx context.Context, input *setPreferenceInput) (*preferenceOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		pref, err := s.store.SetUserPreference(ctx, userID, input.GroupID, input.Body.Channels)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		return &preferenceOutput{Body: *pref}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "delete-preference",
		Method:      http.MethodDelete,
		Path:        "/v1/users/me/preferences/{group_id}",
		Summary:     "Delete notification preference for a group",
		Tags:        []string{"Preferences"},
	}, func(ctx context.Context, input *groupIDInput) (*statusOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		if err := s.store.DeleteUserPreference(ctx, userID, input.GroupID); err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		resp := &statusOutput{}
		resp.Body.Status = "ok"
		return resp, nil
	})
}
