# Deployment Notes

## Docker image build and push

Use the helper script:

```bash
./build-and-push.sh
```

You can override the target image and tag:

```bash
DOCKER_IMAGE=bojankrlekrstic/fitness-platform-svc DOCKER_TAG=version1.0.3 ./build-and-push.sh
```

## Docker Hub access

If push fails with:

```text
denied: requested access to the resource is denied
```

that usually means one of these:

- Docker Hub login is missing
- the token does not have write access
- the repository name is wrong
- the repository does not exist under that account

The repo name used by the script and deployment commands must match the Docker Hub repository exactly.

## Server deploy

- The app runs in Docker.
- Recordings are stored either in the Docker volume `fitness_recordings` or in GCS.
- If you keep local storage, the server disk will accumulate recordings over time.
- If you use GCS, the server disk does not store the media files.

For manual server deployments, use the helper script instead of typing
`docker run` by hand:

```bash
DOCKER_IMAGE=bojankrlekrstic/fitness-platform-svc DOCKER_TAG=version1.1.5 ./deploy/run-app.sh
```

The script always mounts the host recordings directory into the container:

```text
./recordings -> /app/recordings
```

That keeps uploaded recordings outside the app container, so deleting and
recreating `fitness-platform-app-1` does not delete local recording files.

You can override the host folder if needed:

```bash
RECORDINGS_HOST_DIR=/home/krle/app/fitness-platform/recordings \
DOCKER_IMAGE=bojankrlekrstic/fitness-platform-svc \
DOCKER_TAG=version1.1.5 \
./deploy/run-app.sh
```

Do not start the app container without this mount when using local storage:

```text
-v "$RECORDINGS_HOST_DIR:/app/recordings"
```
