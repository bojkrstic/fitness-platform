# fitnes-platform

Go + Gin aplikacija sa PostgreSQL bazom, auth sistemom, admin panelom, training listom i WebSocket room-ovima za chat/video signaling.

## Struktura

- `cmd/fitness-platform` - entrypoint aplikacije
- `internal/bootstrap` - start i wiring sloj
- `internal/handler` - HTTP rute i renderovanje
- `internal/service` - poslovna logika
- `internal/repository` - PostgreSQL pristup i migracije
- `internal/room` - WebSocket room/hub logika
- `internal/session` - cookie/session handling
- `web/templates` - HTML template-i
- `migrations` - SQL migracije

## Pokretanje
docker compose up --build

### Docker

```bash
make up
```

To podiže:
- PostgreSQL na `localhost:5432`
- aplikaciju na `http://localhost:8080`

### Lokalno

```bash
export DATABASE_URL='postgres://fitness:fitness@localhost:5432/fitness?sslmode=disable'
export PORT=8080
export SEED_ADMIN_EMAIL='admin@fitness.local'
export SEED_ADMIN_PASSWORD='Admin123!'
go build -o fitnes-api ./cmd/fitness-platform
./fitnes-api
```

### Preko Makefile

```bash
make dev
```

## `.env` primer

Ako želiš da koristiš `.env` fajl u root-u projekta:

```env
DATABASE_URL=postgres://fitness:fitness@localhost:5432/fitness?sslmode=disable
PORT=8080
SEED_ADMIN_EMAIL=admin@fitness.local
SEED_ADMIN_PASSWORD=Admin123!
```

## Komande

- `make dev` - start aplikacije lokalno
- `make up` - Docker Compose start
- `make build` - build svih Go paketa
- `make test` - pokretanje testova

## Napomene

- Root `main.go` više ne postoji. Entry point je `cmd/fitness-platform/main.go`.
- Aplikacija se gradi kao `fitnes-api`.
- `make build` pravi lokalni binarni fajl `./fitnes-api`.
- `make up` i `docker compose up --build` podižu bazu i aplikaciju zajedno.
- Aplikacija automatski primenjuje SQL migracije pri startu.
- Ako koristiš `make dev` ili `./fitnes-api` lokalno, PostgreSQL mora biti dostupan na adresi iz `DATABASE_URL`.
