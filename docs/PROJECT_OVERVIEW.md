# Project Overview

## Purpose

Fitnes Platform is a small Go web app for:

- user login and registration
- admin-only training creation
- browsing training list and room pages
- websocket room signaling for chat/video

## Tech stack

- Go
- Gin HTTP framework
- PostgreSQL
- `html/template`
- Gorilla WebSocket

## Runtime shape

- Entrypoint: `cmd/fitness-platform/main.go`
- Bootstrap: `internal/bootstrap/run.go`
- HTTP handling: `internal/handler`
- Business logic: `internal/service`
- Database access: `internal/repository`
- Session handling: `internal/session`
- Realtime room logic: `internal/room`
- Templates: `web/templates`
- Migrations: `migrations`

## Important behavior

- Database schema is created and migrated at startup.
- A seed admin is created or updated at startup from env vars.
- Default trainings are inserted if the database is empty.
- Sessions are stored in memory and keyed by cookie.
- Template rendering is split per page to avoid template collision.

