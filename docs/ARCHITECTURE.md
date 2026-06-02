# Architecture

## Layering

The project uses a simple layered structure:

1. `cmd/fitness-platform` starts the app.
2. `internal/bootstrap` wires config, DB, store, service, and handler.
3. `internal/handler` owns routes and HTML responses.
4. `internal/service` owns business rules.
5. `internal/repository` owns PostgreSQL access and migrations.
6. `internal/session` owns auth cookies and in-memory session data.
7. `internal/room` owns websocket rooms and message fanout.

## Request flow

- HTTP request enters Gin router in `internal/handler`.
- Handler loads current user from session cookie when needed.
- Handler calls service methods for validation and domain operations.
- Service calls repository methods for DB reads/writes.
- Handler renders page templates or redirects on success.

## Data flow

- `internal/config.Load()` reads `.env` if present, then environment variables.
- `database/sql` is opened through `repository.OpenDatabase()`.
- `repository.Init()` applies SQL files from `migrations/` in lexical order.
- `service.Init()` seeds admin and default trainings.
- `handler.NewHttpHandler()` prepares routes and template sets.

## Realtime flow

- `/ws/room/:id` upgrades to websocket.
- Room membership is tracked in memory in `internal/room`.
- Participants and history are broadcast when a user joins.
- Chat and signaling messages are relayed through the room hub.

