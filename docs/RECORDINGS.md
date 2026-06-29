# Recordings

## What the app does

- Admin can record the live room camera/video stream from the training page.
- Recordings are listed on the same training page with `View` and `Download`.
- Metadata is stored in PostgreSQL.
- The video file is stored either locally or in Google Cloud Storage.
- Member access to recordings follows the same subscription gate as the live room.

## Storage modes

### Local storage

- Used when GCS env vars are not set.
- Files are written under `RECORDINGS_DIR`, defaulting to `recordings/`.
- In Docker Compose, that directory is mounted as the `fitness_recordings` volume.
- Good for local development and server testing.

### Google Cloud Storage

- Used when these env vars are set:
  - `GCS_RECORDINGS_BUCKET`
  - `GCS_SERVICE_ACCOUNT_EMAIL`
  - `GCS_PRIVATE_KEY`
- The app signs upload and access URLs.
- The browser sends upload chunks directly to GCS.

## File layout

Recordings are organized by date and training:

```text
recordings/YYYY/MM/DD/{training_id}/{recording_id}.webm
```

## Browser upload

- Recording uses `MediaRecorder`.
- The browser sends chunks during recording, not one huge file at the end.
- This keeps RAM usage low.
- Chunks are uploaded in smaller aligned blocks.

## UI behavior

- `Start recording` opens an upload session.
- `Stop & upload` finalizes the file and saves metadata.
- `View` opens the file in the browser.
- `Download` forces a download.

## CORS for GCS

If you use GCS, the bucket needs CORS for direct browser uploads:

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

Apply it with:

```bash
gcloud storage buckets update gs://fitness-recordings-bucket --cors-file=cors.json
```

## Cleanup

- There is no automatic retention yet.
- Local storage will grow on the server until files are removed manually or retention is added.
- GCS storage will also grow until lifecycle rules are added.
