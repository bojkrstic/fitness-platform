#!/usr/bin/env bash
set -euo pipefail

IMAGE="${DOCKER_IMAGE:-bojankrlekrstic/fitness-platform-svc}"
TAG="${DOCKER_TAG:-version1.0.11}"
FULL_IMAGE="${IMAGE}:${TAG}"
NAMESPACE="${IMAGE%%/*}"

if [[ "$IMAGE" != */* ]]; then
	echo "Docker image must include a Docker Hub namespace, for example: bojankrlekrstic/fitness-platform-svc"
	exit 1
fi

DOCKER_USERNAME="$(docker info 2>/dev/null | sed -n 's/^ Username: //p' | head -n 1)"
if [[ -z "$DOCKER_USERNAME" || "$DOCKER_USERNAME" == "<no value>" ]]; then
	echo "Docker is not logged in."
	echo "Run: docker login -u $NAMESPACE"
	echo "Use a Docker Hub access token with Read & Write permissions."
	exit 1
fi

if [[ "$DOCKER_USERNAME" != "$NAMESPACE" ]]; then
	echo "Docker is logged in as '$DOCKER_USERNAME', but this image is under '$NAMESPACE'."
	echo "Run:"
	echo "  docker logout"
	echo "  docker login -u $NAMESPACE"
	echo "Then rerun this script."
	exit 1
fi

echo "Building Docker image: $FULL_IMAGE"
docker build -t "$FULL_IMAGE" .

echo "Pushing image to Docker Hub..."
if ! docker push "$FULL_IMAGE"; then
	echo
	echo "Push failed. Check that Docker Hub repository '$IMAGE' exists and that your token has Read & Write permissions."
	exit 1
fi

echo "Done! Image is pushed to Docker Hub."
