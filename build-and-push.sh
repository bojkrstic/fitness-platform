#!/usr/bin/env bash
set -e

# IMAGE="bojankrlekrstic/fitness-platform:latest"
IMAGE="bojankrlekrstic/fitness-platform-svc:version1.0.0"

echo "Building Docker image: $IMAGE"
docker build -t "$IMAGE" .

echo "Pushing image to Docker Hub..."
docker push "$IMAGE"

echo "Done! Image is pushed to Docker Hub."
