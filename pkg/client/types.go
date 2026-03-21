package client

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type NotificationType struct {
	ID           string    `json:"id"`
	GroupID      string    `json:"group_id"`
	Slug         string    `json:"slug"`
	Name         string    `json:"name"`
	EmailSubject *string   `json:"email_subject,omitempty"`
	EmailBody    *string   `json:"email_body,omitempty"`
	SMSBody      *string   `json:"sms_body,omitempty"`
	InboxTitle   *string   `json:"inbox_title,omitempty"`
	InboxBody    *string   `json:"inbox_body,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type CreateTypeRequest struct {
	GroupID      string  `json:"group_id"`
	Slug         string  `json:"slug"`
	Name         string  `json:"name"`
	EmailSubject *string `json:"email_subject,omitempty"`
	EmailBody    *string `json:"email_body,omitempty"`
	SMSBody      *string `json:"sms_body,omitempty"`
	InboxTitle   *string `json:"inbox_title,omitempty"`
	InboxBody    *string `json:"inbox_body,omitempty"`
}

type UpdateTypeRequest struct {
	Name         string  `json:"name"`
	EmailSubject *string `json:"email_subject,omitempty"`
	EmailBody    *string `json:"email_body,omitempty"`
	SMSBody      *string `json:"sms_body,omitempty"`
	InboxTitle   *string `json:"inbox_title,omitempty"`
	InboxBody    *string `json:"inbox_body,omitempty"`
}

type TypesService struct{ client *Client }

func (s *TypesService) List(ctx context.Context) ([]NotificationType, error) {
	req, err := s.client.newRequest(ctx, http.MethodGet, "/v1/types", nil)
	if err != nil {
		return nil, err
	}
	var types []NotificationType
	if err := s.client.do(req, &types); err != nil {
		return nil, err
	}
	return types, nil
}

func (s *TypesService) Create(ctx context.Context, body CreateTypeRequest) (*NotificationType, error) {
	req, err := s.client.newRequest(ctx, http.MethodPost, "/v1/types", body)
	if err != nil {
		return nil, err
	}
	var nt NotificationType
	if err := s.client.do(req, &nt); err != nil {
		return nil, err
	}
	return &nt, nil
}

func (s *TypesService) Update(ctx context.Context, id string, body UpdateTypeRequest) (*NotificationType, error) {
	req, err := s.client.newRequest(ctx, http.MethodPut, fmt.Sprintf("/v1/types/%s", id), body)
	if err != nil {
		return nil, err
	}
	var nt NotificationType
	if err := s.client.do(req, &nt); err != nil {
		return nil, err
	}
	return &nt, nil
}

func (s *TypesService) Delete(ctx context.Context, id string) error {
	req, err := s.client.newRequest(ctx, http.MethodDelete, fmt.Sprintf("/v1/types/%s", id), nil)
	if err != nil {
		return err
	}
	return s.client.do(req, nil)
}
