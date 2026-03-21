# OpenAPI Spec Generation Design

## Context

Hermes has no machine-readable API documentation. The only docs are a manual markdown integration guide. We need publishable OpenAPI specs that stay in sync with the actual API code, so consumers can generate clients and explore the API.

## Decision

Use **swaggo/swag** to generate OpenAPI 2.0 specs from Go comment annotations on handler functions. Two separate specs split by auth model:

- **Admin API** (API key auth) — server-to-server endpoints
- **User API** (JWT auth) — Inbox + User Service endpoints combined

Generated specs are committed to the repo. A CI check regenerates and diffs to catch drift.

## Spec Organization

```
api/
  admin/
    swagger.json
    swagger.yaml
    docs.go
  user/
    swagger.json
    swagger.yaml
    docs.go
```

### Entry Points

**Admin spec:** Top-level annotations go on `cmd/admin/main.go`. swag parses `internal/admin/` handlers automatically.

**User spec:** A new `cmd/swagger-user/doc.go` file holds the top-level annotations (`@title`, `@version`, `@securityDefinitions.apiKey`). swag's `--parseDir` flag points it at `internal/inbox/` and `internal/userservice/`.

`cmd/swagger-user/doc.go` is not a runnable service — it's purely a swag entry point. It contains only a package declaration and the doc comment block.

## Endpoints to Annotate

### Admin API (10 endpoints)

| Method | Path | Handler | File |
|--------|------|---------|------|
| GET | /v1/groups | handleListGroups | internal/admin/handler_groups.go |
| POST | /v1/groups | handleCreateGroup | internal/admin/handler_groups.go |
| PUT | /v1/groups/{id} | handleUpdateGroup | internal/admin/handler_groups.go |
| GET | /v1/types | handleListTypes | internal/admin/handler_types.go |
| POST | /v1/types | handleCreateType | internal/admin/handler_types.go |
| PUT | /v1/types/{id} | handleUpdateType | internal/admin/handler_types.go |
| DELETE | /v1/types/{id} | handleDeleteType | internal/admin/handler_types.go |
| POST | /v1/send | handleSend | internal/admin/handler_send.go |
| GET | /v1/notifications/{id} | handleGetNotification | internal/admin/handler_notifications.go |
| POST | /v1/auth/token | handleAuthToken | internal/admin/handler_auth.go |

Health endpoints (`/healthz`, `/readyz`) excluded — not part of public API.

### User API (12 endpoints)

| Method | Path | Handler | File |
|--------|------|---------|------|
| GET | /v1/inbox | handleListInbox | internal/inbox/handler_list.go |
| PUT | /v1/inbox/read-all | handleMarkAllRead | internal/inbox/handler_actions.go |
| PUT | /v1/inbox/{id}/read | handleMarkRead | internal/inbox/handler_actions.go |
| DELETE | /v1/inbox/{id}/read | handleMarkUnread | internal/inbox/handler_actions.go |
| PUT | /v1/inbox/{id}/archive | handleArchive | internal/inbox/handler_actions.go |
| DELETE | /v1/inbox/{id}/archive | handleUnarchive | internal/inbox/handler_actions.go |
| DELETE | /v1/inbox/{id} | handleSoftDelete | internal/inbox/handler_actions.go |
| GET | /v1/users/me | handleGetProfile | internal/userservice/handler_profile.go |
| PUT | /v1/users/me/contacts | handleUpdateContacts | internal/userservice/handler_profile.go |
| GET | /v1/users/me/preferences | handleListPreferences | internal/userservice/handler_preferences.go |
| PUT | /v1/users/me/preferences/{group_id} | handleSetPreference | internal/userservice/handler_preferences.go |
| DELETE | /v1/users/me/preferences/{group_id} | handleDeletePreference | internal/userservice/handler_preferences.go |

## Annotation Pattern

Each handler gets a swag comment block above the function signature:

```go
// @Summary List notification groups
// @Tags groups
// @Produce json
// @Success 200 {array} models.NotificationGroup
// @Failure 500 {object} map[string]string
// @Router /v1/groups [get]
// @Security ApiKeyAuth
func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
```

Request body params use `@Param`:
```go
// @Param body body admin.createGroupRequest true "Group to create"
```

Path params:
```go
// @Param id path string true "Group ID"
```

Query params (inbox list):
```go
// @Param archived query bool false "Filter archived notifications"
// @Param cursor query string false "Pagination cursor"
// @Param limit query int false "Page size (default 20)"
```

## Security Definitions

**Admin spec** (`cmd/admin/main.go`):
```go
// @securityDefinitions.apiKey ApiKeyAuth
// @in header
// @name Authorization
```

**User spec** (`cmd/swagger-user/doc.go`):
```go
// @securityDefinitions.apiKey BearerAuth
// @in header
// @name Authorization
// @description JWT token (Bearer <token>)
```

## Makefile Targets

```makefile
swagger:           ## Generate OpenAPI specs
	swag init -g cmd/admin/main.go -o api/admin --parseDependency
	swag init -g cmd/swagger-user/doc.go -o api/user --parseDir internal/inbox,internal/userservice --parseDependency

swagger-check:     ## Verify specs are up to date (for CI)
	$(MAKE) swagger
	git diff --exit-code api/
```

## Dependencies

Add to go.mod:
- `github.com/swaggo/swag/v2` (or latest v1 — check current stable)

Install CLI tool:
```bash
go install github.com/swaggo/swag/cmd/swag@latest
```

## What We're NOT Doing

- **No Swagger UI served from services** — specs are static files only
- **No health endpoint docs** — internal operational endpoints
- **No runtime validation** — swag generates docs, not request validators
- **No OpenAPI 3.0** — swag generates OAS 2.0 natively; conversion can be done later if needed

## Verification

1. Run `make swagger` — should generate specs without errors
2. Open `docs/swagger/admin/swagger.json` — verify all 10 admin endpoints present
3. Open `docs/swagger/user/swagger.json` — verify all 12 user endpoints present
4. Run `make swagger-check` — should exit 0 (no diff)
5. Paste a generated spec into https://editor.swagger.io to visually verify
6. Run `make test` — ensure annotations don't break compilation
