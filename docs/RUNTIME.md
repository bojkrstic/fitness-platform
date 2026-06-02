# Runtime and Docker

## Local run

Build and run the binary:

```bash
go build -o fitnes-api ./cmd/fitness-platform
./fitnes-api
```

Useful env vars:

- `DATABASE_URL`
- `PORT`
- `SEED_ADMIN_EMAIL`
- `SEED_ADMIN_PASSWORD`

Default values:

- `PORT=8080`
- `SEED_ADMIN_EMAIL=admin@fitness.local`
- `SEED_ADMIN_PASSWORD=Admin123!`

## Makefile

- `make dev` runs the app with `go run ./cmd/fitness-platform`
- `make build` creates `./fitnes-api`
- `make build-linux` creates a Linux `./fitnes-api`
- `make test` runs Go tests
- `make up` runs Docker Compose with build

## Docker Compose

- `db` runs PostgreSQL 16
- `app` builds from `Dockerfile`
- app connects to `db` through `DATABASE_URL=postgres://fitness:fitness@db:5432/fitness?sslmode=disable`
- app exposes port `8080`

## Dockerfile

- Multi-stage build
- Build stage compiles `./cmd/fitness-platform`
- Runtime stage is Alpine
- Runtime image includes:
  - compiled binary
  - `web/templates`
  - `migrations`

