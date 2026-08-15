// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	texttemplate "text/template"
	"time"

	"github.com/hermesnotifications/hermes/internal/cache"
	"github.com/hermesnotifications/hermes/internal/models"
	"github.com/hermesnotifications/hermes/internal/provider"
	"github.com/hermesnotifications/hermes/internal/store"
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
		switch {
		case err != nil:
			// Counted apart from a miss because the two need different responses. A
			// miss is the cache working on a cold key; an error is Redis failing, and
			// because the store fallback below is correct it is otherwise invisible —
			// dispatch keeps answering, just with a template SELECT per notification.
			recordCacheResult(ctx, "template", "error")
		case data == nil:
			recordCacheResult(ctx, "template", "miss")
		default:
			var nt models.NotificationTemplate
			if err := json.Unmarshal(data, &nt); err == nil {
				recordCacheResult(ctx, "template", "hit")
				return &nt, nil
			}
			// Cached bytes that will not unmarshal are a hit that bought nothing —
			// a stale encoding after a model change, most likely. Counted as its own
			// outcome so it cannot inflate the hit rate it actively undermines.
			recordCacheResult(ctx, "template", "corrupt")
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
