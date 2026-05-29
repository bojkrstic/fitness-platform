package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

type App struct {
	store             Store
	sessions          *SessionManager
	hub               *RoomHub
	seedAdminEmail    string
	seedAdminPassword string
}

func NewApp(store Store, seedAdminEmail, seedAdminPassword string) *App {
	return &App{
		store:             store,
		sessions:          NewSessionManager(),
		hub:               NewRoomHub(),
		seedAdminEmail:    seedAdminEmail,
		seedAdminPassword: seedAdminPassword,
	}
}

func (a *App) Close() error {
	return a.store.Close()
}

func (a *App) Routes() http.Handler {
	r := chi.NewRouter()

	r.Get("/", a.homeHandler)

	r.Route("/auth", func(r chi.Router) {
		r.Get("/login", a.loginFormHandler)
		r.Post("/login", a.loginHandler)
		r.Get("/register", a.registerFormHandler)
		r.Post("/register", a.registerHandler)
		r.Post("/logout", a.logoutHandler)
	})

	r.Route("/trainings", func(r chi.Router) {
		r.Get("/", a.trainingsHandler)
		r.With(a.requireAuth).Get("/{id}", a.trainingRoomHandler)
	})

	r.With(a.requireAuth, a.requireAdmin).Route("/admin", func(r chi.Router) {
		r.Get("/", a.adminDashboardHandler)
		r.Post("/trainings", a.createTrainingHandler)
	})

	r.With(a.requireAuth).Get("/ws/room/{id}", a.roomSocketHandler)

	return r
}

func (a *App) Bootstrap(ctx context.Context) error {
	if err := a.store.Init(ctx); err != nil {
		return err
	}

	if err := a.ensureSeedAdmin(ctx); err != nil {
		return err
	}

	if err := a.ensureDefaultTrainings(ctx); err != nil {
		return err
	}

	return nil
}

func (a *App) ensureSeedAdmin(ctx context.Context) error {
	_, err := a.store.FindAuthUserByEmail(ctx, a.seedAdminEmail)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(a.seedAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}

	_, err = a.store.CreateUser(ctx, a.seedAdminEmail, string(hash), "admin")
	if err != nil {
		return err
	}

	log.Printf("Seed admin created: %s / %s", a.seedAdminEmail, a.seedAdminPassword)
	return nil
}

func (a *App) ensureDefaultTrainings(ctx context.Context) error {
	trainings, err := a.store.ListTrainings(ctx)
	if err != nil {
		return err
	}
	if len(trainings) > 0 {
		return nil
	}

	admin, err := a.store.FindAuthUserByEmail(ctx, a.seedAdminEmail)
	if err != nil {
		return err
	}

	defaults := []TrainingForm{
		{Title: "Morning HIIT", Trainer: "Ana", Description: "Intenzivan trening za celo telo.", Time: "09:00"},
		{Title: "Yoga Flow", Trainer: "Marko", Description: "Lagani joga trening za istezanje.", Time: "18:00"},
	}

	for _, input := range defaults {
		if _, err := a.store.CreateTraining(ctx, input, admin.ID); err != nil {
			return err
		}
	}

	return nil
}

func (a *App) currentUser(r *http.Request) (*User, bool) {
	session, ok := a.sessions.Current(r)
	if !ok {
		return nil, false
	}

	user, err := a.store.FindUserByID(r.Context(), session.UserID)
	if err != nil {
		return nil, false
	}

	return &user, true
}

func (a *App) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.currentUser(r); !ok {
			target := "/auth/login?next=" + url.QueryEscape(r.URL.RequestURI())
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (a *App) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := a.currentUser(r)
		if !ok {
			target := "/auth/login?next=" + url.QueryEscape(r.URL.RequestURI())
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}
		if user.Role != "admin" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (a *App) homeHandler(w http.ResponseWriter, r *http.Request) {
	trainings, err := a.store.ListTrainings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.render(w, "index.html", HomePageData{
		BasePageData: BasePageData{CurrentUser: a.mustCurrentUser(r)},
		Trainings:    trainings,
	})
}

func (a *App) trainingsHandler(w http.ResponseWriter, r *http.Request) {
	trainings, err := a.store.ListTrainings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.render(w, "trainings.html", TrainingsPageData{
		BasePageData: BasePageData{CurrentUser: a.mustCurrentUser(r)},
		Trainings:    trainings,
	})
}

func (a *App) trainingRoomHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	training, err := a.store.FindTrainingByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.render(w, "training_room.html", TrainingRoomPageData{
		BasePageData: BasePageData{CurrentUser: a.mustCurrentUser(r)},
		Training:     training,
	})
}

func (a *App) loginFormHandler(w http.ResponseWriter, r *http.Request) {
	a.render(w, "login.html", AuthPageData{
		BasePageData: BasePageData{CurrentUser: a.mustCurrentUser(r)},
		Next:         safeNext(r.URL.Query().Get("next")),
	})
}

func (a *App) registerFormHandler(w http.ResponseWriter, r *http.Request) {
	a.render(w, "register.html", AuthPageData{
		BasePageData: BasePageData{CurrentUser: a.mustCurrentUser(r)},
		Next:         safeNext(r.URL.Query().Get("next")),
	})
}

func (a *App) loginHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	next := safeNext(r.FormValue("next"))

	user, err := a.store.FindAuthUserByEmail(r.Context(), email)
	if err != nil {
		a.render(w, "login.html", AuthPageData{
			BasePageData: BasePageData{Error: "Neispravan email ili lozinka."},
			Next:         next,
			Email:        email,
		})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		a.render(w, "login.html", AuthPageData{
			BasePageData: BasePageData{Error: "Neispravan email ili lozinka."},
			Next:         next,
			Email:        email,
		})
		return
	}

	a.sessions.Create(w, user.ID)
	if next == "" {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (a *App) registerHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	confirm := r.FormValue("confirm_password")
	next := safeNext(r.FormValue("next"))

	switch {
	case email == "":
		a.render(w, "register.html", AuthPageData{
			BasePageData: BasePageData{Error: "Email je obavezan."},
			Next:         next,
			Email:        email,
		})
		return
	case len(password) < 8:
		a.render(w, "register.html", AuthPageData{
			BasePageData: BasePageData{Error: "Lozinka mora imati najmanje 8 karaktera."},
			Next:         next,
			Email:        email,
		})
		return
	case password != confirm:
		a.render(w, "register.html", AuthPageData{
			BasePageData: BasePageData{Error: "Lozinke se ne poklapaju."},
			Next:         next,
			Email:        email,
		})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	user, err := a.store.CreateUser(r.Context(), email, string(hash), "member")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			a.render(w, "register.html", AuthPageData{
				BasePageData: BasePageData{Error: "Korisnik sa tim emailom već postoji."},
				Next:         next,
				Email:        email,
			})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.sessions.Create(w, user.ID)
	if next == "" {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (a *App) logoutHandler(w http.ResponseWriter, r *http.Request) {
	a.sessions.Destroy(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) adminDashboardHandler(w http.ResponseWriter, r *http.Request) {
	trainings, err := a.store.ListTrainings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.render(w, "admin.html", AdminPageData{
		BasePageData: BasePageData{
			CurrentUser: a.mustCurrentUser(r),
			Success:     map[string]string{"1": "Trening je kreiran."}[r.URL.Query().Get("created")],
		},
		Trainings: trainings,
	})
}

func (a *App) createTrainingHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	input := TrainingForm{
		Title:       strings.TrimSpace(r.FormValue("title")),
		Trainer:     strings.TrimSpace(r.FormValue("trainer")),
		Description: strings.TrimSpace(r.FormValue("description")),
		Time:        strings.TrimSpace(r.FormValue("time")),
	}

	if input.Title == "" || input.Trainer == "" || input.Description == "" || input.Time == "" {
		trainings, _ := a.store.ListTrainings(r.Context())
		a.render(w, "admin.html", AdminPageData{
			BasePageData: BasePageData{
				CurrentUser: a.mustCurrentUser(r),
				Error:       "Sva polja su obavezna.",
			},
			Trainings: trainings,
			Form:      input,
		})
		return
	}

	user, _ := a.currentUser(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if _, err := a.store.CreateTraining(r.Context(), input, user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin?created=1", http.StatusSeeOther)
}

func (a *App) mustCurrentUser(r *http.Request) *User {
	user, _ := a.currentUser(r)
	return user
}

func (a *App) render(w http.ResponseWriter, page string, data any) {
	tmpl := template.Must(template.ParseFiles(
		"templates/layout.html",
		"templates/"+page,
	))

	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func safeNext(next string) string {
	next = strings.TrimSpace(next)
	if next == "" {
		return ""
	}

	u, err := url.Parse(next)
	if err != nil {
		return ""
	}
	if u.IsAbs() || !strings.HasPrefix(next, "/") {
		return ""
	}

	return next
}

func openDatabase(ctx context.Context, dsn string) (*sql.DB, error) {
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

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}
