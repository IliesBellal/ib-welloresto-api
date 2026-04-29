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
| `internal/modules/` | 28+ business modules (auth, orders, menu, pos, deliveroo, ubereats, etc.) |
| `internal/middleware/` | CORS, auth token extraction, RBAC permissions, request audit logging |
| `internal/infrastructure/` | External service clients (stripe, redis, r2, brevo, websocket) |
| `internal/models/` | Shared DTOs and data structures |
| `internal/config/` | Env-var config loading |
| `migrations/` | Raw SQL migration files (no migration tool — run manually) |
| `docs/` | Deep-dive documentation for complex modules |

### Database

MySQL via `database/sql`. Connection is intentionally capped at **1 open + 1 idle connection** with a 3-minute lifetime (Hostinger hosting constraint) — see [internal/database/mysql.go](internal/database/mysql.go). No ORM; queries are written by hand in repository files. Soft deletes use `enabled = 0`.

### Auth & permissions

Token-based auth validated against Redis. Tokens injected into request context by the auth middleware, then checked by the permissions middleware (RBAC). MFA (SMS/email) and email verification are supported. See `internal/middleware/` and the `internal/modules/auth/` module.

### Real-time & background jobs

WebSocket hub in `internal/infrastructure/websocket/` for live order events. Cron jobs defined in [cmd/api/tasks.go](cmd/api/tasks.go) — currently disabled (early `return` at the top of the setup function).

### Environment variables

Required at runtime (no `.env` in repo):
- `MYSQL_URL` — MySQL connection string
- `PORT` — defaults to `8080`
- `ENV` — `local` or `production` (affects log level)
- Stripe, Uber Eats, Deliveroo, Brevo, Cloudflare R2, Google API credentials
