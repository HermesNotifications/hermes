package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

type tenantItem struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	DefaultLocale string    `json:"default_locale"`
	UserCount     int       `json:"user_count"`
	CreatedAt     time.Time `json:"created_at"`
}

type tenantListOutput struct {
	Body []tenantItem
}

func (s *Server) registerTenantRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "list-tenants",
		Method:      http.MethodGet,
		Path:        "/v1/tenants",
		Summary:     "List tenants",
		Tags:        []string{"Tenants"},
	}, func(ctx context.Context, input *struct{}) (*tenantListOutput, error) {
		tenants, err := s.store.ListTenants(ctx)
		if err != nil {
			s.logger.Error("failed to list tenants", "error", err)
			return nil, huma.Error500InternalServerError("internal server error")
		}

		counts, err := s.store.CountUsersByTenant(ctx)
		if err != nil {
			s.logger.Error("failed to count users by tenant", "error", err)
			return nil, huma.Error500InternalServerError("internal server error")
		}

		items := make([]tenantItem, len(tenants))
		for i, t := range tenants {
			items[i] = tenantItem{
				ID:            t.ID,
				Name:          t.Name,
				DefaultLocale: t.DefaultLocale,
				UserCount:     counts[t.ID],
				CreatedAt:     t.CreatedAt,
			}
		}
		return &tenantListOutput{Body: items}, nil
	})
}
