ALTER TABLE training_recordings
ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ,
ADD COLUMN IF NOT EXISTS delete_after TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_training_recordings_active
ON training_recordings (training_id, recorded_at DESC)
WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_training_recordings_delete_after
ON training_recordings (delete_after)
WHERE delete_after IS NOT NULL;
