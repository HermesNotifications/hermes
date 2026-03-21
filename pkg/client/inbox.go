package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type InboxNotification struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	UserID      string    `json:"user_id"`
	GroupID     string    `json:"group_id"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	ActionURL   *string   `json:"action_url,omitempty"`
	ActionLabel *string   `json:"action_label,omitempty"`
	Channels    []string  `json:"channels"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	ReadAt      *string   `json:"read_at,omitempty"`
	ArchivedAt  *string   `json:"archived_at,omitempty"`
}

type InboxListResponse struct {
	Data        []InboxNotification `json:"data"`
	UnreadCount int                 `json:"unread_count"`
	Cursor      string              `json:"cursor,omitempty"`
}

type InboxClient struct {
	baseURL    string
	jwt        string
	httpClient *http.Client
}

func NewInboxClient(baseURL, jwt string) *InboxClient {
	return &InboxClient{
		baseURL:    baseURL,
		jwt:        jwt,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *InboxClient) List(ctx context.Context, archived bool, cursor string, limit int) (*InboxListResponse, error) {
	path := fmt.Sprintf("/v1/inbox?archived=%t&limit=%d", archived, limit)
	if cursor != "" {
		path += "&cursor=" + cursor
	}
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var resp InboxListResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *InboxClient) MarkRead(ctx context.Context, id string) error {
	req, err := c.newRequest(ctx, http.MethodPut, "/v1/inbox/"+id+"/read", nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

func (c *InboxClient) MarkUnread(ctx context.Context, id string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/v1/inbox/"+id+"/read", nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

func (c *InboxClient) Archive(ctx context.Context, id string) error {
	req, err := c.newRequest(ctx, http.MethodPut, "/v1/inbox/"+id+"/archive", nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

func (c *InboxClient) Unarchive(ctx context.Context, id string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/v1/inbox/"+id+"/archive", nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

func (c *InboxClient) Delete(ctx context.Context, id string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, "/v1/inbox/"+id, nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

func (c *InboxClient) MarkAllRead(ctx context.Context) error {
	req, err := c.newRequest(ctx, http.MethodPut, "/v1/inbox/read-all", nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

func (c *InboxClient) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.jwt)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *InboxClient) do(req *http.Request, v any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return &APIError{StatusCode: resp.StatusCode, Message: errResp.Error}
	}

	if v != nil && resp.StatusCode != http.StatusNoContent {
		return json.NewDecoder(resp.Body).Decode(v)
	}
	return nil
}
