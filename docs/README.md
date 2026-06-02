# Fitnes Platform Docs

Read this first when you reopen the repo with Codex.

## What this app is

- Go web app with PostgreSQL, HTML templates, login/register, admin training CRUD, and WebSocket room signaling.
- Runtime entrypoint is `cmd/fitness-platform`.
- Local binary name is `fitnes-api`.
- Docker Compose starts the app and database together.

## Start here

1. [Project Overview](./PROJECT_OVERVIEW.md)
2. [Architecture](./ARCHITECTURE.md)
3. [Runtime and Docker](./RUNTIME.md)
4. [Auth and Sessions](./AUTH_AND_SESSIONS.md)

## Working rules

- `main.go` lives only under `cmd/fitness-platform`.
- HTTP rendering uses `web/templates/layout.html` plus one page template per view.
- Sessions are in-memory, cookie-backed, and lost on process restart.
- Database schema is created and migrated on app start from `migrations/`.

