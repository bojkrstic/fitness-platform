package repository

import (
	"context"
	crand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"fitnes-platform/internal/model"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var ErrNotFound = errors.New("not found")

type Store interface {
	Init(ctx context.Context) error
	Close() error
	CreateUser(ctx context.Context, email, passwordHash, role string) (model.User, error)
	UpsertUser(ctx context.Context, email, passwordHash, role string) (model.User, error)
	FindAuthUserByEmail(ctx context.Context, email string) (model.AuthUser, error)
	FindUserByID(ctx context.Context, id string) (model.User, error)
	ListTrainings(ctx context.Context) ([]model.Training, error)
	FindTrainingByID(ctx context.Context, id string) (model.Training, error)
	CreateTraining(ctx context.Context, input model.TrainingForm, createdBy string) (model.Training, error)
	ListRecordings(ctx context.Context, trainingID string) ([]model.Recording, error)
	FindRecordingByID(ctx context.Context, id string) (model.Recording, error)
	CreateRecording(ctx context.Context, recording model.Recording) (model.Recording, error)
	UpdateRecordingName(ctx context.Context, id, originalFilename string) (model.Recording, error)
	SoftDeleteRecording(ctx context.Context, id string) (model.Recording, error)
	ListExpiredDeletedRecordings(ctx context.Context) ([]model.Recording, error)
	HardDeleteRecording(ctx context.Context, id string) error
	UpsertBillingCustomer(ctx context.Context, userID, stripeCustomerID string) error
	FindBillingCustomerByUserID(ctx context.Context, userID string) (model.BillingCustomer, error)
	FindUserIDByBillingCustomerID(ctx context.Context, stripeCustomerID string) (string, error)
	UpsertSubscription(ctx context.Context, sub model.Subscription) (model.Subscription, error)
	FindSubscriptionByUserID(ctx context.Context, userID string) (model.Subscription, error)
	FindUserByStripeSubscriptionID(ctx context.Context, stripeSubscriptionID string) (string, error)
	MarkStripeWebhookEvent(ctx context.Context, eventID, eventType string) (bool, error)
}

type PostgresStore struct {
	db *sql.DB
}

func OpenDatabase(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

func (s *PostgresStore) Init(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}

	entries, err := os.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}

		var applied bool
		if err := s.db.QueryRowContext(
			ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name = $1)`,
			entry.Name(),
		).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", entry.Name(), err)
		}
		if applied {
			continue
		}

		content, err := os.ReadFile(filepath.Join("migrations", entry.Name()))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", entry.Name(), err)
		}

		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}

		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO schema_migrations (name) VALUES ($1)`,
			entry.Name(),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", entry.Name(), err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", entry.Name(), err)
		}
	}

	return nil
}

func (s *PostgresStore) CreateUser(ctx context.Context, email, passwordHash, role string) (model.User, error) {
	id := newID()
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO users (id, email, password_hash, role) VALUES ($1, $2, $3, $4)`,
		id, email, passwordHash, role,
	)
	if err != nil {
		return model.User{}, fmt.Errorf("insert user: %w", err)
	}

	return model.User{ID: id, Email: email, Role: role}, nil
}

func (s *PostgresStore) UpsertUser(ctx context.Context, email, passwordHash, role string) (model.User, error) {
	row := s.db.QueryRowContext(
		ctx,
		`INSERT INTO users (id, email, password_hash, role)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (email) DO UPDATE
		 SET password_hash = EXCLUDED.password_hash,
		     role = EXCLUDED.role
		 RETURNING id, email, role`,
		newID(), email, passwordHash, role,
	)

	var u model.User
	if err := row.Scan(&u.ID, &u.Email, &u.Role); err != nil {
		return model.User{}, fmt.Errorf("upsert user: %w", err)
	}

	return u, nil
}

func (s *PostgresStore) FindAuthUserByEmail(ctx context.Context, email string) (model.AuthUser, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, email, password_hash, role FROM users WHERE email = $1`,
		email,
	)

	var u model.AuthUser
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.AuthUser{}, ErrNotFound
		}
		return model.AuthUser{}, fmt.Errorf("find auth user: %w", err)
	}

	return u, nil
}

func (s *PostgresStore) FindUserByID(ctx context.Context, id string) (model.User, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, email, role FROM users WHERE id = $1`,
		id,
	)

	var u model.User
	if err := row.Scan(&u.ID, &u.Email, &u.Role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.User{}, ErrNotFound
		}
		return model.User{}, fmt.Errorf("find user: %w", err)
	}

	return u, nil
}

func (s *PostgresStore) ListTrainings(ctx context.Context) ([]model.Training, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, title, trainer, description, time, created_by
		 FROM trainings
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list trainings: %w", err)
	}
	defer rows.Close()

	var trainings []model.Training
	for rows.Next() {
		var t model.Training
		if err := rows.Scan(&t.ID, &t.Title, &t.Trainer, &t.Description, &t.Time, &t.CreatedBy); err != nil {
			return nil, fmt.Errorf("scan training: %w", err)
		}
		trainings = append(trainings, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trainings: %w", err)
	}

	return trainings, nil
}

func (s *PostgresStore) FindTrainingByID(ctx context.Context, id string) (model.Training, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, title, trainer, description, time, created_by
		 FROM trainings
		 WHERE id = $1`,
		id,
	)

	var t model.Training
	if err := row.Scan(&t.ID, &t.Title, &t.Trainer, &t.Description, &t.Time, &t.CreatedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Training{}, ErrNotFound
		}
		return model.Training{}, fmt.Errorf("find training: %w", err)
	}

	return t, nil
}

func (s *PostgresStore) CreateTraining(ctx context.Context, input model.TrainingForm, createdBy string) (model.Training, error) {
	id := newID()
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO trainings (id, title, trainer, description, time, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		id, input.Title, input.Trainer, input.Description, input.Time, createdBy,
	)
	if err != nil {
		return model.Training{}, fmt.Errorf("insert training: %w", err)
	}

	return model.Training{
		ID:          id,
		Title:       input.Title,
		Trainer:     input.Trainer,
		Description: input.Description,
		Time:        input.Time,
		CreatedBy:   createdBy,
	}, nil
}

func (s *PostgresStore) ListRecordings(ctx context.Context, trainingID string) ([]model.Recording, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, training_id, object_name, original_filename, content_type, size_bytes, duration_seconds,
		        to_char(recorded_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'), created_by,
		        COALESCE(to_char(deleted_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'), ''),
		        COALESCE(to_char(delete_after AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'), '')
		 FROM training_recordings
		 WHERE training_id = $1
		   AND deleted_at IS NULL
		 ORDER BY recorded_at DESC`,
		trainingID,
	)
	if err != nil {
		return nil, fmt.Errorf("list recordings: %w", err)
	}
	defer rows.Close()

	var recordings []model.Recording
	for rows.Next() {
		var r model.Recording
		if err := rows.Scan(
			&r.ID,
			&r.TrainingID,
			&r.ObjectName,
			&r.OriginalFilename,
			&r.ContentType,
			&r.SizeBytes,
			&r.DurationSeconds,
			&r.RecordedAt,
			&r.CreatedBy,
			&r.DeletedAt,
			&r.DeleteAfter,
		); err != nil {
			return nil, fmt.Errorf("scan recording: %w", err)
		}
		recordings = append(recordings, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recordings: %w", err)
	}

	return recordings, nil
}

func (s *PostgresStore) FindRecordingByID(ctx context.Context, id string) (model.Recording, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, training_id, object_name, original_filename, content_type, size_bytes, duration_seconds,
		        to_char(recorded_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'), created_by,
		        COALESCE(to_char(deleted_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'), ''),
		        COALESCE(to_char(delete_after AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'), '')
		 FROM training_recordings
		 WHERE id = $1`,
		id,
	)

	var r model.Recording
	if err := row.Scan(
		&r.ID,
		&r.TrainingID,
		&r.ObjectName,
		&r.OriginalFilename,
		&r.ContentType,
		&r.SizeBytes,
		&r.DurationSeconds,
		&r.RecordedAt,
		&r.CreatedBy,
		&r.DeletedAt,
		&r.DeleteAfter,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Recording{}, ErrNotFound
		}
		return model.Recording{}, fmt.Errorf("find recording: %w", err)
	}

	return r, nil
}

func (s *PostgresStore) CreateRecording(ctx context.Context, recording model.Recording) (model.Recording, error) {
	if recording.ID == "" {
		recording.ID = newID()
	}

	row := s.db.QueryRowContext(
		ctx,
		`INSERT INTO training_recordings
		 (id, training_id, object_name, original_filename, content_type, size_bytes, duration_seconds, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING to_char(recorded_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"')`,
		recording.ID,
		recording.TrainingID,
		recording.ObjectName,
		recording.OriginalFilename,
		recording.ContentType,
		recording.SizeBytes,
		recording.DurationSeconds,
		recording.CreatedBy,
	)
	if err := row.Scan(&recording.RecordedAt); err != nil {
		return model.Recording{}, fmt.Errorf("insert recording: %w", err)
	}

	return recording, nil
}

func (s *PostgresStore) UpdateRecordingName(ctx context.Context, id, originalFilename string) (model.Recording, error) {
	row := s.db.QueryRowContext(
		ctx,
		`UPDATE training_recordings
		 SET original_filename = $2
		 WHERE id = $1
		   AND deleted_at IS NULL
		 RETURNING id, training_id, object_name, original_filename, content_type, size_bytes, duration_seconds,
		           to_char(recorded_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'), created_by,
		           COALESCE(to_char(deleted_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'), ''),
		           COALESCE(to_char(delete_after AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'), '')`,
		id, originalFilename,
	)

	var r model.Recording
	if err := row.Scan(
		&r.ID,
		&r.TrainingID,
		&r.ObjectName,
		&r.OriginalFilename,
		&r.ContentType,
		&r.SizeBytes,
		&r.DurationSeconds,
		&r.RecordedAt,
		&r.CreatedBy,
		&r.DeletedAt,
		&r.DeleteAfter,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Recording{}, ErrNotFound
		}
		return model.Recording{}, fmt.Errorf("update recording name: %w", err)
	}

	return r, nil
}

func (s *PostgresStore) SoftDeleteRecording(ctx context.Context, id string) (model.Recording, error) {
	row := s.db.QueryRowContext(
		ctx,
		`UPDATE training_recordings
		 SET deleted_at = NOW(),
		     delete_after = NOW() + INTERVAL '2 days'
		 WHERE id = $1
		   AND deleted_at IS NULL
		 RETURNING id, training_id, object_name, original_filename, content_type, size_bytes, duration_seconds,
		           to_char(recorded_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'), created_by,
		           COALESCE(to_char(deleted_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'), ''),
		           COALESCE(to_char(delete_after AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'), '')`,
		id,
	)

	var r model.Recording
	if err := row.Scan(
		&r.ID,
		&r.TrainingID,
		&r.ObjectName,
		&r.OriginalFilename,
		&r.ContentType,
		&r.SizeBytes,
		&r.DurationSeconds,
		&r.RecordedAt,
		&r.CreatedBy,
		&r.DeletedAt,
		&r.DeleteAfter,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Recording{}, ErrNotFound
		}
		return model.Recording{}, fmt.Errorf("soft delete recording: %w", err)
	}

	return r, nil
}

func (s *PostgresStore) ListExpiredDeletedRecordings(ctx context.Context) ([]model.Recording, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, training_id, object_name, original_filename, content_type, size_bytes, duration_seconds,
		        to_char(recorded_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'), created_by,
		        COALESCE(to_char(deleted_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'), ''),
		        COALESCE(to_char(delete_after AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'), '')
		 FROM training_recordings
		 WHERE delete_after IS NOT NULL
		   AND delete_after <= NOW()
		 ORDER BY delete_after ASC
		 LIMIT 100`,
	)
	if err != nil {
		return nil, fmt.Errorf("list expired deleted recordings: %w", err)
	}
	defer rows.Close()

	var recordings []model.Recording
	for rows.Next() {
		var r model.Recording
		if err := rows.Scan(
			&r.ID,
			&r.TrainingID,
			&r.ObjectName,
			&r.OriginalFilename,
			&r.ContentType,
			&r.SizeBytes,
			&r.DurationSeconds,
			&r.RecordedAt,
			&r.CreatedBy,
			&r.DeletedAt,
			&r.DeleteAfter,
		); err != nil {
			return nil, fmt.Errorf("scan expired deleted recording: %w", err)
		}
		recordings = append(recordings, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired deleted recordings: %w", err)
	}

	return recordings, nil
}

func (s *PostgresStore) HardDeleteRecording(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM training_recordings WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("hard delete recording: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("hard delete recording rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) UpsertBillingCustomer(ctx context.Context, userID, stripeCustomerID string) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO billing_customers (user_id, stripe_customer_id)
		 VALUES ($1, $2)
		 ON CONFLICT (user_id) DO UPDATE
		 SET stripe_customer_id = EXCLUDED.stripe_customer_id,
		     updated_at = NOW()`,
		userID, stripeCustomerID,
	)
	if err != nil {
		return fmt.Errorf("upsert billing customer: %w", err)
	}
	return nil
}

func (s *PostgresStore) FindBillingCustomerByUserID(ctx context.Context, userID string) (model.BillingCustomer, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT user_id, stripe_customer_id,
		        to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'),
		        to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"')
		 FROM billing_customers
		 WHERE user_id = $1`,
		userID,
	)

	var customer model.BillingCustomer
	if err := row.Scan(&customer.UserID, &customer.StripeCustomerID, &customer.CreatedAt, &customer.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.BillingCustomer{}, ErrNotFound
		}
		return model.BillingCustomer{}, fmt.Errorf("find billing customer: %w", err)
	}

	return customer, nil
}

func (s *PostgresStore) FindUserIDByBillingCustomerID(ctx context.Context, stripeCustomerID string) (string, error) {
	var userID string
	err := s.db.QueryRowContext(
		ctx,
		`SELECT user_id FROM billing_customers WHERE stripe_customer_id = $1`,
		stripeCustomerID,
	).Scan(&userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("find user by billing customer: %w", err)
	}

	return userID, nil
}

func (s *PostgresStore) UpsertSubscription(ctx context.Context, sub model.Subscription) (model.Subscription, error) {
	if sub.ID == "" {
		sub.ID = newID()
	}

	row := s.db.QueryRowContext(
		ctx,
		`INSERT INTO user_subscriptions
		 (id, user_id, stripe_customer_id, stripe_subscription_id, stripe_price_id, status,
		  current_period_start, current_period_end, cancel_at_period_end)
		 VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::timestamptz, NULLIF($8, '')::timestamptz, $9)
		 ON CONFLICT (user_id) DO UPDATE
		 SET stripe_customer_id = EXCLUDED.stripe_customer_id,
		     stripe_subscription_id = EXCLUDED.stripe_subscription_id,
		     stripe_price_id = EXCLUDED.stripe_price_id,
		     status = EXCLUDED.status,
		     current_period_start = EXCLUDED.current_period_start,
		     current_period_end = EXCLUDED.current_period_end,
		     cancel_at_period_end = EXCLUDED.cancel_at_period_end,
		     updated_at = NOW()
		 RETURNING id, user_id, stripe_customer_id, stripe_subscription_id, stripe_price_id, status,
		           to_char(current_period_start AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'),
		           to_char(current_period_end AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'),
		           cancel_at_period_end,
		           to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'),
		           to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"')`,
		sub.ID,
		sub.UserID,
		sub.StripeCustomerID,
		sub.StripeSubscriptionID,
		sub.StripePriceID,
		sub.Status,
		sub.CurrentPeriodStart,
		sub.CurrentPeriodEnd,
		sub.CancelAtPeriodEnd,
	)
	if err := row.Scan(
		&sub.ID,
		&sub.UserID,
		&sub.StripeCustomerID,
		&sub.StripeSubscriptionID,
		&sub.StripePriceID,
		&sub.Status,
		&sub.CurrentPeriodStart,
		&sub.CurrentPeriodEnd,
		&sub.CancelAtPeriodEnd,
		&sub.CreatedAt,
		&sub.UpdatedAt,
	); err != nil {
		return model.Subscription{}, fmt.Errorf("upsert subscription: %w", err)
	}

	return sub, nil
}

func (s *PostgresStore) FindSubscriptionByUserID(ctx context.Context, userID string) (model.Subscription, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, user_id, stripe_customer_id, stripe_subscription_id, stripe_price_id, status,
		        to_char(current_period_start AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'),
		        to_char(current_period_end AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'),
		        cancel_at_period_end,
		        to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"'),
		        to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS "UTC"')
		 FROM user_subscriptions
		 WHERE user_id = $1`,
		userID,
	)

	var sub model.Subscription
	if err := row.Scan(
		&sub.ID,
		&sub.UserID,
		&sub.StripeCustomerID,
		&sub.StripeSubscriptionID,
		&sub.StripePriceID,
		&sub.Status,
		&sub.CurrentPeriodStart,
		&sub.CurrentPeriodEnd,
		&sub.CancelAtPeriodEnd,
		&sub.CreatedAt,
		&sub.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Subscription{}, ErrNotFound
		}
		return model.Subscription{}, fmt.Errorf("find subscription: %w", err)
	}

	return sub, nil
}

func (s *PostgresStore) FindUserByStripeSubscriptionID(ctx context.Context, stripeSubscriptionID string) (string, error) {
	var userID string
	err := s.db.QueryRowContext(
		ctx,
		`SELECT user_id FROM user_subscriptions WHERE stripe_subscription_id = $1`,
		stripeSubscriptionID,
	).Scan(&userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("find user by subscription id: %w", err)
	}

	return userID, nil
}

func (s *PostgresStore) MarkStripeWebhookEvent(ctx context.Context, eventID, eventType string) (bool, error) {
	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO stripe_webhook_events (event_id, event_type)
		 VALUES ($1, $2)
		 ON CONFLICT (event_id) DO NOTHING`,
		eventID, eventType,
	)
	if err != nil {
		return false, fmt.Errorf("mark webhook event: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("mark webhook event rows affected: %w", err)
	}

	return affected > 0, nil
}

func newID() string {
	var b [16]byte
	if _, err := crand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("generate id: %v", err))
	}

	return hex.EncodeToString(b[:])
}
