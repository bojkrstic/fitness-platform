package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

var ErrNotFound = errors.New("not found")

type Store interface {
	Init(ctx context.Context) error
	Close() error
	CreateUser(ctx context.Context, email, passwordHash, role string) (User, error)
	FindAuthUserByEmail(ctx context.Context, email string) (AuthUser, error)
	FindUserByID(ctx context.Context, id string) (User, error)
	ListTrainings(ctx context.Context) ([]Training, error)
	FindTrainingByID(ctx context.Context, id string) (Training, error)
	CreateTraining(ctx context.Context, input TrainingForm, createdBy string) (Training, error)
}

type PostgresStore struct {
	db *sql.DB
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

	entries, err := fs.ReadDir(migrationFiles, "migrations")
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

		content, err := migrationFiles.ReadFile("migrations/" + entry.Name())
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

func (s *PostgresStore) CreateUser(ctx context.Context, email, passwordHash, role string) (User, error) {
	id := newID()
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO users (id, email, password_hash, role) VALUES ($1, $2, $3, $4)`,
		id, email, passwordHash, role,
	)
	if err != nil {
		return User{}, fmt.Errorf("insert user: %w", err)
	}

	return User{ID: id, Email: email, Role: role}, nil
}

func (s *PostgresStore) FindAuthUserByEmail(ctx context.Context, email string) (AuthUser, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, email, password_hash, role FROM users WHERE email = $1`,
		email,
	)

	var u AuthUser
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AuthUser{}, ErrNotFound
		}
		return AuthUser{}, fmt.Errorf("find auth user: %w", err)
	}

	return u, nil
}

func (s *PostgresStore) FindUserByID(ctx context.Context, id string) (User, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, email, role FROM users WHERE id = $1`,
		id,
	)

	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.Role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, fmt.Errorf("find user: %w", err)
	}

	return u, nil
}

func (s *PostgresStore) ListTrainings(ctx context.Context) ([]Training, error) {
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

	var trainings []Training
	for rows.Next() {
		var t Training
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

func (s *PostgresStore) FindTrainingByID(ctx context.Context, id string) (Training, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, title, trainer, description, time, created_by
		 FROM trainings
		 WHERE id = $1`,
		id,
	)

	var t Training
	if err := row.Scan(&t.ID, &t.Title, &t.Trainer, &t.Description, &t.Time, &t.CreatedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Training{}, ErrNotFound
		}
		return Training{}, fmt.Errorf("find training: %w", err)
	}

	return t, nil
}

func (s *PostgresStore) CreateTraining(ctx context.Context, input TrainingForm, createdBy string) (Training, error) {
	id := newID()
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO trainings (id, title, trainer, description, time, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		id, input.Title, input.Trainer, input.Description, input.Time, createdBy,
	)
	if err != nil {
		return Training{}, fmt.Errorf("insert training: %w", err)
	}

	return Training{
		ID:          id,
		Title:       input.Title,
		Trainer:     input.Trainer,
		Description: input.Description,
		Time:        input.Time,
		CreatedBy:   createdBy,
	}, nil
}
