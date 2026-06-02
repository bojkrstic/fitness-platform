package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"fitnes-platform/internal/config"
	"fitnes-platform/internal/model"
	"fitnes-platform/internal/repository"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrPasswordMismatch   = errors.New("password mismatch")
	ErrPasswordTooShort   = errors.New("password too short")
	ErrEmailRequired      = errors.New("email required")
	ErrDuplicateEmail     = errors.New("duplicate email")
	ErrMissingField       = errors.New("missing field")
)

type Service struct {
	store             repository.Store
	seedAdminEmail    string
	seedAdminPassword string
}

func New(store repository.Store, cfg config.Config) *Service {
	return &Service{
		store:             store,
		seedAdminEmail:    cfg.SeedAdminEmail,
		seedAdminPassword: cfg.SeedAdminPassword,
	}
}

func (s *Service) Init(ctx context.Context) error {
	if err := s.store.Init(ctx); err != nil {
		return err
	}

	if err := s.ensureSeedAdmin(ctx); err != nil {
		return err
	}

	if err := s.ensureDefaultTrainings(ctx); err != nil {
		return err
	}

	return nil
}

func (s *Service) Close() error {
	return s.store.Close()
}

func (s *Service) CurrentUser(ctx context.Context, userID string) (*model.User, error) {
	user, err := s.store.FindUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *Service) HomeTrainings(ctx context.Context) ([]model.Training, error) {
	return s.store.ListTrainings(ctx)
}

func (s *Service) Training(ctx context.Context, id string) (model.Training, error) {
	return s.store.FindTrainingByID(ctx, id)
}

func (s *Service) Login(ctx context.Context, email, password string) (model.User, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return model.User{}, ErrEmailRequired
	}

	user, err := s.store.FindAuthUserByEmail(ctx, email)
	if err != nil {
		return model.User{}, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return model.User{}, ErrInvalidCredentials
	}

	return user.User, nil
}

func (s *Service) Register(ctx context.Context, email, password, confirm string) (model.User, error) {
	email = strings.TrimSpace(email)
	switch {
	case email == "":
		return model.User{}, ErrEmailRequired
	case len(password) < 8:
		return model.User{}, ErrPasswordTooShort
	case password != confirm:
		return model.User{}, ErrPasswordMismatch
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return model.User{}, fmt.Errorf("hash password: %w", err)
	}

	user, err := s.store.CreateUser(ctx, email, string(hash), "member")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return model.User{}, ErrDuplicateEmail
		}
		return model.User{}, err
	}

	return user, nil
}

func (s *Service) CreateTraining(ctx context.Context, input model.TrainingForm, createdBy string) (model.Training, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Trainer = strings.TrimSpace(input.Trainer)
	input.Description = strings.TrimSpace(input.Description)
	input.Time = strings.TrimSpace(input.Time)

	if input.Title == "" || input.Trainer == "" || input.Description == "" || input.Time == "" {
		return model.Training{}, ErrMissingField
	}

	return s.store.CreateTraining(ctx, input, createdBy)
}

func (s *Service) ensureSeedAdmin(ctx context.Context) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(s.seedAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}

	_, err = s.store.UpsertUser(ctx, s.seedAdminEmail, string(hash), "admin")
	if err != nil {
		return err
	}

	log.Printf("Seed admin created: %s / %s", s.seedAdminEmail, s.seedAdminPassword)
	return nil
}

func (s *Service) ensureDefaultTrainings(ctx context.Context) error {
	trainings, err := s.store.ListTrainings(ctx)
	if err != nil {
		return err
	}
	if len(trainings) > 0 {
		return nil
	}

	admin, err := s.store.FindAuthUserByEmail(ctx, s.seedAdminEmail)
	if err != nil {
		return err
	}

	defaults := []model.TrainingForm{
		{Title: "Morning HIIT", Trainer: "Ana", Description: "Intenzivan trening za celo telo.", Time: "09:00"},
		{Title: "Yoga Flow", Trainer: "Marko", Description: "Lagani joga trening za istezanje.", Time: "18:00"},
	}

	for _, input := range defaults {
		if _, err := s.store.CreateTraining(ctx, input, admin.ID); err != nil {
			return err
		}
	}

	return nil
}
