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

```bash
docker compose up --build
```

Nakon starta aplikacija je dostupna na:

```text
http://localhost:8020
```

Primer training URL-a:

```text
http://localhost:8020/trainings/3c4efed8f37fca7b0d3afbaf5401b859
```

### Docker

```bash
make up
```

To podiže:
- PostgreSQL na `localhost:5433`
- aplikaciju na `http://localhost:8020`

Unutar Docker mreže aplikacija se kači na bazu preko adrese `db:5432`.
Sa host mašine baza je dostupna na `localhost:5433`.

### Lokalno

Ako aplikaciju pokrećeš lokalno preko `go run`, `make dev` ili lokalnog binarnog fajla, a bazu preko Docker Compose-a, koristi `localhost:5433`:

```bash
export DATABASE_URL='postgres://fitness:fitness@localhost:5433/fitness?sslmode=disable'
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
DATABASE_URL=postgres://fitness:fitness@localhost:5433/fitness?sslmode=disable
PORT=8080
SEED_ADMIN_EMAIL=admin@fitness.local
SEED_ADMIN_PASSWORD=Admin123!
WEBRTC_ICE_SERVERS=[{"urls":"stun:stun.l.google.com:19302"}]
```

Za video između različitih mreža potreban je TURN server. Tada `WEBRTC_ICE_SERVERS`
treba da sadrži i `turn:`/`turns:` adresu:

```env
WEBRTC_ICE_SERVERS=[{"urls":"stun:stun.l.google.com:19302"},{"urls":"turns:turn.example.com:5349","username":"turn-user","credential":"turn-password"}]
```

STUN često radi samo u istoj mreži ili iza jednostavnog NAT-a. TURN prosleđuje
WebRTC media saobraćaj kada browseri ne mogu direktno da uspostave peer-to-peer
vezu.

## Komande

- `make dev` - start aplikacije lokalno
- `make up` - Docker Compose start
- `make build` - build svih Go paketa
- `make test` - pokretanje testova
- `make docker-build DOCKER_IMAGE=dockerhub-user/fitness-platform DOCKER_TAG=latest` - build Docker image-a
- `make docker-push DOCKER_IMAGE=dockerhub-user/fitness-platform DOCKER_TAG=latest` - build i push na Docker Hub

## Docker Hub publish

Lokalno:

```bash
docker login
make docker-push DOCKER_IMAGE=dockerhub-user/fitness-platform DOCKER_TAG=latest
```

Preko GitHub Actions workflow-a `.github/workflows/docker-hub.yml` podesi:

- secret `DOCKERHUB_USERNAME` - Docker Hub username
- secret `DOCKERHUB_TOKEN` - Docker Hub access token
- opciono variable `DOCKERHUB_IMAGE` - ime repozitorijuma, npr. `fitness-platform`

Workflow objavljuje:
- `latest` za push na `main`
- git tag, npr. `v1.0.0`
- `sha-...` tag za svaki build

## Docker deploy na serveru

Image koji se koristi na serveru:

```text
bojankrlekrstic/fitness-platform:version1.1.4
```

Ako prvi put podižeš aplikaciju bez `docker compose`, napravi network i PostgreSQL
kontejner:

```bash
docker network create fitness-platform_global

docker run -d \
  --name fitness-platform-db-1 \
  --network fitness-platform_global \
  -e POSTGRES_DB=fitness \
  -e POSTGRES_USER=fitness \
  -e POSTGRES_PASSWORD=fitness \
  -v fitness_pgdata:/var/lib/postgresql/data \
  postgres:16-alpine
```

Kada postoji nova verzija na Docker Hub-u, na serveru:

```bash
docker ps
```

Obori prethodni app kontejner:

```bash
docker rm -f fitness-platform-app-1
```

Pokreni novu verziju:

```bash
docker run -d \
  --name fitness-platform-app-1 \
  --network fitness-platform_global \
  -p 8020:8080 \
  -e DATABASE_URL='postgres://fitness:fitness@fitness-platform-db-1:5432/fitness?sslmode=disable' \
  -e PORT=8080 \
  -e SEED_ADMIN_EMAIL='admin@fitness.local' \
  -e SEED_ADMIN_PASSWORD='Admin123!' \
  -e WEBRTC_ICE_SERVERS='[{"urls":"stun:stun.l.google.com:19302"}]' \
  bojankrlekrstic/fitness-platform:version1.1.4
```

Ako koristiš TURN server za video između različitih mreža, pokreni sa
`WEBRTC_ICE_SERVERS` koji ima i `turn:`/`turns:` adresu:

```bash
docker run -d \
  --name fitness-platform-app-1 \
  --network fitness-platform_global \
  -p 8020:8080 \
  -e DATABASE_URL='postgres://fitness:fitness@fitness-platform-db-1:5432/fitness?sslmode=disable' \
  -e PORT=8080 \
  -e SEED_ADMIN_EMAIL='admin@fitness.local' \
  -e SEED_ADMIN_PASSWORD='Admin123!' \
  -e WEBRTC_ICE_SERVERS='[{"urls":"stun:stun.l.google.com:19302"},{"urls":"turns:turn.example.com:5349","username":"turn-user","credential":"turn-password"}]' \
  bojankrlekrstic/fitness-platform:version1.1.4
```

Proveri logove:

```bash
docker logs fitness-platform-app-1 --tail=50
```

Aplikacija je tada dostupna na:

```text
http://SERVER_IP:8020
```

Za kameru preko spoljne adrese koristi HTTPS domen. Browser kamera neće raditi
preko običnog `http://SERVER_IP`, osim na `localhost`.

## Napomene

- Root `main.go` više ne postoji. Entry point je `cmd/fitness-platform/main.go`.
- Aplikacija se gradi kao `fitnes-api`.
- `make build` pravi lokalni binarni fajl `./fitnes-api`.
- `make up` i `docker compose up --build` podižu bazu i aplikaciju zajedno.
- Aplikacija automatski primenjuje SQL migracije pri startu.
- Ako koristiš `make dev` ili `./fitnes-api` lokalno, PostgreSQL mora biti dostupan na adresi iz `DATABASE_URL`.
- Ako koristiš Docker Compose aplikaciju, otvaraj `http://localhost:8020`, jer je container port `8080` mapiran na host port `8020`.

evo za dockerhub ponavljam:
Docker hub
  Kada se napravi nova verzija, onda se pokrene ./build-and-push.sh samo se promeni verzija v1.1.2 recimo
  na taj nacin se formira izvrsna verzija i prebaci u dockerhub, na lokaciji https://app.docker.com/accounts/bojankrlekrstic
  user: bojankrlekrstic
  bitno je da se ulogujes na dockerhub preko docker login ili Docker Hub access token-a


github je znaci za server tamo se samo nalazi kod nista drugo
 ->  obavezno za github obrati paznju na grane, develop i main

## Kratke komande za deploy fitness aplikacije

```bash
docker ps | grep fitness
```

```bash
docker rm -f fitness-platform-app-1
```

```bash
docker run -d --name fitness-platform-app-1 --network fitness-platform_global -p 8020:8080 -e DATABASE_URL='postgres://fitness:fitness@fitness-platform-db-1:5432/fitness?sslmode=disable' -e PORT=8080 -e SEED_ADMIN_EMAIL='admin@fitness.local' -e SEED_ADMIN_PASSWORD='Admin123!' -e WEBRTC_ICE_SERVERS='[{"urls":"stun:stun.l.google.com:19302"}]' bojankrlekrstic/fitness-platform:version1.1.4
```
