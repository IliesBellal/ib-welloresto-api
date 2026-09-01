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

This is a **Go REST API** for a multi-tenant restaurant management platform. Entry point: [cmd/api/main.go](cmd/api/main.go) — loads config from env, initializes the database and Zap logger, then calls `SetupRoutes()`.

**Stack:** Chi router, PostgreSQL (`database/sql`, no ORM), Redis, WebSockets (gorilla), Stripe, Uber Eats/Deliveroo integrations, Cloudflare R2 storage, Brevo (email/SMS), FCM push notifications.

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

**PostgreSQL is the only database engine in production** (confirmed 2026-09-01) — the MySQL → Postgres migration documented under [docs/migration-postgres/](docs/migration-postgres/) is complete; MySQL is no longer live anywhere. Connection pool: **15 max open, 4 idle, 5-minute lifetime** — see [internal/database/postgres.go](internal/database/postgres.go) (the old "1 open + 1 idle, 3-minute lifetime" figure was a MySQL/Hostinger-specific constraint, now dead code — see [internal/database/mysql.go](internal/database/mysql.go)). The codebase still carries a `DB_DIALECT` switch (`internal/database/dbx`, `dbx.ActiveDialect()`) and MySQL-flavored code paths/env vars (`MYSQL_URL`) from the migration itself, and `migrations/` still contains historical MySQL-era files (`migrations/done/`) alongside the current Postgres migrations (`migrations/todo/`, syntax like `DROP COLUMN IF EXISTS`, `to_regclass` guards) — but do not write new MySQL-specific code, and never assume MySQL is a live target when reasoning about schema changes or deployment order. Queries are written by hand in repository files, no ORM. Soft deletes use `enabled = false`.

### Auth & permissions

Token-based auth validated against Redis. Tokens injected into request context by the auth middleware, then checked by the permissions middleware (RBAC). Permission helpers live in [internal/middleware/permissions.go](internal/middleware/permissions.go) and are applied per-route via `middleware.RequirePermission(middleware.HasMenuAccess)` or combined with `AnyOf`/`AllOf`. MFA (SMS/email) and email verification are supported. See `internal/middleware/` and `internal/modules/auth/`.

### AI layer

`internal/ai/` provides a provider-agnostic LLM abstraction. A `Registry` is built at startup in `SetupRoutes` by `buildAIRegistry()` and injected into services that need LLM features. Tasks (`menu_translation`, `upsell`) are mapped to providers and models via env vars. Response caching goes through a Redis-backed `aicache.Cache`. Add new tasks in [internal/config/ai.go](internal/config/ai.go) and new providers in [internal/ai/providers/](internal/ai/providers/).

### Real-time & background jobs

WebSocket hub in `internal/infrastructure/websocket/` for live order events. Background task logic lives in `internal/tasks/` as a `TasksManager`; cron scheduling is wired in [cmd/api/tasks.go](cmd/api/tasks.go) and runs unconditionally on every environment — `SetupTasks` has no early `return` and no `ENV`-based gate, and `SetupRoutes` always calls it (`cmd/api/routes.go`). All jobs (`@hourly`, `@every 1m`, `@every 15m`, etc.) are registered and started on both `staging` and production; there is no per-environment disablement (confirmed on `staging` — see [docs/migration-postgres/54-tasks-full-execution-validation.md](docs/migration-postgres/54-tasks-full-execution-validation.md)). The `TasksManager` is also exposed to the `admin/upsell` HTTP endpoint for manual triggers.

### Webhooks

Inbound webhooks from external platforms are handled in `internal/webhook/` (not `internal/modules/`). Each platform (Stripe, Uber Eats, Deliveroo orders, Deliveroo menu) has its own sub-package with handler/service/repository. Routes are registered under `/webhooks/`.

### Multi-tenant scoping

Every authenticated request is scoped to a merchant via the auth token. Repository queries filter by merchant ID extracted from the request context — there is no cross-tenant data access in normal flows.

### Environment variables

Required at runtime (no `.env` in repo):
- `POSTGRES_URL` — Postgres connection string (the live one — set `DB_DIALECT=postgres`; `MYSQL_URL` still exists as a legacy/dialect-switch fallback in code but has no live target)
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
