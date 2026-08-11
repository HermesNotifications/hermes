# Provider Plugin Model — Phase 2: Normalized Content & Address Model

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move per-channel template content and recipient addresses out of fixed columns
(`email_subject`/`…`/`inbox_body`, `users.email`/`users.phone`) into normalized,
channel-keyed tables (`template_channel_content`, `user_contact_points`), de-hardcoding the
content/address shape so adding a channel needs no schema change. End state: the fixed
columns are gone and the Phase-1 residual accessors (`RenderedContent.Field`,
`Recipient.AddressFor`) are deleted.

**Architecture:** The Phase-1 `internal/provider` registry gains a **content schema** per
channel (`[]ContentField` — local field key, render kind text/html, projection onto the
delivery message's Title/Body). Template content becomes `template_channel_content`
(`(template_id, channel_slug) → content jsonb`); recipient addresses become
`user_contact_points` (`(user_id, address_key) → address`). The store assembles these into
map-shaped model fields (`NotificationTemplate.Content`, `User.Contacts`). Rendering and
dispatch read those maps and project via the registry schema. The public admin/user API and
the 4 generated SDKs move to channel-keyed shapes; the Send API + `SendMessage` + AsyncAPI
gain a channel-keyed contact override map.

**Tech Stack:** Go, Postgres (golang-migrate up/down pairs), huma (OpenAPI generation),
openapi-generator (TS/Python/Java/.NET SDKs). No DynamoDB work — templates and users are
Postgres-only today (config / Phase-2-DynamoDB-candidate per [ADR 0001](../../adr/0001-dynamodb-model-via-extenddb.md)),
so this phase stays entirely on the Postgres store. Builds on [ADR 0002](../../adr/0002-provider-plugin-model-bus-native-isolation.md)
(Phase 1).

**Design source:** `docs/superpowers/specs/2026-06-13-provider-plugin-model-design.md` §3
("Normalized content & address model") and §2.

---

## Key design decisions (apply across all sub-phases)

1. **`template_channel_content` is keyed by `channel_slug`** (content genuinely varies per
   channel). `content` is a JSONB object of the channel's **local** content-field keys →
   template string, e.g. email `{"subject": "...", "body": "..."}`, sms `{"body": "..."}`,
   inbox `{"title": "...", "body": "..."}`. The local keys (`subject`/`body`/`title`) come
   from the channel's content schema in the registry — *not* the global `email_subject`
   form.

2. **`user_contact_points` is keyed by `address_key`** (`"email"`, `"phone"`), **not**
   `channel_slug`. This refines the spec's table (which named `channel_slug`): an email
   address serves *every* email-type channel, so the address partitions by address type, not
   channel — and it matches the spec's own resolution text ("resolve contact points by the
   channel's required address key") and the natural backfill from `users.email`/`users.phone`.
   The registry's `ChannelDescriptor.AddressKey` is the lookup key.

3. **Model carries map fields:** `NotificationTemplate.Content map[string]map[string]string`
   (channel_slug → field_key → template string) and `User.Contacts map[string]string`
   (address_key → address). These are added **alongside** the old fixed fields first; the
   old fields are removed only in the final sub-phase (2e), so every intermediate step
   compiles and stays green.

4. **Safe cutover sequencing.** The migration that *creates + backfills* the new tables
   (2a) **keeps** the old columns; the store **dual-writes** (old columns + new tables) so
   both representations stay consistent during the transition. Readers flip to the new
   tables incrementally (2b–2d). A final migration (2e) **drops** the old columns and the
   old model fields. The end state is the cutover you asked for (no old columns); the
   intermediate dual-write is what makes each task boundary independently green and
   reviewable rather than one giant red change.

5. **Render kind per field** lives in the registry (`ContentField.Render` = `text`|`html`):
   email body is HTML (XSS-escaped), everything else is text — preserving exactly today's
   `renderText`/`renderHTML` split.

---

## Sub-phase roadmap

Each sub-phase is a separately reviewable, shippable increment. **This document fully
specifies Phase 2a.** Phases 2b–2e are scoped here and will be planned in detail (their own
plan docs) once the preceding phase lands — the real breakage surface is best discovered
incrementally.

| Sub-phase | Scope | End state |
|---|---|---|
| **2a (this plan)** | Registry content schema; model map fields (additive); migration `000015` create+backfill `template_channel_content` + `user_contact_points` (**keep** old columns); store dual-writes + loads the new tables into `Content`/`Contacts`. | New tables live & populated; old columns still present; behavior unchanged. |
| **2b** | Rendering (`RenderTemplates`) + dispatch (`contentForChannel`, recipient build) read from `Content`/`Contacts` via the registry schema. Remove `RenderedContent.Field` + `Recipient.AddressFor` residuals; reshape `RenderedContent`. | Hot path reads normalized data. |
| **2c** | Admin template CRUD + user contact API → channel-keyed shapes; `make openapi`; `make sdk-generate` (TS/Python/Java/.NET). | Public API + SDKs normalized. |
| **2d** | Send API + `SendMessage` + AsyncAPI: channel-keyed contact override map (generalize `msg.Email`/`msg.Phone`). | Send path normalized end-to-end. |
| **2e** | Migration `000016` DROP old columns; remove old model fields (`EmailSubject`…, `User.Email`/`Phone`) and Phase-1 `provider.Field*` constants; final sweep. | Cutover complete; fixed columns gone. |

---

# PHASE 2a — Registry content schema, model fields, schema & store foundation

## File Structure (2a)

- Modify: `internal/provider/channel.go` — add `ContentField`, `RenderText`/`RenderHTML`, `Content []ContentField`, `ContentField(key)` lookup.
- Modify: `internal/provider/builtins.go` — populate `Content` for email/sms/inbox.
- Test: `internal/provider/content_test.go` (new) — content-schema assertions.
- Modify: `internal/models/models.go` — add `NotificationTemplate.Content`, `User.Contacts`.
- Create: `migrations/000015_normalized_content_address.up.sql` / `.down.sql`.
- Create: `internal/store/postgres/template_content.go` — content row read/write helpers.
- Create: `internal/store/postgres/user_contacts.go` — contact-point row read/write helpers.
- Modify: `internal/store/postgres/templates.go` — dual-write content on Create/Update; load `Content` on Get/List.
- Modify: `internal/store/postgres/users.go` — dual-write contact on UpdateUserContacts; load `Contacts` on Get/List/Ensure.
- Modify: `internal/store/interfaces.go` — extend `TemplateRepository`/`UserRepository` with the new explicit content/contact methods.
- Test: `internal/store/postgres/template_content_test.go`, `internal/store/postgres/user_contacts_test.go` (new, `//go:build integration`).

> All store tests in 2a are **integration** tests (`//go:build integration`, require
> `make infra-up`). Run with `make test-integration` or
> `go test ./internal/store/postgres/... -tags=integration -p 1 -count=1`.

---

## Task 1: Registry content schema

**Files:**
- Modify: `internal/provider/channel.go`
- Modify: `internal/provider/builtins.go`
- Test: `internal/provider/content_test.go`

- [ ] **Step 1: Write the failing test** — create `internal/provider/content_test.go`:

```go
// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package provider

import "testing"

func TestBuiltins_ContentSchema(t *testing.T) {
	email, _ := Builtins.Channel(ChannelEmail)
	subject, ok := email.ContentFieldByKey("subject")
	if !ok || subject.Render != RenderText || subject.MapsTo != "title" {
		t.Fatalf("email subject field: got %+v ok=%v", subject, ok)
	}
	body, ok := email.ContentFieldByKey("body")
	if !ok || body.Render != RenderHTML || body.MapsTo != "body" {
		t.Fatalf("email body field: got %+v ok=%v", body, ok)
	}
	if _, ok := email.ContentFieldByKey("nope"); ok {
		t.Fatal("unknown content field should report ok=false")
	}

	sms, _ := Builtins.Channel(ChannelSMS)
	if len(sms.Content) != 1 || sms.Content[0].Key != "body" || sms.Content[0].Render != RenderText {
		t.Fatalf("sms content: got %+v", sms.Content)
	}

	inbox, _ := Builtins.Channel(ChannelInbox)
	title, ok := inbox.ContentFieldByKey("title")
	if !ok || title.MapsTo != "title" || title.Render != RenderText {
		t.Fatalf("inbox title field: got %+v ok=%v", title, ok)
	}
}
```

- [ ] **Step 2: Run to verify it fails** — `go test ./internal/provider/ -run TestBuiltins_ContentSchema -v` → FAIL (`ContentFieldByKey`/`RenderText`/`Content` undefined).

- [ ] **Step 3: Add the schema types** to `internal/provider/channel.go`. Add these constants and type, and add the `Content` field + lookup method to `ChannelDescriptor` (keep all existing fields):

```go
// Render kinds for a content field.
const (
	RenderText = "text"
	RenderHTML = "html"
)

// ContentField declares one field in a channel's content schema. The Key is the
// channel-local field name stored in template_channel_content's JSONB (e.g.
// "subject", "body", "title"). Render selects text vs HTML rendering. MapsTo
// projects the rendered value onto the delivery MessageContent ("title" |
// "body" | "" for neither).
type ContentField struct {
	Key    string
	Render string // RenderText | RenderHTML
	MapsTo string // "title" | "body" | ""
}
```

Add to the `ChannelDescriptor` struct (after `BodyField`):

```go
	// Content is the channel's content schema: the ordered set of content
	// fields a template provides for this channel. Stored per channel in
	// template_channel_content. Supersedes TitleField/BodyField/HasContent
	// (removed in phase 2e).
	Content []ContentField
```

Add this method anywhere in `channel.go`:

```go
// ContentFieldByKey returns the content-field schema for a local field key.
func (d ChannelDescriptor) ContentFieldByKey(key string) (ContentField, bool) {
	for _, f := range d.Content {
		if f.Key == key {
			return f, true
		}
	}
	return ContentField{}, false
}
```

- [ ] **Step 4: Populate `Content`** in `internal/provider/builtins.go`. Add a `Content:` field to each of the three `RegisterChannel` calls (keep the existing `TitleField`/`BodyField`/`HasContent`/`AddressKey`/`AddressLabel`):

For email:
```go
		Content: []ContentField{
			{Key: "subject", Render: RenderText, MapsTo: "title"},
			{Key: "body", Render: RenderHTML, MapsTo: "body"},
		},
```
For sms:
```go
		Content: []ContentField{
			{Key: "body", Render: RenderText, MapsTo: "body"},
		},
```
For inbox:
```go
		Content: []ContentField{
			{Key: "title", Render: RenderText, MapsTo: "title"},
			{Key: "body", Render: RenderText, MapsTo: "body"},
		},
```

- [ ] **Step 5: Run** — `go test ./internal/provider/ -v` → all PASS (existing + new). `gofmt -l internal/provider/` → empty.

- [ ] **Step 6: Commit**

```bash
git add internal/provider/
git commit -m "feat(provider): add per-channel content schema to the registry"
```

---

## Task 2: Model map fields (additive)

**Files:**
- Modify: `internal/models/models.go`
- Test: `internal/models/models_test.go` (create if absent, else append) — JSON round-trip.

- [ ] **Step 1: Write the failing test.** Check whether `internal/models/models_test.go` exists; create it (package `models_test`) or append. Add:

```go
// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package models_test

import (
	"encoding/json"
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
)

func TestNotificationTemplate_ContentJSON(t *testing.T) {
	tpl := models.NotificationTemplate{
		Content: map[string]map[string]string{
			"email": {"subject": "Hi {{.name}}", "body": "<p>x</p>"},
			"sms":   {"body": "hi"},
		},
	}
	b, err := json.Marshal(tpl)
	if err != nil {
		t.Fatal(err)
	}
	var back models.NotificationTemplate
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Content["email"]["subject"] != "Hi {{.name}}" || back.Content["sms"]["body"] != "hi" {
		t.Fatalf("round-trip mismatch: %+v", back.Content)
	}
}

func TestUser_ContactsJSON(t *testing.T) {
	u := models.User{Contacts: map[string]string{"email": "a@b.c", "phone": "+1555"}}
	b, _ := json.Marshal(u)
	var back models.User
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Contacts["email"] != "a@b.c" || back.Contacts["phone"] != "+1555" {
		t.Fatalf("round-trip mismatch: %+v", back.Contacts)
	}
}
```

- [ ] **Step 2: Run to verify it fails** — `go test ./internal/models/ -run 'ContentJSON|ContactsJSON' -v` → FAIL (`Content`/`Contacts` undefined).

- [ ] **Step 3: Add the fields.** In `internal/models/models.go`, add to `NotificationTemplate` (after `InboxBody`, before `CreatedAt`):

```go
	// Content is the normalized per-channel content: channel slug -> field key
	// -> template string. Added in phase 2a alongside the fixed Email*/SMS*/
	// Inbox* fields, which are removed in phase 2e.
	Content map[string]map[string]string `json:"content,omitempty"`
```

Add to `User` (after `Phone`, before `Locale`):

```go
	// Contacts is the normalized contact-point map: address key ("email",
	// "phone") -> address. Added in phase 2a alongside Email/Phone, which are
	// removed in phase 2e.
	Contacts map[string]string `json:"contacts,omitempty"`
```

- [ ] **Step 4: Run** — `go test ./internal/models/ -run 'ContentJSON|ContactsJSON' -v` → PASS. `go build ./...` → exit 0.

- [ ] **Step 5: Commit**

```bash
git add internal/models/models.go internal/models/models_test.go
git commit -m "feat(models): add normalized Content/Contacts map fields (additive)"
```

---

## Task 3: Migration 000015 — create + backfill (keep old columns)

**Files:**
- Create: `migrations/000015_normalized_content_address.up.sql`
- Create: `migrations/000015_normalized_content_address.down.sql`
- Test: `internal/store/postgres/migration_000015_test.go` (new, `//go:build integration`)

- [ ] **Step 1: Write the up migration** — `migrations/000015_normalized_content_address.up.sql`:

```sql
-- Normalized per-channel template content and per-address-key user contact points.
-- Phase 2a: create + backfill. Old fixed columns are KEPT here and dropped in 000016 (phase 2e).

CREATE TABLE template_channel_content (
    template_id  TEXT  NOT NULL REFERENCES notification_templates(id) ON DELETE CASCADE,
    channel_slug TEXT  NOT NULL,
    content      JSONB NOT NULL DEFAULT '{}',
    PRIMARY KEY (template_id, channel_slug)
);

CREATE TABLE user_contact_points (
    user_id     TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    address_key TEXT    NOT NULL,
    address     TEXT    NOT NULL,
    verified    BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (user_id, address_key)
);

-- Backfill template content from the fixed columns. jsonb_strip_nulls drops absent fields
-- so a template with only a subject doesn't store a null body.
INSERT INTO template_channel_content (template_id, channel_slug, content)
SELECT id, 'email', jsonb_strip_nulls(jsonb_build_object('subject', email_subject, 'body', email_body))
FROM notification_templates
WHERE email_subject IS NOT NULL OR email_body IS NOT NULL;

INSERT INTO template_channel_content (template_id, channel_slug, content)
SELECT id, 'sms', jsonb_build_object('body', sms_body)
FROM notification_templates
WHERE sms_body IS NOT NULL;

INSERT INTO template_channel_content (template_id, channel_slug, content)
SELECT id, 'inbox', jsonb_strip_nulls(jsonb_build_object('title', inbox_title, 'body', inbox_body))
FROM notification_templates
WHERE inbox_title IS NOT NULL OR inbox_body IS NOT NULL;

-- Backfill contact points from users.email / users.phone.
INSERT INTO user_contact_points (user_id, address_key, address, verified)
SELECT id, 'email', email, FALSE FROM users WHERE email IS NOT NULL AND email <> '';

INSERT INTO user_contact_points (user_id, address_key, address, verified)
SELECT id, 'phone', phone, FALSE FROM users WHERE phone IS NOT NULL AND phone <> '';
```

- [ ] **Step 2: Write the down migration** — `migrations/000015_normalized_content_address.down.sql`:

```sql
DROP TABLE IF EXISTS user_contact_points;
DROP TABLE IF EXISTS template_channel_content;
```

- [ ] **Step 3: Write the integration test** — `internal/store/postgres/migration_000015_test.go`. First read an existing `//go:build integration` test in `internal/store/postgres/` (e.g. `templates_test.go`) to copy the exact test harness (how it gets a `*Store`/pool, the build tag line, and any `setupTestStore`/`newTestStore` helper). Then write a test that: creates a template via `CreateTemplate` with the fixed fields set, and asserts a `template_channel_content` row exists with the expected JSONB; creates a user + sets contacts, asserts a `user_contact_points` row exists. Use the SAME harness helper the neighboring tests use. Example shape (adapt the harness call to match the existing pattern):

```go
//go:build integration

// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package postgres

import (
	"context"
	"testing"
)

func TestMigration000015_TablesExist(t *testing.T) {
	s := newTestStore(t) // <-- match the harness used by templates_test.go
	ctx := context.Background()

	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM template_channel_content`).Scan(&n); err != nil {
		t.Fatalf("template_channel_content not queryable: %v", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM user_contact_points`).Scan(&n); err != nil {
		t.Fatalf("user_contact_points not queryable: %v", err)
	}
}
```

(The backfill itself is exercised end-to-end by Tasks 4 & 5; this test just confirms the
migration applies cleanly in the integration DB.)

- [ ] **Step 4: Apply migrations and run** — with infra up (`make infra-up`), run `make migrate` then `go test ./internal/store/postgres/ -tags=integration -run TestMigration000015 -p 1 -count=1 -v` → PASS. If `newTestStore`/harness name differs, fix the test to match before running.

- [ ] **Step 5: Commit**

```bash
git add migrations/000015_normalized_content_address.up.sql migrations/000015_normalized_content_address.down.sql internal/store/postgres/migration_000015_test.go
git commit -m "feat(db): add template_channel_content + user_contact_points with backfill"
```

---

## Task 4: Store — template content dual-write + load

**Files:**
- Create: `internal/store/postgres/template_content.go`
- Modify: `internal/store/postgres/templates.go`
- Modify: `internal/store/interfaces.go`
- Test: `internal/store/postgres/template_content_test.go` (new, `//go:build integration`)

The store must keep the fixed columns and the new table consistent. On **read**, load the
`Content` map from `template_channel_content`. On **write** (Create/Update), continue writing
the fixed columns (unchanged) **and** derive+upsert the per-channel content rows from the
template's fixed fields via the registry schema, so both representations agree.

- [ ] **Step 1: Add the interface methods.** In `internal/store/interfaces.go`, add to
`TemplateRepository`:

```go
	GetTemplateContent(ctx context.Context, templateID string) (map[string]map[string]string, error)
	SetTemplateContent(ctx context.Context, templateID string, content map[string]map[string]string) error
```

- [ ] **Step 2: Implement the content row helpers** — create `internal/store/postgres/template_content.go`:

```go
// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
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
// Email*/SMS*/Inbox* fields, using the registry's content schema for field keys. Used during
// the phase-2 transition to keep the new table consistent with the fixed columns on write.
// Removed in phase 2e when the fixed columns are dropped.
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
```

Add the imports `"github.com/hermes-notifications/hermes/internal/models"` and
`"github.com/hermes-notifications/hermes/internal/provider"` to this file.

- [ ] **Step 3: Wire dual-write + load into `templates.go`.** In `internal/store/postgres/templates.go`:

In `CreateTemplate`, after the successful `INSERT ... RETURNING created_at` (after the `if err != nil` block, before `return input, nil`):
```go
	if err := s.SetTemplateContent(ctx, input.ID, contentFromFixedColumns(input)); err != nil {
		return nil, err
	}
	input.Content = contentFromFixedColumns(input)
```

In `UpdateTemplate`, after the successful scan (before `return input, nil`):
```go
	if err := s.SetTemplateContent(ctx, input.ID, contentFromFixedColumns(input)); err != nil {
		return nil, err
	}
	input.Content = contentFromFixedColumns(input)
```

In `GetTemplateByID`, after the successful scan (before `return t, nil`):
```go
	content, err := s.GetTemplateContent(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	t.Content = content
```

In `GetTemplateBySlug`, the same (after scan, before `return t, nil`):
```go
	content, err := s.GetTemplateContent(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	t.Content = content
```

In `ListTemplates`, after the loop appends all templates (before `return templates, rows.Err()`), load content per template. **Note:** load *after* `rows.Close()`/iteration to avoid using the connection mid-iteration — restructure to collect rows first, then enrich:
```go
	for i := range templates {
		content, err := s.GetTemplateContent(ctx, templates[i].ID)
		if err != nil {
			return nil, err
		}
		templates[i].Content = content
	}
```
Place this block after the `for rows.Next()` loop and after `rows.Err()` is known to be nil — i.e. replace the final `return templates, rows.Err()` with:
```go
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range templates {
		content, err := s.GetTemplateContent(ctx, templates[i].ID)
		if err != nil {
			return nil, err
		}
		templates[i].Content = content
	}
	return templates, nil
```

- [ ] **Step 4: Write the integration test** — `internal/store/postgres/template_content_test.go`. Match the harness used by `templates_test.go`. Assert:
  - `CreateTemplate` with `EmailSubject`+`EmailBody`+`SMSBody` set → returned `Content` has `email.subject`, `email.body`, `sms.body`; and `GetTemplateBySlug` reloads the same `Content` from the table.
  - `UpdateTemplate` changing `EmailSubject` → `GetTemplateContent` reflects the new value and stale channels are gone.

```go
//go:build integration

// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package postgres

import (
	"context"
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
)

func strptr(s string) *string { return &s } // drop if a shared helper already exists in this package

func TestTemplateContent_DualWriteAndLoad(t *testing.T) {
	s := newTestStore(t) // match templates_test.go harness
	ctx := context.Background()

	created, err := s.CreateTemplate(ctx, &models.NotificationTemplate{
		Slug: "tc-dualwrite", Name: "T",
		DefaultChannels: []string{"email", "sms"},
		EmailSubject:    strptr("Hi {{.name}}"),
		EmailBody:       strptr("<p>x</p>"),
		SMSBody:         strptr("hi"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Content["email"]["subject"] != "Hi {{.name}}" ||
		created.Content["email"]["body"] != "<p>x</p>" ||
		created.Content["sms"]["body"] != "hi" {
		t.Fatalf("create content: %+v", created.Content)
	}

	got, err := s.GetTemplateBySlug(ctx, "tc-dualwrite")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content["email"]["subject"] != "Hi {{.name}}" || got.Content["sms"]["body"] != "hi" {
		t.Fatalf("reload content: %+v", got.Content)
	}

	got.EmailSubject = strptr("Updated")
	updated, err := s.UpdateTemplate(ctx, got)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Content["email"]["subject"] != "Updated" {
		t.Fatalf("update content: %+v", updated.Content)
	}
	reload, _ := s.GetTemplateContent(ctx, got.ID)
	if reload["email"]["subject"] != "Updated" {
		t.Fatalf("reloaded content after update: %+v", reload)
	}
}
```

> If `strptr`/`strPtr` already exists in the `postgres` test package, drop the local helper.

- [ ] **Step 5: Run** — `go build ./...` → 0. With infra up: `go test ./internal/store/postgres/ -tags=integration -run 'TemplateContent|Migration000015' -p 1 -count=1 -v` → PASS. Also run the existing template tests to confirm no regression: `go test ./internal/store/postgres/ -tags=integration -run Template -p 1 -count=1`.

- [ ] **Step 6: Commit**

```bash
git add internal/store/postgres/template_content.go internal/store/postgres/templates.go internal/store/interfaces.go internal/store/postgres/template_content_test.go
git commit -m "feat(store): dual-write and load normalized template content"
```

---

## Task 5: Store — user contact dual-write + load

**Files:**
- Create: `internal/store/postgres/user_contacts.go`
- Modify: `internal/store/postgres/users.go`
- Modify: `internal/store/interfaces.go`
- Test: `internal/store/postgres/user_contacts_test.go` (new, `//go:build integration`)

- [ ] **Step 1: Add the interface methods.** In `internal/store/interfaces.go`, add to `UserRepository`:

```go
	GetUserContacts(ctx context.Context, userID string) (map[string]string, error)
	SetUserContact(ctx context.Context, userID, addressKey, address string) error
```

- [ ] **Step 2: Implement the contact helpers** — create `internal/store/postgres/user_contacts.go`:

```go
// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package postgres

import (
	"context"
	"fmt"
)

// GetUserContacts returns the user's contact points: address key -> address.
func (s *Store) GetUserContacts(ctx context.Context, userID string) (map[string]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT address_key, address FROM user_contact_points WHERE user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("get user contacts: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var key, addr string
		if err := rows.Scan(&key, &addr); err != nil {
			return nil, fmt.Errorf("scan user contact: %w", err)
		}
		out[key] = addr
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// SetUserContact upserts a single contact point (verified defaults to false on insert,
// preserved on update).
func (s *Store) SetUserContact(ctx context.Context, userID, addressKey, address string) error {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO user_contact_points (user_id, address_key, address)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, address_key) DO UPDATE SET address = EXCLUDED.address`,
		userID, addressKey, address); err != nil {
		return fmt.Errorf("set user contact: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: Wire dual-write + load into `users.go`.**

In `GetUserByID`, after the successful scan (before `return u, nil`):
```go
	contacts, err := s.GetUserContacts(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	u.Contacts = contacts
```

In `EnsureUser`, after the successful scan (before `return u, nil`):
```go
	contacts, err := s.GetUserContacts(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	u.Contacts = contacts
```

In `UpdateUserContacts`, after the successful scan and before `return u, nil`, dual-write the
provided contacts into the new table (only the non-nil ones, matching the COALESCE semantics):
```go
	if email != nil {
		if err := s.SetUserContact(ctx, userID, "email", *email); err != nil {
			return nil, err
		}
	}
	if phone != nil {
		if err := s.SetUserContact(ctx, userID, "phone", *phone); err != nil {
			return nil, err
		}
	}
	contacts, err := s.GetUserContacts(ctx, userID)
	if err != nil {
		return nil, err
	}
	u.Contacts = contacts
```

In `ListUsers`, after the loop + `rows.Err()` check, enrich each user (same restructure as
templates): replace the final `return users, rows.Err()` with:
```go
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range users {
		contacts, err := s.GetUserContacts(ctx, users[i].ID)
		if err != nil {
			return nil, err
		}
		users[i].Contacts = contacts
	}
	return users, nil
```

- [ ] **Step 4: Write the integration test** — `internal/store/postgres/user_contacts_test.go`. Match the harness used by `users_test.go`. Assert:
  - After `UpdateUserContacts(userID, &email, &phone)`, the returned user's `Contacts["email"]`/`Contacts["phone"]` match, and `GetUserContacts` returns rows.
  - `GetUserByID` reloads `Contacts`.

```go
//go:build integration

// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package postgres

import (
	"context"
	"testing"
)

func TestUserContacts_DualWriteAndLoad(t *testing.T) {
	s := newTestStore(t) // match users_test.go harness
	ctx := context.Background()

	u := ensureTestUser(t, s) // match the helper users_test.go uses to create a user+tenant
	email, phone := "a@b.c", "+15551234"

	updated, err := s.UpdateUserContacts(ctx, u.ID, &email, &phone)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Contacts["email"] != email || updated.Contacts["phone"] != phone {
		t.Fatalf("update contacts: %+v", updated.Contacts)
	}

	got, err := s.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Contacts["email"] != email || got.Contacts["phone"] != phone {
		t.Fatalf("reload contacts: %+v", got.Contacts)
	}
}
```

> Read `users_test.go` to copy its exact user/tenant setup; replace `ensureTestUser` and
> `newTestStore` with the real helpers.

- [ ] **Step 5: Run** — `go build ./...` → 0. With infra up: `go test ./internal/store/postgres/ -tags=integration -run 'UserContacts|User' -p 1 -count=1 -v` → PASS (new + existing user tests).

- [ ] **Step 6: Commit**

```bash
git add internal/store/postgres/user_contacts.go internal/store/postgres/users.go internal/store/interfaces.go internal/store/postgres/user_contacts_test.go
git commit -m "feat(store): dual-write and load normalized user contact points"
```

---

## Task 6: Phase 2a verification

- [ ] **Step 1: Build** — `go build ./...` → exit 0.
- [ ] **Step 2: Unit tests** — `make test` → PASS (provider, models, dispatch unaffected).
- [ ] **Step 3: Integration tests** (infra up) — `make migrate` then `go test ./internal/store/postgres/... -tags=integration -p 1 -count=1` → PASS. Also `make test-e2e` to confirm the pipeline still works end-to-end with dual-write in place.
- [ ] **Step 4: Lint** — `make lint` → 0 issues.
- [ ] **Step 5: Spec-sync check** — `make openapi-check` should still pass (no API shape change in 2a; `models.NotificationTemplate`/`models.User` gained `content`/`contacts` fields with `omitempty`, which DO surface in the admin/user OpenAPI output since those models are embedded in responses). If `make openapi-check` fails because the generated spec now includes the new fields, run `make openapi` and commit the regenerated specs as part of this phase:
```bash
make openapi
git add api/
git commit -m "chore(api): regenerate specs for additive content/contacts model fields"
```
> This is expected: `templateOutput`/`userOutput` embed the models, so the additive fields
> appear in responses. That is acceptable in 2a (additive, non-breaking). The request-shape
> reshape is 2c.

---

## Self-Review (Phase 2a)

**Scope coverage:** registry content schema (Task 1) ✓; model map fields additive (Task 2) ✓;
new tables + backfill, old columns kept (Task 3) ✓; store dual-write+load for content
(Task 4) and contacts (Task 5) ✓. Rendering/dispatch/API/SDK and the column DROP are
explicitly **out of 2a** (phases 2b–2e) — listed in the roadmap.

**No behavior change in 2a:** the hot path (rendering, dispatch) still reads the fixed
columns/model fields; the new tables are populated in parallel. Existing tests must still
pass (Task 6 runs them).

**Placeholder scan:** the store-test harness calls (`newTestStore`, `ensureTestUser`) are
marked "match the existing helper" rather than invented — the implementer must read
`templates_test.go`/`users_test.go` first. This is deliberate (the harness name is the one
thing I can't verify without the file), not a placeholder for logic. All production code is
complete.

**Type consistency:** `Content map[string]map[string]string` and `Contacts map[string]string`
are used identically in models, store helpers, and tests. Registry `ContentField{Key,
Render, MapsTo}` and `RenderText`/`RenderHTML` are consistent across Task 1 and the store's
`contentFromFixedColumns`. `GetTemplateContent`/`SetTemplateContent`/`GetUserContacts`/
`SetUserContact` signatures match between interface and impl.

**Risk note:** `contentFromFixedColumns` is the transitional bridge (fixed columns → content
table). It is deleted in 2e along with the fixed columns. Until 2c, the admin API still sends
fixed fields, so deriving content from them on write is correct and keeps the table the
source of truth for readers in 2b.

---

## Execution Handoff

**Phase 2a plan complete.** Phases 2b–2e are scoped in the roadmap and will be planned in
detail after 2a lands. Two execution options for 2a:

1. **Subagent-Driven (recommended)** — fresh subagent per task, spec + code-quality review between tasks.
2. **Inline Execution** — batch execution with checkpoints.

Note: Tasks 3–5 require local infra (`make infra-up`) for their integration tests.
