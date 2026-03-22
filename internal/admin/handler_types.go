package admin

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/models"
)

type createTypeInput struct {
	Body struct {
		GroupID      string  `json:"group_id" required:"true" minLength:"1" doc:"ID of the group this type belongs to"`
		Slug         string  `json:"slug" required:"true" minLength:"1" doc:"URL-friendly identifier"`
		Name         string  `json:"name" required:"true" minLength:"1" doc:"Human-readable name"`
		EmailSubject *string `json:"email_subject,omitempty" doc:"Email subject template"`
		EmailBody    *string `json:"email_body,omitempty" doc:"Email body template"`
		SMSBody      *string `json:"sms_body,omitempty" doc:"SMS body template"`
		InboxTitle   *string `json:"inbox_title,omitempty" doc:"Inbox notification title template"`
		InboxBody    *string `json:"inbox_body,omitempty" doc:"Inbox notification body template"`
	}
}

type updateTypeInput struct {
	ID string `path:"id" doc:"Type ID"`
	Body struct {
		Name         string  `json:"name" doc:"Human-readable name"`
		EmailSubject *string `json:"email_subject,omitempty" doc:"Email subject template"`
		EmailBody    *string `json:"email_body,omitempty" doc:"Email body template"`
		SMSBody      *string `json:"sms_body,omitempty" doc:"SMS body template"`
		InboxTitle   *string `json:"inbox_title,omitempty" doc:"Inbox notification title template"`
		InboxBody    *string `json:"inbox_body,omitempty" doc:"Inbox notification body template"`
	}
}

type deleteTypeInput struct {
	ID string `path:"id" doc:"Type ID"`
}

type typeOutput struct {
	Body models.NotificationType
}

type typeListOutput struct {
	Body []models.NotificationType
}

func (s *Server) registerTypeRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "list-types",
		Method:      http.MethodGet,
		Path:        "/v1/types",
		Summary:     "List notification types",
		Tags:        []string{"Types"},
	}, func(ctx context.Context, input *struct{}) (*typeListOutput, error) {
		types, err := s.store.ListTypes(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &typeListOutput{Body: types}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "create-type",
		Method:        http.MethodPost,
		Path:          "/v1/types",
		Summary:       "Create a notification type",
		Tags:          []string{"Types"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *createTypeInput) (*typeOutput, error) {
		nt, err := s.store.CreateType(ctx, &models.NotificationType{
			GroupID: input.Body.GroupID, Slug: input.Body.Slug, Name: input.Body.Name,
			EmailSubject: input.Body.EmailSubject, EmailBody: input.Body.EmailBody,
			SMSBody: input.Body.SMSBody, InboxTitle: input.Body.InboxTitle, InboxBody: input.Body.InboxBody,
		})
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &typeOutput{Body: *nt}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "update-type",
		Method:      http.MethodPut,
		Path:        "/v1/types/{id}",
		Summary:     "Update a notification type",
		Tags:        []string{"Types"},
	}, func(ctx context.Context, input *updateTypeInput) (*typeOutput, error) {
		existing, err := s.store.GetTypeByID(ctx, input.ID)
		if err != nil {
			return nil, huma.Error404NotFound("type not found")
		}

		updated, err := s.store.UpdateType(ctx, &models.NotificationType{
			ID: input.ID, Name: input.Body.Name,
			EmailSubject: input.Body.EmailSubject, EmailBody: input.Body.EmailBody,
			SMSBody: input.Body.SMSBody, InboxTitle: input.Body.InboxTitle, InboxBody: input.Body.InboxBody,
		})
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		if s.cache != nil {
			s.cache.InvalidateTypeConfig(ctx, existing.Slug)
		}

		return &typeOutput{Body: *updated}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "delete-type",
		Method:        http.MethodDelete,
		Path:          "/v1/types/{id}",
		Summary:       "Delete a notification type",
		Tags:          []string{"Types"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *deleteTypeInput) (*struct{}, error) {
		existing, err := s.store.GetTypeByID(ctx, input.ID)
		if err != nil {
			return nil, huma.Error404NotFound("type not found")
		}

		if err := s.store.DeleteType(ctx, input.ID); err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		if s.cache != nil {
			s.cache.InvalidateTypeConfig(ctx, existing.Slug)
		}
		return nil, nil
	})
}
