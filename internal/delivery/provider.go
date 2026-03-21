package delivery

import "context"

type DeliveryRequest struct {
	NotificationID string
	TenantID       string
	UserID         string
	Channel        string
	Title          string
	Body           string
	ActionURL      string
	ActionLabel    string
	EmailTo        string
	PhoneTo        string
}

type DeliveryResult struct {
	Success      bool
	ProviderName string
	ProviderID   string
	Error        string
	Metadata     map[string]string
}

type Provider interface {
	Send(ctx context.Context, req DeliveryRequest) (DeliveryResult, error)
	Name() string
}
