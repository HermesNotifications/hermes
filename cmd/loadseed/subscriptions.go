package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// insertSubscriptionTree creates categories, subscriptions, and templates for a single tenant.
// tenantIdx namespaces the IDs so multiple tenants' trees don't collide.
// subscription_categories and notification_templates have UNIQUE indexes on slug,
// so slugs include the runID + tenantIdx to stay globally unique across runs.
//
func insertSubscriptionTree(ctx context.Context, pool *pgxpool.Pool, runID string, tenantIdx, numCats, subsPerCat, tmplsPerSub int) ([]Category, error) {
	cats := make([]Category, numCats)

	for ci := 0; ci < numCats; ci++ {
		catID := fmt.Sprintf("lt-sct-%s-%d-%d", runID, tenantIdx, ci)
		catSlug := fmt.Sprintf("lt_%s_t%d_cat%d", runID, tenantIdx, ci)
		if _, err := pool.Exec(ctx,
			`INSERT INTO subscription_categories (id, slug, name, default_channels, default_state, sort_order)
			 VALUES ($1, $2, $3, '{inbox,email}', 'on', $4)`,
			catID, catSlug, fmt.Sprintf("Load Test Cat %d/%d", tenantIdx, ci), ci,
		); err != nil {
			return nil, fmt.Errorf("insert category: %w", err)
		}
		cat := Category{ID: catID, Subscriptions: make([]Subscription, subsPerCat)}

		for si := 0; si < subsPerCat; si++ {
			subID := fmt.Sprintf("lt-sub-%s-%d-%d-%d", runID, tenantIdx, ci, si)
			subSlug := fmt.Sprintf("sub%d", si)
			if _, err := pool.Exec(ctx,
				`INSERT INTO subscriptions (id, category_id, slug, name, sort_order)
				 VALUES ($1, $2, $3, $4, $5)`,
				subID, catID, subSlug, fmt.Sprintf("Sub %d/%d/%d", tenantIdx, ci, si), si,
			); err != nil {
				return nil, fmt.Errorf("insert subscription: %w", err)
			}
			sub := Subscription{ID: subID, Templates: make([]Template, tmplsPerSub)}

			for ti := 0; ti < tmplsPerSub; ti++ {
				tmplID := fmt.Sprintf("lt-tpl-%s-%d-%d-%d-%d", runID, tenantIdx, ci, si, ti)
				tmplSlug := fmt.Sprintf("lt_%s_t%d_c%d_s%d_tpl%d", runID, tenantIdx, ci, si, ti)
				if _, err := pool.Exec(ctx,
					`INSERT INTO notification_templates
					 (id, subscription_id, slug, name, default_channels, email_subject, email_body, inbox_title, inbox_body)
					 VALUES ($1, $2, $3, $4, '{inbox,email}', $5, $6, $7, $8)`,
					tmplID, subID, tmplSlug, fmt.Sprintf("Template %d", ti),
					"Load test {{.subject}}", "Hello {{.name}}, this is a load test.",
					"Load Test", "{{.name}}: {{.subject}}",
				); err != nil {
					return nil, fmt.Errorf("insert template: %w", err)
				}
				sub.Templates[ti] = Template{ID: tmplID, Slug: tmplSlug, Channels: []string{"inbox", "email"}}
			}
			cat.Subscriptions[si] = sub
		}
		cats[ci] = cat
	}
	return cats, nil
}
