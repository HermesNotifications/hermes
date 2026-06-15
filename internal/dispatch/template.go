// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	texttemplate "text/template"
	"time"

	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/provider"
	"github.com/hermes-notifications/hermes/internal/store"
)

type TemplateResolver struct {
	store store.TemplateRepository
	cache *cache.Client
}

func NewTemplateResolver(store store.TemplateRepository, cache *cache.Client) *TemplateResolver {
	return &TemplateResolver{store: store, cache: cache}
}

func (tr *TemplateResolver) Resolve(ctx context.Context, slug string) (*models.NotificationTemplate, error) {
	if tr.cache != nil {
		data, err := tr.cache.GetTemplateConfig(ctx, slug)
		if err == nil && data != nil {
			var nt models.NotificationTemplate
			if err := json.Unmarshal(data, &nt); err == nil {
				return &nt, nil
			}
		}
	}
	nt, err := tr.store.GetTemplateBySlug(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("resolve template %s: %w", slug, err)
	}
	if tr.cache != nil {
		if data, err := json.Marshal(nt); err == nil {
			_ = tr.cache.SetTemplateConfig(ctx, slug, data, 5*time.Minute)
		}
	}
	return nt, nil
}

// RenderedContent holds rendered per-channel content: channel slug -> field key
// -> rendered value. Produced by RenderTemplates from a template's normalized
// Content map and the channel registry's render kinds.
type RenderedContent map[string]map[string]string

func RenderTemplates(nt *models.NotificationTemplate, data map[string]any) (RenderedContent, error) {
	out := RenderedContent{}
	for channel, fields := range nt.Content {
		desc, ok := provider.Builtins.Channel(channel)
		rendered := make(map[string]string, len(fields))
		for key, tmpl := range fields {
			renderHTMLField := false
			if ok {
				if cf, found := desc.ContentFieldByKey(key); found {
					renderHTMLField = cf.Render == provider.RenderHTML
				}
			}
			var (
				val string
				err error
			)
			if renderHTMLField {
				val, err = renderHTML(tmpl, data)
			} else {
				val, err = renderText(tmpl, data)
			}
			if err != nil {
				return nil, fmt.Errorf("render %s.%s: %w", channel, key, err)
			}
			rendered[key] = val
		}
		out[channel] = rendered
	}
	return out, nil
}

func RenderDirectContent(title, body string, data map[string]any) (string, string, error) {
	if len(data) == 0 {
		return title, body, nil
	}
	renderedTitle, err := renderText(title, data)
	if err != nil {
		return "", "", fmt.Errorf("render title: %w", err)
	}
	renderedBody, err := renderText(body, data)
	if err != nil {
		return "", "", fmt.Errorf("render body: %w", err)
	}
	return renderedTitle, renderedBody, nil
}

func renderText(tmplStr string, data map[string]any) (string, error) {
	t, err := texttemplate.New("").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func renderHTML(tmplStr string, data map[string]any) (string, error) {
	t, err := htmltemplate.New("").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
