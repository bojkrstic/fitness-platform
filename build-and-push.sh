#!/usr/bin/env bash
set -e

IMAGE="${DOCKER_IMAGE:-bojankrlekrstic/fitness-platform-svc}"
TAG="${DOCKER_TAG:-version1.0.3}"
FULL_IMAGE="${IMAGE}:${TAG}"

echo "Building Docker image: $FULL_IMAGE"
docker build -t "$FULL_IMAGE" .

echo "Pushing image to Docker Hub..."
docker push "$FULL_IMAGE"

echo "Done! Image is pushed to Docker Hub."
