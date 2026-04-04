package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
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

type createTenantInput struct {
	Body struct {
		Name string `json:"name" required:"true" minLength:"1" doc:"Tenant name"`
	}
}

type createTenantOutput struct {
	Body tenantItem
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

	huma.Register(s.api, huma.Operation{
		OperationID:   "create-tenant",
		Method:        http.MethodPost,
		Path:          "/v1/tenants",
		Summary:       "Create a tenant",
		Tags:          []string{"Tenants"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *createTenantInput) (*createTenantOutput, error) {
		id := uuid.New().String()
		tenant, err := s.store.CreateTenant(ctx, id, input.Body.Name)
		if err != nil {
			s.logger.Error("failed to create tenant", "error", err)
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &createTenantOutput{Body: tenantItem{
			ID:            tenant.ID,
			Name:          tenant.Name,
			DefaultLocale: tenant.DefaultLocale,
			UserCount:     0,
			CreatedAt:     tenant.CreatedAt,
		}}, nil
	})
}
