// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/provider"
)

// GetTemplateContent returns the normalized per-channel content map for a template:
// channel slug -> field key -> template string.
func (s *Store) GetTemplateContent(ctx context.Context, templateID string) (map[string]map[string]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT channel_slug, content FROM template_channel_content WHERE template_id = $1`, templateID)
	if err != nil {
		return nil, fmt.Errorf("get template content: %w", err)
	}
	defer rows.Close()

	out := map[string]map[string]string{}
	for rows.Next() {
		var channel string
		var raw []byte
		if err := rows.Scan(&channel, &raw); err != nil {
			return nil, fmt.Errorf("scan template content: %w", err)
		}
		fields := map[string]string{}
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, fmt.Errorf("unmarshal template content for %s: %w", channel, err)
		}
		out[channel] = fields
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// SetTemplateContent replaces a template's per-channel content rows with the given map.
func (s *Store) SetTemplateContent(ctx context.Context, templateID string, content map[string]map[string]string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("set template content begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM template_channel_content WHERE template_id = $1`, templateID); err != nil {
		return fmt.Errorf("clear template content: %w", err)
	}
	for channel, fields := range content {
		if len(fields) == 0 {
			continue
		}
		raw, err := json.Marshal(fields)
		if err != nil {
			return fmt.Errorf("marshal template content for %s: %w", channel, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO template_channel_content (template_id, channel_slug, content) VALUES ($1, $2, $3)`,
			templateID, channel, raw); err != nil {
			return fmt.Errorf("insert template content for %s: %w", channel, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("set template content commit: %w", err)
	}
	return nil
}

// contentFromFixedColumns derives the normalized content map from a template's fixed
// Email*/SMS*/Inbox* fields. Transitional bridge during phase 2; removed in phase 2e.
func contentFromFixedColumns(t *models.NotificationTemplate) map[string]map[string]string {
	out := map[string]map[string]string{}
	put := func(channel, key string, v *string) {
		if v == nil {
			return
		}
		if out[channel] == nil {
			out[channel] = map[string]string{}
		}
		out[channel][key] = *v
	}
	put(provider.ChannelEmail, "subject", t.EmailSubject)
	put(provider.ChannelEmail, "body", t.EmailBody)
	put(provider.ChannelSMS, "body", t.SMSBody)
	put(provider.ChannelInbox, "title", t.InboxTitle)
	put(provider.ChannelInbox, "body", t.InboxBody)
	return out
}
