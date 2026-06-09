# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build ./cmd/api

# Run all tests in a module
go test ./internal/modules/availabilities/...

# Run a specific test
go test ./internal/modules/availabilities/... -run TestIsProductAvailable_NoAvailabilities

# Run tests with coverage
go test ./internal/modules/availabilities/... -cover

# Run tests for all modules
go test ./internal/...
```

## Architecture

This is a **Go REST API** for a multi-tenant restaurant management platform. Entry point: [cmd/api/main.go](cmd/api/main.go) — loads config from env, initializes MySQL and Zap logger, then calls `SetupRoutes()`.

**Stack:** Chi router, MySQL (`database/sql`, no ORM), Redis, WebSockets (gorilla), Stripe, Uber Eats/Deliveroo integrations, Cloudflare R2 storage, Brevo (email/SMS), FCM push notifications.

### Layer pattern

Every module under `internal/modules/` follows the same 3-layer structure:

```
Handler (HTTP) → Service (Business logic) → Repository (SQL)
```

Files within a module: `handler.go`, `service.go`, `repository.go`, `models.go`. All wired together via constructor-based DI in [cmd/api/routes.go](cmd/api/routes.go).

### Key directories

| Path | Purpose |
|---|---|
| `cmd/api/` | Entry point, route registration, cron task setup |
| `internal/modules/` | 30+ business modules (auth, orders, menu, pos, planning, haccp, upsell, translation, etc.) |
| `internal/middleware/` | CORS, auth token extraction, RBAC permissions, request audit logging |
| `internal/infrastructure/` | External service clients (stripe, redis, r2, brevo, websocket) |
| `internal/tasks/` | Background task implementations (orders, payments, notifications, upsell); wired via `TasksManager` |
| `internal/webhook/` | Inbound webhook handlers for Stripe, Uber Eats, Deliveroo (each has its own handler/service/repository) |
| `internal/ai/` | LLM abstraction layer: `Registry`, `LLMProvider` interface, provider implementations (Anthropic, OpenAI), Redis cache |
| `internal/models/` | Shared DTOs and data structures |
| `internal/config/` | Env-var config loading |
| `migrations/` | Raw SQL migration files (no migration tool — run manually) |
| `docs/` | Deep-dive documentation for complex modules |

### Database

MySQL via `database/sql`. Connection is intentionally capped at **1 open + 1 idle connection** with a 3-minute lifetime (Hostinger hosting constraint) — see [internal/database/mysql.go](internal/database/mysql.go). No ORM; queries are written by hand in repository files. Soft deletes use `enabled = 0`.

### Auth & permissions

Token-based auth validated against Redis. Tokens injected into request context by the auth middleware, then checked by the permissions middleware (RBAC). Permission helpers live in [internal/middleware/permissions.go](internal/middleware/permissions.go) and are applied per-route via `middleware.RequirePermission(middleware.HasMenuAccess)` or combined with `AnyOf`/`AllOf`. MFA (SMS/email) and email verification are supported. See `internal/middleware/` and `internal/modules/auth/`.

### AI layer

`internal/ai/` provides a provider-agnostic LLM abstraction. A `Registry` is built at startup in `SetupRoutes` by `buildAIRegistry()` and injected into services that need LLM features. Tasks (`menu_translation`, `upsell`) are mapped to providers and models via env vars. Response caching goes through a Redis-backed `aicache.Cache`. Add new tasks in [internal/config/ai.go](internal/config/ai.go) and new providers in [internal/ai/providers/](internal/ai/providers/).

### Real-time & background jobs

WebSocket hub in `internal/infrastructure/websocket/` for live order events. Background task logic lives in `internal/tasks/` as a `TasksManager`; cron scheduling is wired in [cmd/api/tasks.go](cmd/api/tasks.go) — currently disabled (early `return` at the top of `SetupTasks`). The `TasksManager` is also exposed to the `admin/upsell` HTTP endpoint for manual triggers.

### Webhooks

Inbound webhooks from external platforms are handled in `internal/webhook/` (not `internal/modules/`). Each platform (Stripe, Uber Eats, Deliveroo orders, Deliveroo menu) has its own sub-package with handler/service/repository. Routes are registered under `/webhooks/`.

### Multi-tenant scoping

Every authenticated request is scoped to a merchant via the auth token. Repository queries filter by merchant ID extracted from the request context — there is no cross-tenant data access in normal flows.

### Environment variables

Required at runtime (no `.env` in repo):
- `MYSQL_URL` — MySQL connection string
- `GOOGLE_API_KEY` — required (validated at startup)
- `R2_PRIVATE_BUCKET` — required (validated at startup)
- `PORT` — defaults to `8080`
- `ENV` — `local` or `production` (affects log level)
- Stripe: `STRIPE_API_KEY`, `STRIPE_ONBOARDING_RETURN_URL`, `STRIPE_ONBOARDING_REFRESH_URL`
- Uber Eats, Deliveroo, Brevo, Cloudflare R2, FCM credentials
- AI: `ANTHROPIC_API_KEY`, `OPENAI_API_KEY` (optional — AI features error at runtime without them)
- AI task overrides: `AI_TASK_MENU_TRANSLATION_PROVIDER/MODEL/TEMPERATURE/MAX_TOKENS`, `AI_TASK_UPSELL_PROVIDER/MODEL/TEMPERATURE/MAX_TOKENS`

### Deprecations

The `/customer` route prefix is deprecated (since 2024-06-25) in favour of `/customers`. Both are currently registered; remove `/customer` once all clients migrate.
