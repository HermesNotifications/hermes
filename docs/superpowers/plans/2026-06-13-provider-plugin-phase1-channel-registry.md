# Provider Plugin Model — Phase 1: Channel/Provider Registry & De-hardcoding — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the hardcoded `email`/`sms`/`inbox` channel knowledge out of the three `switch ch` blocks in `internal/dispatch` and into a new `internal/provider` registry, with **zero behavior change**.

**Architecture:** A new `internal/provider` package owns a `Registry` of `ChannelDescriptor`s (and built-in provider `Manifest`s). Each descriptor carries the per-channel facts the three switches encode today: which template fields satisfy the channel (`HasContent`), which contact address the channel needs (`AddressKey`/`AddressLabel`), and which rendered fields project onto the delivery message (`TitleField`/`BodyField`). A process-wide `provider.Builtins` registry is seeded with `email`/`sms`/`inbox` and the existing providers (`smtp`, `ses`, `sms`, `inbox`). Dispatch's three switches become registry lookups. Two tiny *legacy-struct accessors* remain (`RenderedContent.Field`, `Recipient.AddressFor`) — they map a string key onto today's fixed columns/fields and are deleted in Phase 2 when content and contacts become normalized tables. Adding a channel that reuses an existing address + rendered fields then requires **only** a registry registration, no dispatch edits.

**Tech Stack:** Go 1.x, standard library `testing`. No new dependencies. No infrastructure needed for any test in this phase (all unit tests).

**Scope note:** This plan covers **only** spec Phase 1 (registries + de-hardcoding, no behavior change). Normalized content/contact tables are Phase 2; routing, per-provider subjects, and provider *selection* are Phase 3. The provider `Manifest` here is intentionally minimal (`ID` + `Channel`); capabilities/cost/ingress/subjects arrive in later phases.

**Design source:** `docs/superpowers/specs/2026-06-13-provider-plugin-model-design.md` (§2 "Channel vs. Provider model", "Sequencing" phase 1).

---

## File Structure

**New files (package `internal/provider`):**
- `internal/provider/channel.go` — channel slug constants, rendered-field key constants, `ChannelDescriptor` type.
- `internal/provider/manifest.go` — `Manifest` type (minimal: `ID`, `Channel`).
- `internal/provider/registry.go` — `Registry` type: channel + provider registration and lookup.
- `internal/provider/builtins.go` — `var Builtins *Registry`, seeded with the three built-in channels and four built-in providers.
- `internal/provider/registry_test.go` — registry unit tests (`package provider`).
- `internal/provider/builtins_test.go` — built-in-registry content/address/projection tests (`package provider`).

**Modified files:**
- `internal/nats/messages.go` — add `Recipient.AddressFor(key string) string` accessor.
- `internal/dispatch/template.go` — add `RenderedContent.Field(key string) string` accessor.
- `internal/dispatch/channels.go` — `FilterChannelsForTemplate` → registry lookup; add `filterChannelsByContact` helper + `contactSkip` type.
- `internal/dispatch/dispatch.go` — replace the inline contact-info `switch ch` block with a call to `filterChannelsByContact`; replace `contentForChannel`'s `switch channel` with a registry lookup.

**New test files:**
- `internal/dispatch/channels_internal_test.go` — `package dispatch` (internal) tests for `FilterChannelsForTemplate`, `filterChannelsByContact`, `contentForChannel`, and `RenderedContent.Field`.

**Untouched (read-only references):** `internal/models/models.go` (`NotificationTemplate`), `internal/delivery/provider.go` (`delivery.Provider`), `internal/email/email.go`, `internal/delivery/inbox.go`.

---

## Task 1: `internal/provider` package — registry, descriptors, built-ins

**Files:**
- Create: `internal/provider/channel.go`
- Create: `internal/provider/manifest.go`
- Create: `internal/provider/registry.go`
- Create: `internal/provider/builtins.go`
- Test: `internal/provider/registry_test.go`, `internal/provider/builtins_test.go`

- [ ] **Step 1: Write the failing registry test**

Create `internal/provider/registry_test.go`:

```go
// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package provider

import "testing"

func TestRegistry_RegisterAndLookupChannel(t *testing.T) {
	r := NewRegistry()
	r.RegisterChannel(ChannelDescriptor{Slug: "email", AddressKey: "email"})

	desc, ok := r.Channel("email")
	if !ok {
		t.Fatal("expected channel 'email' to be registered")
	}
	if desc.AddressKey != "email" {
		t.Fatalf("AddressKey: got %q, want %q", desc.AddressKey, "email")
	}
	if _, ok := r.Channel("nope"); ok {
		t.Fatal("expected unknown channel to report ok=false")
	}
}

func TestRegistry_DuplicateChannelPanics(t *testing.T) {
	r := NewRegistry()
	r.RegisterChannel(ChannelDescriptor{Slug: "email"})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate channel registration")
		}
	}()
	r.RegisterChannel(ChannelDescriptor{Slug: "email"})
}

func TestRegistry_RegisterProviderAndOrder(t *testing.T) {
	r := NewRegistry()
	r.RegisterChannel(ChannelDescriptor{Slug: "email"})
	r.RegisterProvider(Manifest{ID: "smtp", Channel: "email"})
	r.RegisterProvider(Manifest{ID: "ses", Channel: "email"})

	got := r.ProvidersForChannel("email")
	if len(got) != 2 || got[0] != "smtp" || got[1] != "ses" {
		t.Fatalf("ProvidersForChannel: got %v, want [smtp ses]", got)
	}
	if got := r.ProvidersForChannel("sms"); got != nil {
		t.Fatalf("ProvidersForChannel(sms): got %v, want nil", got)
	}
}

func TestRegistry_ProviderForUnknownChannelPanics(t *testing.T) {
	r := NewRegistry()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when registering provider for unknown channel")
		}
	}()
	r.RegisterProvider(Manifest{ID: "smtp", Channel: "email"})
}
```

- [ ] **Step 2: Run the test to verify it fails to compile**

Run: `go test ./internal/provider/ -run TestRegistry -v`
Expected: FAIL — build error, `undefined: NewRegistry`, `ChannelDescriptor`, `Manifest`, etc.

- [ ] **Step 3: Create `channel.go`**

Create `internal/provider/channel.go`:

```go
// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package provider

import "github.com/hermes-notifications/hermes/internal/models"

// Built-in channel slugs.
const (
	ChannelEmail = "email"
	ChannelSMS   = "sms"
	ChannelInbox = "inbox"
)

// Rendered-content field keys. ChannelDescriptor.TitleField / BodyField
// reference these; dispatch's RenderedContent.Field resolves them against the
// (still fixed, until phase 2) rendered columns.
const (
	FieldEmailSubject = "email_subject"
	FieldEmailBody    = "email_body"
	FieldSMSBody      = "sms_body"
	FieldInboxTitle   = "inbox_title"
	FieldInboxBody    = "inbox_body"
)

// ChannelDescriptor declares everything dispatch needs to route a channel
// without hardcoding its name. It replaces the three `switch ch` blocks in
// internal/dispatch (FilterChannelsForTemplate, the contact-info filter, and
// contentForChannel).
type ChannelDescriptor struct {
	// Slug is the channel identifier (e.g. "email").
	Slug string

	// AddressKey names the contact point this channel delivers to: "email",
	// "phone", or "" when the channel needs no external address (e.g. inbox).
	AddressKey string

	// AddressLabel is the human phrase used in the "user has no X" skip event,
	// preserving today's exact event reason strings (e.g. "email address",
	// "phone number"). Empty when AddressKey is "".
	AddressLabel string

	// TitleField / BodyField name the rendered-content fields projected onto
	// the delivery message's Title / Body. Empty means the channel has no
	// title / no body.
	TitleField string
	BodyField  string

	// HasContent reports whether a template provides content for this channel.
	// Replaces the FilterChannelsForTemplate switch.
	HasContent func(t *models.NotificationTemplate) bool
}
```

- [ ] **Step 4: Create `manifest.go`**

Create `internal/provider/manifest.go`:

```go
// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package provider

// Manifest is the registration record for a delivery provider. Phase 1 carries
// only identity + channel; capabilities, cost tier, ingress style, and derived
// NATS subjects arrive in later phases (see the provider-plugin design doc).
type Manifest struct {
	ID      string // provider id, e.g. "ses"
	Channel string // the channel slug this provider serves
}
```

- [ ] **Step 5: Create `registry.go`**

Create `internal/provider/registry.go`:

```go
// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package provider

import "fmt"

// Registry is the source of truth that de-hardcodes channel/provider knowledge
// out of dispatch. It is populated once (see Builtins) and read concurrently
// thereafter; it is not safe for concurrent registration.
type Registry struct {
	channels  map[string]ChannelDescriptor
	providers map[string]Manifest // keyed by provider ID
	byChannel map[string][]string // channel slug -> ordered provider IDs
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		channels:  map[string]ChannelDescriptor{},
		providers: map[string]Manifest{},
		byChannel: map[string][]string{},
	}
}

// RegisterChannel adds a channel descriptor. Panics on an empty or duplicate slug.
func (r *Registry) RegisterChannel(d ChannelDescriptor) {
	if d.Slug == "" {
		panic("provider: channel descriptor has empty slug")
	}
	if _, dup := r.channels[d.Slug]; dup {
		panic(fmt.Sprintf("provider: channel %q already registered", d.Slug))
	}
	r.channels[d.Slug] = d
}

// Channel returns the descriptor for slug and whether it exists.
func (r *Registry) Channel(slug string) (ChannelDescriptor, bool) {
	d, ok := r.channels[slug]
	return d, ok
}

// RegisterProvider adds a provider manifest under its channel. Panics on an
// empty/duplicate ID or an unregistered channel.
func (r *Registry) RegisterProvider(m Manifest) {
	if m.ID == "" {
		panic("provider: manifest has empty ID")
	}
	if _, ok := r.channels[m.Channel]; !ok {
		panic(fmt.Sprintf("provider: provider %q references unregistered channel %q", m.ID, m.Channel))
	}
	if _, dup := r.providers[m.ID]; dup {
		panic(fmt.Sprintf("provider: provider %q already registered", m.ID))
	}
	r.providers[m.ID] = m
	r.byChannel[m.Channel] = append(r.byChannel[m.Channel], m.ID)
}

// ProvidersForChannel returns the ordered provider IDs registered for a channel
// (nil if none).
func (r *Registry) ProvidersForChannel(channel string) []string {
	return r.byChannel[channel]
}
```

- [ ] **Step 6: Run the registry test to verify it passes**

Run: `go test ./internal/provider/ -run TestRegistry -v`
Expected: PASS (4 tests).

- [ ] **Step 7: Write the failing built-ins test**

Create `internal/provider/builtins_test.go`:

```go
// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package provider

import (
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
)

func sp(s string) *string { return &s }

func TestBuiltins_Channels(t *testing.T) {
	for _, slug := range []string{ChannelEmail, ChannelSMS, ChannelInbox} {
		if _, ok := Builtins.Channel(slug); !ok {
			t.Errorf("built-in channel %q not registered", slug)
		}
	}
}

func TestBuiltins_AddressKeys(t *testing.T) {
	cases := map[string]struct{ key, label string }{
		ChannelEmail: {"email", "email address"},
		ChannelSMS:   {"phone", "phone number"},
		ChannelInbox: {"", ""},
	}
	for slug, want := range cases {
		desc, _ := Builtins.Channel(slug)
		if desc.AddressKey != want.key || desc.AddressLabel != want.label {
			t.Errorf("%s: got (%q,%q), want (%q,%q)", slug, desc.AddressKey, desc.AddressLabel, want.key, want.label)
		}
	}
}

func TestBuiltins_HasContent(t *testing.T) {
	email, _ := Builtins.Channel(ChannelEmail)
	if !email.HasContent(&models.NotificationTemplate{EmailBody: sp("x")}) {
		t.Error("email HasContent: expected true when EmailBody set")
	}
	if email.HasContent(&models.NotificationTemplate{SMSBody: sp("x")}) {
		t.Error("email HasContent: expected false when only SMSBody set")
	}
	sms, _ := Builtins.Channel(ChannelSMS)
	if !sms.HasContent(&models.NotificationTemplate{SMSBody: sp("x")}) {
		t.Error("sms HasContent: expected true when SMSBody set")
	}
	inbox, _ := Builtins.Channel(ChannelInbox)
	if !inbox.HasContent(&models.NotificationTemplate{InboxTitle: sp("x")}) {
		t.Error("inbox HasContent: expected true when InboxTitle set")
	}
}

func TestBuiltins_Providers(t *testing.T) {
	if got := Builtins.ProvidersForChannel(ChannelEmail); len(got) != 2 || got[0] != "smtp" || got[1] != "ses" {
		t.Errorf("email providers: got %v, want [smtp ses]", got)
	}
	if got := Builtins.ProvidersForChannel(ChannelSMS); len(got) != 1 || got[0] != "sms" {
		t.Errorf("sms providers: got %v, want [sms]", got)
	}
	if got := Builtins.ProvidersForChannel(ChannelInbox); len(got) != 1 || got[0] != "inbox" {
		t.Errorf("inbox providers: got %v, want [inbox]", got)
	}
}
```

- [ ] **Step 8: Run the built-ins test to verify it fails to compile**

Run: `go test ./internal/provider/ -run TestBuiltins -v`
Expected: FAIL — `undefined: Builtins`.

- [ ] **Step 9: Create `builtins.go`**

Create `internal/provider/builtins.go`:

```go
// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package provider

import "github.com/hermes-notifications/hermes/internal/models"

// Builtins is the process-wide registry of first-party channels and providers.
// It is constructed once at package-init time and read-only thereafter, so the
// existing dispatch call sites can consult it without a signature change.
// (Phase 3 will layer a DB-backed view over this for third-party providers.)
var Builtins = newBuiltinRegistry()

func newBuiltinRegistry() *Registry {
	r := NewRegistry()

	r.RegisterChannel(ChannelDescriptor{
		Slug:         ChannelEmail,
		AddressKey:   "email",
		AddressLabel: "email address",
		TitleField:   FieldEmailSubject,
		BodyField:    FieldEmailBody,
		HasContent: func(t *models.NotificationTemplate) bool {
			return t.EmailSubject != nil || t.EmailBody != nil
		},
	})
	r.RegisterChannel(ChannelDescriptor{
		Slug:         ChannelSMS,
		AddressKey:   "phone",
		AddressLabel: "phone number",
		BodyField:    FieldSMSBody,
		HasContent: func(t *models.NotificationTemplate) bool {
			return t.SMSBody != nil
		},
	})
	r.RegisterChannel(ChannelDescriptor{
		Slug:       ChannelInbox,
		AddressKey: "", // inbox needs no external contact point
		TitleField: FieldInboxTitle,
		BodyField:  FieldInboxBody,
		HasContent: func(t *models.NotificationTemplate) bool {
			return t.InboxTitle != nil || t.InboxBody != nil
		},
	})

	// Built-in providers, matching what the workers run today. Provider-level
	// selection lands in phase 3; registered now so the registry reflects the
	// deployed providers (email worker: smtp/ses, sms worker: webhook named
	// "sms", inbox worker: "inbox").
	r.RegisterProvider(Manifest{ID: "smtp", Channel: ChannelEmail})
	r.RegisterProvider(Manifest{ID: "ses", Channel: ChannelEmail})
	r.RegisterProvider(Manifest{ID: "sms", Channel: ChannelSMS})
	r.RegisterProvider(Manifest{ID: "inbox", Channel: ChannelInbox})

	return r
}
```

- [ ] **Step 10: Run the full provider package test**

Run: `go test ./internal/provider/ -v`
Expected: PASS (all registry + built-ins tests).

- [ ] **Step 11: Commit**

```bash
git add internal/provider/
git commit -m "feat(provider): add channel/provider registry with built-ins"
```

---

## Task 2: Legacy-struct accessors (`Recipient.AddressFor`, `RenderedContent.Field`)

These two tiny accessors are the boundary between the registry's string keys and today's fixed structs. They are deliberately the *only* place that still maps a key to a fixed field; Phase 2 deletes them when contacts/content become maps.

**Files:**
- Modify: `internal/nats/messages.go` (add method after the `Recipient` struct, ~line 41)
- Modify: `internal/dispatch/template.go` (add method after the `RenderedContent` struct, ~line 57)
- Test: `internal/dispatch/channels_internal_test.go` (created here; covers `Field`. `AddressFor` is covered transitively in Task 4, and directly below.)

- [ ] **Step 1: Write the failing accessor tests**

Create `internal/dispatch/channels_internal_test.go`:

```go
// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dispatch

import (
	"testing"

	hermenats "github.com/hermes-notifications/hermes/internal/nats"
	"github.com/hermes-notifications/hermes/internal/provider"
)

func TestRenderedContent_Field(t *testing.T) {
	rc := &RenderedContent{
		EmailSubject: "subj",
		EmailBody:    "ebody",
		SMSBody:      "sbody",
		InboxTitle:   "ititle",
		InboxBody:    "ibody",
	}
	cases := map[string]string{
		provider.FieldEmailSubject: "subj",
		provider.FieldEmailBody:    "ebody",
		provider.FieldSMSBody:      "sbody",
		provider.FieldInboxTitle:   "ititle",
		provider.FieldInboxBody:    "ibody",
		"":                         "",
		"unknown":                  "",
	}
	for key, want := range cases {
		if got := rc.Field(key); got != want {
			t.Errorf("Field(%q): got %q, want %q", key, got, want)
		}
	}
}

func TestRecipient_AddressFor(t *testing.T) {
	r := hermenats.Recipient{Email: "a@b.c", Phone: "+15551234"}
	if got := r.AddressFor("email"); got != "a@b.c" {
		t.Errorf("AddressFor(email): got %q", got)
	}
	if got := r.AddressFor("phone"); got != "+15551234" {
		t.Errorf("AddressFor(phone): got %q", got)
	}
	if got := r.AddressFor(""); got != "" {
		t.Errorf("AddressFor(\"\"): got %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails to compile**

Run: `go test ./internal/dispatch/ -run 'TestRenderedContent_Field|TestRecipient_AddressFor' -v`
Expected: FAIL — `rc.Field undefined` and `r.AddressFor undefined`.

- [ ] **Step 3: Add `Recipient.AddressFor`**

In `internal/nats/messages.go`, immediately after the `Recipient` struct (ends ~line 41), add:

```go
// AddressFor returns the contact address for a channel's address key ("email"
// or "phone"). Unknown keys return "". This maps the legacy fixed Recipient
// onto the registry's address keys and is removed in phase 2 when contacts
// become normalized contact points.
func (r Recipient) AddressFor(key string) string {
	switch key {
	case "email":
		return r.Email
	case "phone":
		return r.Phone
	}
	return ""
}
```

- [ ] **Step 4: Add `RenderedContent.Field`**

In `internal/dispatch/template.go`, immediately after the `RenderedContent` struct (ends ~line 57), add (this requires importing `internal/provider` — add it to the file's import block):

```go
// Field returns a rendered-content value by its provider field key (see
// internal/provider Field* constants). Unknown/empty keys return "". This maps
// the legacy fixed RenderedContent onto the registry's field keys and is
// removed in phase 2 when content becomes a normalized per-channel table.
func (rc *RenderedContent) Field(key string) string {
	switch key {
	case provider.FieldEmailSubject:
		return rc.EmailSubject
	case provider.FieldEmailBody:
		return rc.EmailBody
	case provider.FieldSMSBody:
		return rc.SMSBody
	case provider.FieldInboxTitle:
		return rc.InboxTitle
	case provider.FieldInboxBody:
		return rc.InboxBody
	}
	return ""
}
```

Add the import to `internal/dispatch/template.go`'s import block:

```go
	"github.com/hermes-notifications/hermes/internal/provider"
```

- [ ] **Step 5: Run to verify the tests pass**

Run: `go test ./internal/dispatch/ -run 'TestRenderedContent_Field|TestRecipient_AddressFor' -v`
Expected: PASS (2 tests).

- [ ] **Step 6: Commit**

```bash
git add internal/nats/messages.go internal/dispatch/template.go internal/dispatch/channels_internal_test.go
git commit -m "feat(dispatch): add registry-keyed accessors for contact and rendered content"
```

---

## Task 3: De-hardcode `FilterChannelsForTemplate`

**Files:**
- Modify: `internal/dispatch/channels.go:136-158` (the function body) + import block
- Test: `internal/dispatch/channels_internal_test.go` (extend)

- [ ] **Step 1: Add a failing/at-risk test capturing current behavior**

Append to `internal/dispatch/channels_internal_test.go`:

```go
func TestFilterChannelsForTemplate(t *testing.T) {
	nt := &models.NotificationTemplate{
		EmailBody:  sp("e"),
		InboxTitle: sp("i"),
		// SMSBody nil -> sms filtered out
	}
	got := FilterChannelsForTemplate([]string{"email", "sms", "inbox", "bogus"}, nt)
	want := []string{"email", "inbox"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	// nil template passes everything through unchanged.
	all := []string{"email", "sms", "anything"}
	if got := FilterChannelsForTemplate(all, nil); len(got) != 3 {
		t.Fatalf("nil template: got %v, want passthrough", got)
	}
}

func sp(s string) *string { return &s }
```

> Note: `sp` is defined once here; if it already exists in another `package dispatch` (internal) test file, drop this duplicate. (The existing `template_test.go` is `package dispatch_test` with its own `strPtr`, so there is no collision.) Also add `"github.com/hermes-notifications/hermes/internal/models"` to this test file's imports.

- [ ] **Step 2: Run to confirm it passes against the current switch implementation**

Run: `go test ./internal/dispatch/ -run TestFilterChannelsForTemplate -v`
Expected: PASS (this pins the behavior we must preserve).

- [ ] **Step 3: Replace the switch with a registry lookup**

In `internal/dispatch/channels.go`, replace the `FilterChannelsForTemplate` body (lines 136-158):

```go
// FilterChannelsForTemplate filters channels to only those with template
// content defined for them, per the channel registry. For direct sends (nil
// template), all channels pass through. Unknown channels are dropped.
func FilterChannelsForTemplate(channels []string, nt *models.NotificationTemplate) []string {
	if nt == nil {
		return channels
	}
	var filtered []string
	for _, ch := range channels {
		desc, ok := provider.Builtins.Channel(ch)
		if !ok || desc.HasContent == nil {
			continue
		}
		if desc.HasContent(nt) {
			filtered = append(filtered, ch)
		}
	}
	return filtered
}
```

Add `"github.com/hermes-notifications/hermes/internal/provider"` to the import block in `internal/dispatch/channels.go`.

- [ ] **Step 4: Run the test to verify behavior is preserved**

Run: `go test ./internal/dispatch/ -run TestFilterChannelsForTemplate -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dispatch/channels.go internal/dispatch/channels_internal_test.go
git commit -m "refactor(dispatch): de-hardcode FilterChannelsForTemplate via channel registry"
```

---

## Task 4: De-hardcode the contact-info filter

The current filter is **inline** inside the dispatch handler (`internal/dispatch/dispatch.go:287-310`) and emits events as it goes. We extract a pure, testable helper that returns kept channels plus the skips, and keep event emission at the call site — preserving the **exact** log text and event reason strings (which differ today: the log says "user has no email", the event reason says "user has no email address").

**Files:**
- Modify: `internal/dispatch/channels.go` (add `contactSkip` type + `filterChannelsByContact`) + import `hermenats`
- Modify: `internal/dispatch/dispatch.go:287-310` (replace inline block with a call)
- Test: `internal/dispatch/channels_internal_test.go` (extend)

- [ ] **Step 1: Write the failing helper test**

Append to `internal/dispatch/channels_internal_test.go`:

```go
func TestFilterChannelsByContact(t *testing.T) {
	// email + sms required; inbox always kept. Recipient has email only.
	rec := hermenats.Recipient{Email: "a@b.c"}
	kept, skipped := filterChannelsByContact([]string{"email", "sms", "inbox"}, rec)

	wantKept := []string{"email", "inbox"}
	if len(kept) != len(wantKept) || kept[0] != "email" || kept[1] != "inbox" {
		t.Fatalf("kept: got %v, want %v", kept, wantKept)
	}
	if len(skipped) != 1 || skipped[0].Channel != "sms" {
		t.Fatalf("skipped: got %v, want one sms skip", skipped)
	}
	// Exact strings the call site formats from the skip:
	if got := "skipping " + skipped[0].Channel + " channel: user has no " + skipped[0].AddressKey; got != "skipping sms channel: user has no phone" {
		t.Fatalf("log string: got %q", got)
	}
	if got := "user has no " + skipped[0].AddressLabel; got != "user has no phone number" {
		t.Fatalf("event reason: got %q", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails to compile**

Run: `go test ./internal/dispatch/ -run TestFilterChannelsByContact -v`
Expected: FAIL — `undefined: filterChannelsByContact` / `contactSkip`.

- [ ] **Step 3: Add the helper and type to `channels.go`**

Append to `internal/dispatch/channels.go` (and add `hermenats "github.com/hermes-notifications/hermes/internal/nats"` to its imports):

```go
// contactSkip records a channel dropped because the recipient lacks the
// channel's required contact address. AddressKey/AddressLabel are carried so
// the caller can reproduce today's exact log and event-reason strings.
type contactSkip struct {
	Channel      string
	AddressKey   string
	AddressLabel string
}

// filterChannelsByContact keeps only channels whose required contact point is
// present on the recipient. Channels with no address requirement (AddressKey
// "") are always kept, as are unknown channels (matching the prior switch,
// which had no default case). Returns kept channels and the skipped ones.
func filterChannelsByContact(channels []string, recipient hermenats.Recipient) (kept []string, skipped []contactSkip) {
	for _, ch := range channels {
		desc, ok := provider.Builtins.Channel(ch)
		if ok && desc.AddressKey != "" && recipient.AddressFor(desc.AddressKey) == "" {
			skipped = append(skipped, contactSkip{
				Channel:      ch,
				AddressKey:   desc.AddressKey,
				AddressLabel: desc.AddressLabel,
			})
			continue
		}
		kept = append(kept, ch)
	}
	return kept, skipped
}
```

- [ ] **Step 4: Run the helper test to verify it passes**

Run: `go test ./internal/dispatch/ -run TestFilterChannelsByContact -v`
Expected: PASS.

- [ ] **Step 5: Replace the inline block in `dispatch.go`**

In `internal/dispatch/dispatch.go`, replace the block from the comment `// Filter channels that require contact info` through `channels = filteredChannels` (lines 287-310) with:

```go
	// Filter channels that require contact info (per the channel registry).
	filteredChannels, skipped := filterChannelsByContact(channels, recipient)
	for _, s := range skipped {
		log.Warn(fmt.Sprintf("skipping %s channel: user has no %s", s.Channel, s.AddressKey), "user_id", user.ID)
		d.publishEvent(ctx, msg.NotificationID, s.Channel, "routing.no_contact", "warn", map[string]any{
			"reason": "user has no " + s.AddressLabel,
		})
	}
	channels = filteredChannels
```

> Verify `fmt` is already imported in `dispatch.go` (it is used elsewhere in the file). If not, add it.

- [ ] **Step 6: Run the dispatch package tests**

Run: `go test ./internal/dispatch/ -v`
Expected: PASS (all existing + new tests).

- [ ] **Step 7: Commit**

```bash
git add internal/dispatch/channels.go internal/dispatch/dispatch.go internal/dispatch/channels_internal_test.go
git commit -m "refactor(dispatch): de-hardcode contact-info filter via channel registry"
```

---

## Task 5: De-hardcode `contentForChannel`

**Files:**
- Modify: `internal/dispatch/dispatch.go:360-385` (the function body)
- Test: `internal/dispatch/channels_internal_test.go` (extend)

- [ ] **Step 1: Write the behavior-pinning test**

Append to `internal/dispatch/channels_internal_test.go`:

```go
func TestContentForChannel(t *testing.T) {
	url := "https://x"
	original := hermenats.MessageContent{ActionURL: &url}
	rc := &RenderedContent{
		EmailSubject: "es", EmailBody: "eb",
		SMSBody:    "sb",
		InboxTitle: "it", InboxBody: "ib",
	}

	// rendered == nil -> passthrough of original.
	if got := contentForChannel("email", original, nil); got.ActionURL != &url {
		t.Fatal("nil rendered: expected original passthrough")
	}

	email := contentForChannel("email", original, rc)
	if email.Title != "es" || email.Body != "eb" || email.ActionURL != &url {
		t.Fatalf("email: got %+v", email)
	}
	sms := contentForChannel("sms", original, rc)
	if sms.Title != "" || sms.Body != "sb" {
		t.Fatalf("sms: got title=%q body=%q, want title empty", sms.Title, sms.Body)
	}
	inbox := contentForChannel("inbox", original, rc)
	if inbox.Title != "it" || inbox.Body != "ib" {
		t.Fatalf("inbox: got %+v", inbox)
	}
	// unknown channel -> empty title/body (ActionURL still carried).
	bogus := contentForChannel("bogus", original, rc)
	if bogus.Title != "" || bogus.Body != "" || bogus.ActionURL != &url {
		t.Fatalf("bogus: got %+v", bogus)
	}
}
```

- [ ] **Step 2: Run to confirm it passes against the current switch**

Run: `go test ./internal/dispatch/ -run TestContentForChannel -v`
Expected: PASS (pins current behavior).

- [ ] **Step 3: Replace the switch with a registry lookup**

In `internal/dispatch/dispatch.go`, replace the `switch channel { … }` block inside `contentForChannel` (lines 373-382) with:

```go
	if desc, ok := provider.Builtins.Channel(channel); ok {
		mc.Title = rendered.Field(desc.TitleField)
		mc.Body = rendered.Field(desc.BodyField)
	}
```

So the full function reads:

```go
func contentForChannel(channel string, original hermenats.MessageContent, rendered *RenderedContent) hermenats.MessageContent {
	if rendered == nil {
		return original
	}

	mc := hermenats.MessageContent{
		ActionURL:   original.ActionURL,
		ActionLabel: original.ActionLabel,
	}

	if desc, ok := provider.Builtins.Channel(channel); ok {
		mc.Title = rendered.Field(desc.TitleField)
		mc.Body = rendered.Field(desc.BodyField)
	}

	return mc
}
```

> `internal/provider` is already imported in `dispatch.go` from Task 4. If for some reason Tasks were reordered and it isn't, add it.

- [ ] **Step 4: Run the test to verify behavior is preserved**

Run: `go test ./internal/dispatch/ -run TestContentForChannel -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dispatch/dispatch.go internal/dispatch/channels_internal_test.go
git commit -m "refactor(dispatch): de-hardcode contentForChannel via channel registry"
```

---

## Task 6: Full verification + ADR

**Files:**
- Create: `docs/adr/NNNN-provider-plugin-model.md` (next sequential number)

- [ ] **Step 1: Build everything**

Run: `go build ./...`
Expected: exit 0, no output.

- [ ] **Step 2: Run the full unit-test suite**

Run: `make test`
Expected: PASS, no failures. (Confirms the de-hardcoding caused **no behavior change** — existing dispatch/delivery tests still pass untouched.)

- [ ] **Step 3: Lint**

Run: `make lint`
Expected: clean. (Fix any `goimports`/`golangci-lint` findings — likely import ordering in the edited files.)

- [ ] **Step 4: Confirm no stray channel literals remain in the de-hardcoded paths**

Run: `grep -nE '"(email|sms|inbox)"' internal/dispatch/channels.go internal/dispatch/dispatch.go`
Expected: **no** matches inside `FilterChannelsForTemplate`, the contact filter, or `contentForChannel`. (The subject string `"delivery." + ch` and unrelated lines are fine; the three switches must be gone.)

- [ ] **Step 5: Write the ADR**

Use the **superpowers/writing-adrs** skill. Determine the next ADR number from `docs/adr/`, then record the decision: *Provider plugin model & bus-native isolation (channel/provider registry as the de-hardcoding seam; bus-native NATS-subject isolation chosen over gRPC/WASM)*. Reference `docs/superpowers/specs/2026-06-13-provider-plugin-model-design.md`. Mark status `Accepted`. Note that this ADR opens the model; the cross-service NATS subject contract is realized in Phase 3.

> Per CLAUDE.md, architecturally significant decisions get an ADR in the same PR. Phase 1 introduces the registry seam but not yet the NATS contract; recording the overall model now (with the bus-native-vs-alternatives rationale from the spec's "Decisions made during brainstorming") is the right altitude. If the team prefers to defer the bus-native specifics to the Phase 3 PR, scope this ADR to the registry/contract-seam decision and leave isolation as a follow-up ADR — confirm with the reviewer.

- [ ] **Step 6: Commit the ADR**

```bash
git add docs/adr/
git commit -m "docs(adr): record provider plugin model decision"
```

---

## Self-Review

**Spec coverage (Phase 1 only):**
- "interface, registry" → Task 1 (`Registry`, `ChannelDescriptor`, `Manifest`). The `delivery.Provider` interface is left in place unchanged (the spec says "lightly evolved"; no evolution is needed to de-hardcode the switches, so none is done here — evolution lands when provider selection arrives in Phase 3). ✓
- "replace switches" → the three `switch ch` blocks: `FilterChannelsForTemplate` (Task 3), contact filter (Task 4), `contentForChannel` (Task 5). ✓
- "existing channels/providers registered as built-ins" → `provider.Builtins` seeds email/sms/inbox + smtp/ses/sms/inbox (Task 1). ✓
- "No behavior change" → every de-hardcoding task pins prior behavior with a test that passes **before** the edit, and `make test` (Step 6.2) runs the untouched existing suite. ✓
- De-hardcoding goal "adding a channel is registering metadata" → satisfied for channels reusing existing address keys + rendered fields; the residual `AddressFor`/`Field` accessors (legacy-struct shims) are explicitly Phase-2 removals, documented in code and this plan. ✓

**Out of Phase 1 (correctly deferred, not in this plan):** normalized `template_channel_content`/`user_contact_points` (Phase 2), routing policies / `DeliveryPlan` / per-provider subjects / provider *selection* (Phase 3), Redelivery, receipts, health, breaker.

**Placeholder scan:** No TBD/TODO; all steps carry real code and exact run commands. ✓

**Type consistency:** `ChannelDescriptor` fields (`Slug`, `AddressKey`, `AddressLabel`, `TitleField`, `BodyField`, `HasContent`) are used identically in `builtins.go`, the dispatch lookups, and tests. `Manifest{ID, Channel}`, `Registry.Channel`/`RegisterChannel`/`RegisterProvider`/`ProvidersForChannel`, `Recipient.AddressFor`, `RenderedContent.Field`, `contactSkip{Channel, AddressKey, AddressLabel}` — all names match across tasks. ✓

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-06-13-provider-plugin-phase1-channel-registry.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**
