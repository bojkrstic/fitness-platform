#!/usr/bin/env bash
set -euo pipefail

APP_CONTAINER="${APP_CONTAINER:-fitness-platform-app-1}"
DOCKER_IMAGE="${DOCKER_IMAGE:-bojankrlekrstic/fitness-platform-svc}"
DOCKER_TAG="${DOCKER_TAG:-version1.1.5}"
FULL_IMAGE="${DOCKER_IMAGE}:${DOCKER_TAG}"
DOCKER_NETWORK="${DOCKER_NETWORK:-fitness-platform_default}"
PORT_BINDING="${PORT_BINDING:-8020:8080}"
ENV_FILE="${ENV_FILE:-.env}"
RECORDINGS_HOST_DIR="${RECORDINGS_HOST_DIR:-$PWD/recordings}"
DATABASE_URL="${DATABASE_URL:-postgres://fitness:fitness@fitness-platform-db-1:5432/fitness?sslmode=disable}"

mkdir -p "$RECORDINGS_HOST_DIR"

if docker container inspect "$APP_CONTAINER" >/dev/null 2>&1; then
	echo "Copying existing recordings from $APP_CONTAINER to $RECORDINGS_HOST_DIR ..."
	docker cp "$APP_CONTAINER:/app/recordings/." "$RECORDINGS_HOST_DIR/" >/dev/null 2>&1 || true
fi

echo "Pulling $FULL_IMAGE ..."
docker pull "$FULL_IMAGE"

if docker container inspect "$APP_CONTAINER" >/dev/null 2>&1; then
	echo "Removing old container $APP_CONTAINER ..."
	docker rm -f "$APP_CONTAINER"
fi

echo "Starting $APP_CONTAINER ..."
docker run -d \
	--name "$APP_CONTAINER" \
	--network "$DOCKER_NETWORK" \
	-p "$PORT_BINDING" \
	--env-file "$ENV_FILE" \
	-e "DATABASE_URL=$DATABASE_URL" \
	-e "RECORDINGS_DIR=/app/recordings" \
	-v "$RECORDINGS_HOST_DIR:/app/recordings" \
	"$FULL_IMAGE"

echo "App started on $PORT_BINDING"
echo "Recordings host directory: $RECORDINGS_HOST_DIR"
