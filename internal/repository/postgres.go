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

func newID() string {
	var b [16]byte
	if _, err := crand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("generate id: %v", err))
	}

	return hex.EncodeToString(b[:])
}
