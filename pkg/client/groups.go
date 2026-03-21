package client

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type Group struct {
	ID              string    `json:"id"`
	Slug            string    `json:"slug"`
	Name            string    `json:"name"`
	DefaultChannels []string  `json:"default_channels"`
	CreatedAt       time.Time `json:"created_at"`
}

type CreateGroupRequest struct {
	Slug            string   `json:"slug"`
	Name            string   `json:"name"`
	DefaultChannels []string `json:"default_channels,omitempty"`
}

type UpdateGroupRequest struct {
	Name            *string  `json:"name,omitempty"`
	DefaultChannels []string `json:"default_channels"`
}

type GroupsService struct {
	client *Client
}

func (s *GroupsService) List(ctx context.Context) ([]Group, error) {
	req, err := s.client.newRequest(ctx, http.MethodGet, "/v1/groups", nil)
	if err != nil {
		return nil, err
	}
	var groups []Group
	if err := s.client.do(req, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

func (s *GroupsService) Create(ctx context.Context, body CreateGroupRequest) (*Group, error) {
	req, err := s.client.newRequest(ctx, http.MethodPost, "/v1/groups", body)
	if err != nil {
		return nil, err
	}
	var group Group
	if err := s.client.do(req, &group); err != nil {
		return nil, err
	}
	return &group, nil
}

func (s *GroupsService) Update(ctx context.Context, id string, body UpdateGroupRequest) (*Group, error) {
	req, err := s.client.newRequest(ctx, http.MethodPut, fmt.Sprintf("/v1/groups/%s", id), body)
	if err != nil {
		return nil, err
	}
	var group Group
	if err := s.client.do(req, &group); err != nil {
		return nil, err
	}
	return &group, nil
}
