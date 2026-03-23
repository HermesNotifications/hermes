package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"time"
	texttemplate "text/template"

	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/store"
)

type TemplateResolver struct {
	store *store.Store
	cache *cache.Client
}

func NewTemplateResolver(store *store.Store, cache *cache.Client) *TemplateResolver {
	return &TemplateResolver{store: store, cache: cache}
}

func (tr *TemplateResolver) Resolve(ctx context.Context, slug string) (*models.NotificationType, error) {
	if tr.cache != nil {
		data, err := tr.cache.GetTypeConfig(ctx, slug)
		if err == nil && data != nil {
			var nt models.NotificationType
			if err := json.Unmarshal(data, &nt); err == nil {
				return &nt, nil
			}
		}
	}
	nt, err := tr.store.GetTypeBySlug(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("resolve type %s: %w", slug, err)
	}
	if tr.cache != nil {
		if data, err := json.Marshal(nt); err == nil {
			tr.cache.SetTypeConfig(ctx, slug, data, 5*time.Minute)
		}
	}
	return nt, nil
}

type RenderedContent struct {
	EmailSubject string
	EmailBody    string
	SMSBody      string
	InboxTitle   string
	InboxBody    string
}

func RenderTemplates(nt *models.NotificationType, data map[string]any) (*RenderedContent, error) {
	rc := &RenderedContent{}
	var err error
	if nt.EmailSubject != nil {
		rc.EmailSubject, err = renderText(*nt.EmailSubject, data)
		if err != nil {
			return nil, fmt.Errorf("render email_subject: %w", err)
		}
	}
	if nt.EmailBody != nil {
		rc.EmailBody, err = renderHTML(*nt.EmailBody, data)
		if err != nil {
			return nil, fmt.Errorf("render email_body: %w", err)
		}
	}
	if nt.SMSBody != nil {
		rc.SMSBody, err = renderText(*nt.SMSBody, data)
		if err != nil {
			return nil, fmt.Errorf("render sms_body: %w", err)
		}
	}
	if nt.InboxTitle != nil {
		rc.InboxTitle, err = renderText(*nt.InboxTitle, data)
		if err != nil {
			return nil, fmt.Errorf("render inbox_title: %w", err)
		}
	}
	if nt.InboxBody != nil {
		rc.InboxBody, err = renderText(*nt.InboxBody, data)
		if err != nil {
			return nil, fmt.Errorf("render inbox_body: %w", err)
		}
	}
	return rc, nil
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
