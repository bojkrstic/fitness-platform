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
