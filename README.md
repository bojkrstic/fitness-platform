# fitnes-platform

Go + Gin aplikacija sa PostgreSQL bazom, auth sistemom, admin panelom, training listom i WebSocket room-ovima za chat/video signaling.

Detalji za snimanje i deployment su izdvojeni u:
- [`docs/RECORDINGS.md`](./docs/RECORDINGS.md)
- [`docs/DEPLOYMENT.md`](./docs/DEPLOYMENT.md)

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

Ako aplikaciju pokrećeš lokalno preko `go run`, `make dev`, `make local` ili lokalnog binarnog fajla, a bazu preko Docker Compose-a, koristi `localhost:5433`:

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
make local
```

Ili:

```bash
make dev
```

## `.env` primer

Ako želiš da koristiš `.env` fajl u root-u projekta:

```env
DATABASE_URL=postgres://fitness:fitness@localhost:5433/fitness?sslmode=disable
PORT=8080
APP_BASE_URL=http://localhost:8080
SEED_ADMIN_EMAIL=admin@fitness.local
SEED_ADMIN_PASSWORD=Admin123!
WEBRTC_TURN_HOST=localhost
WEBRTC_TURN_PORT=3478
WEBRTC_TURN_USERNAME=fitness
WEBRTC_TURN_PASSWORD=change-this-turn-password
WEBRTC_TURN_REALM=fitness-platform
WEBRTC_TURN_EXTERNAL_IP=127.0.0.1
STRIPE_SECRET_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...
STRIPE_PRICE_ID=price_...
GCS_RECORDINGS_BUCKET=fitness-recordings-bucket
GCS_SERVICE_ACCOUNT_EMAIL=recordings-uploader@PROJECT_ID.iam.gserviceaccount.com
GCS_PRIVATE_KEY="-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n"
RECORDINGS_DIR=recordings
```

Billing uses Stripe Checkout for subscription purchases, Stripe webhooks for state sync, and the Stripe customer portal for subscription management.

For local Docker Compose runs, `APP_BASE_URL` defaults to `http://localhost:8020`, so you only need to supply the three Stripe values to enable billing.

Za video između različitih mreža potreban je TURN server. Docker Compose podiže
Coturn servis i aplikaciji prosleđuje TURN konfiguraciju preko `WEBRTC_TURN_*`
varijabli.

Na serveru promeni:
- `WEBRTC_TURN_HOST` - domen ili javni IP koji browser korisnika može da dosegne.
- `WEBRTC_TURN_EXTERNAL_IP` - javni IP servera koji Coturn oglašava u WebRTC kandidatima.
- `WEBRTC_TURN_PASSWORD` - jaka lozinka, ne ostavljati demo vrednost.

STUN često radi samo u istoj mreži ili iza jednostavnog NAT-a. TURN prosleđuje
WebRTC media saobraćaj kada browseri ne mogu direktno da uspostave peer-to-peer
vezu.

## Snimanje treninga na Google Cloud Storage

Snimanje koristi browser `MediaRecorder`. Admin snima svoj lokalni video/audio
stream, browser uploaduje `.webm` fajl direktno na Google Cloud Storage, a
aplikacija u PostgreSQL čuva samo metadata snimka.

Potrebne varijable:

```env
GCS_RECORDINGS_BUCKET=fitness-recordings-bucket
GCS_SERVICE_ACCOUNT_EMAIL=recordings-uploader@PROJECT_ID.iam.gserviceaccount.com
GCS_PRIVATE_KEY="-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n"
```

Service account treba pravo da upisuje i čita objekte u bucket-u, npr.
`Storage Object User` za konkretan bucket. Bucket treba da ostane privatan;
aplikacija izdaje kratkotrajne signed URL linkove za upload, gledanje i
download.

Ako GCS varijable nisu podešene, aplikacija koristi lokalni storage u
`RECORDINGS_DIR` direktorijumu. To znači da recording radi odmah i u dev
okruženju bez dodatnog cloud setup-a.

Ako koristiš GCS, browser upload chunkova zahteva CORS pravilo na bucket-u.
Backend otvara GCS resumable session, tako da browser ne mora da cita
`Location` header sa GCS `POST` odgovora; browser direktno radi samo `PUT`
chunk upload.

```json
[
  {
    "origin": ["http://localhost:8020", "https://tvoj-domen.example"],
    "method": ["PUT", "GET"],
    "responseHeader": ["Content-Type", "Content-Disposition", "Content-Range", "Range"],
    "maxAgeSeconds": 3600
  }
]
```

Primena preko `gcloud`:

```bash
gcloud storage buckets update gs://fitness-recordings-bucket --cors-file=cors.json
```

Ako je storage lokalni, CORS ti ne treba.

Snimci se organizuju po datumu i treningu:

```text
recordings/YYYY/MM/DD/{training_id}/{recording_id}.webm
```

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
bojankrlekrstic/fitness-platform-svc:version1.1.5
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
  bojankrlekrstic/fitness-platform-svc:version1.1.5
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
  bojankrlekrstic/fitness-platform-svc:version1.1.5
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
docker run -d --name fitness-platform-app-1 --network fitness-platform_global -p 8020:8080 -e DATABASE_URL='postgres://fitness:fitness@fitness-platform-db-1:5432/fitness?sslmode=disable' -e PORT=8080 -e SEED_ADMIN_EMAIL='admin@fitness.local' -e SEED_ADMIN_PASSWORD='Admin123!' -e WEBRTC_ICE_SERVERS='[{"urls":"stun:stun.l.google.com:19302"}]' bojankrlekrstic/fitness-platform-svc:version1.1.5
```

Kako se pokrece na serveru:
pre toga se pokrene ./build-and-push.sh sa novom verzijom i onda se krene dalje, jer se ovako kreira docker hub nova verzija

1. docker ps
2. docker rm -f fitness-platform-app-1
3. docker pull bojankrlekrstic/fitness-platform-svc:version1.1.5
4. docker run -d --name fitness-platform-app-1 --network fitness-platform_default -p 8020:8080 --env-file .env -e DATABASE_URL="postgres://fitness:fitness@fitness-platform-db-1:5432/fitness?sslmode=disable" bojankrlekrstic/fitness-platform-svc:version1.1.5
5. curl http://localhost:8020    -> ovo je test
