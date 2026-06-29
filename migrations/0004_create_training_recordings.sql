CREATE TABLE IF NOT EXISTS training_recordings (
	id TEXT PRIMARY KEY,
	training_id TEXT NOT NULL REFERENCES trainings(id) ON DELETE CASCADE,
	object_name TEXT NOT NULL,
	original_filename TEXT NOT NULL,
	content_type TEXT NOT NULL,
	size_bytes BIGINT NOT NULL DEFAULT 0,
	duration_seconds INTEGER NOT NULL DEFAULT 0,
	recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	created_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_training_recordings_training_recorded
ON training_recordings (training_id, recorded_at DESC);
