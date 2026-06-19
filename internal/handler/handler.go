package handler

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"

	"fitnes-platform/internal/config"
	"fitnes-platform/internal/model"
	"fitnes-platform/internal/repository"
	"fitnes-platform/internal/room"
	"fitnes-platform/internal/service"
	"fitnes-platform/internal/session"

	"github.com/gin-gonic/gin"
)

type HttpHandler struct {
	svc       *service.Service
	sessions  *session.Manager
	hub       *room.Hub
	templates map[string]*template.Template
	router    *gin.Engine
	cfg       config.Config
}

func NewHttpHandler(svc *service.Service, cfg config.Config) *HttpHandler {
	gin.SetMode(gin.ReleaseMode)

	h := &HttpHandler{
		svc:       svc,
		sessions:  session.NewManager(),
		hub:       room.NewHub(),
		templates: mustLoadTemplates(),
		cfg:       cfg,
	}

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	router.GET("/", h.homeHandler)

	auth := router.Group("/auth")
	{
		auth.GET("/login", h.loginFormHandler)
		auth.POST("/login", h.loginHandler)
		auth.GET("/register", h.registerFormHandler)
		auth.POST("/register", h.registerHandler)
		auth.POST("/logout", h.logoutHandler)
	}

	trainings := router.Group("/trainings")
	{
		trainings.GET("/", h.trainingsHandler)
		trainings.GET("/:id", h.requireAuth(), h.trainingRoomHandler)
	}

	admin := router.Group("/admin", h.requireAuth(), h.requireAdmin())
	{
		admin.GET("/", h.adminDashboardHandler)
		admin.POST("/trainings", h.createTrainingHandler)
	}

	router.GET("/ws/room/:id", h.requireAuth(), h.roomSocketHandler)

	h.router = router
	return h
}

func mustLoadTemplates() map[string]*template.Template {
	pages := map[string]*template.Template{}
	pageFiles := map[string]string{
		"index":         "web/templates/index.html",
		"trainings":     "web/templates/trainings.html",
		"login":         "web/templates/login.html",
		"register":      "web/templates/register.html",
		"admin":         "web/templates/admin.html",
		"training_room": "web/templates/training_room.html",
	}

	for name, pageFile := range pageFiles {
		funcs := template.FuncMap{
			"json": func(v any) template.JS {
				payload, err := json.Marshal(v)
				if err != nil {
					return "null"
				}
				return template.JS(payload)
			},
		}
		tpl := template.Must(template.New("layout.html").Funcs(funcs).ParseFiles("web/templates/layout.html", pageFile))
		pages[name] = tpl
	}

	return pages
}

func (h *HttpHandler) renderPage(c *gin.Context, status int, page string, data any) {
	tpl, ok := h.templates[page]
	if !ok {
		c.String(http.StatusInternalServerError, "template not found")
		return
	}

	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tpl.ExecuteTemplate(c.Writer, "layout", data); err != nil {
		c.Error(err)
	}
}

func (h *HttpHandler) Init(ctx context.Context) error {
	return h.svc.Init(ctx)
}

func (h *HttpHandler) Serve(port string) error {
	httpServer := &http.Server{
		Addr:    ":" + port,
		Handler: h.router,
	}

	log.Printf("Server running on http://localhost:%s", port)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

func (h *HttpHandler) Close() error {
	return h.svc.Close()
}

func (h *HttpHandler) currentUser(c *gin.Context) (*model.User, bool) {
	sess, ok := h.sessions.Current(c.Request)
	if !ok {
		return nil, false
	}

	user, err := h.svc.CurrentUser(c.Request.Context(), sess.UserID)
	if err != nil {
		return nil, false
	}

	return user, true
}

func (h *HttpHandler) mustCurrentUser(c *gin.Context) *model.User {
	user, _ := h.currentUser(c)
	return user
}

func (h *HttpHandler) requireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := h.currentUser(c); !ok {
			target := "/auth/login?next=" + url.QueryEscape(c.Request.URL.RequestURI())
			c.Redirect(http.StatusSeeOther, target)
			c.Abort()
			return
		}
		c.Next()
	}
}

func (h *HttpHandler) requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := h.currentUser(c)
		if !ok {
			target := "/auth/login?next=" + url.QueryEscape(c.Request.URL.RequestURI())
			c.Redirect(http.StatusSeeOther, target)
			c.Abort()
			return
		}
		if user.Role != "admin" {
			c.String(http.StatusForbidden, "forbidden")
			c.Abort()
			return
		}
		c.Next()
	}
}

func (h *HttpHandler) homeHandler(c *gin.Context) {
	trainings, err := h.svc.HomeTrainings(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	h.renderPage(c, http.StatusOK, "index", model.HomePageData{
		BasePageData: model.BasePageData{CurrentUser: h.mustCurrentUser(c)},
		Trainings:    trainings,
	})
}

func (h *HttpHandler) trainingsHandler(c *gin.Context) {
	trainings, err := h.svc.HomeTrainings(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	h.renderPage(c, http.StatusOK, "trainings", model.TrainingsPageData{
		BasePageData: model.BasePageData{CurrentUser: h.mustCurrentUser(c)},
		Trainings:    trainings,
	})
}

func (h *HttpHandler) trainingRoomHandler(c *gin.Context) {
	id := c.Param("id")
	training, err := h.svc.Training(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.Status(http.StatusNotFound)
			return
		}
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	h.renderPage(c, http.StatusOK, "training_room", model.TrainingRoomPageData{
		BasePageData: model.BasePageData{CurrentUser: h.mustCurrentUser(c)},
		Training:     training,
		ICEServers:   h.cfg.WebRTCICEServers,
	})
}

func (h *HttpHandler) loginFormHandler(c *gin.Context) {
	h.renderPage(c, http.StatusOK, "login", model.AuthPageData{
		BasePageData: model.BasePageData{CurrentUser: h.mustCurrentUser(c)},
		Next:         safeNext(c.Query("next")),
	})
}

func (h *HttpHandler) registerFormHandler(c *gin.Context) {
	h.renderPage(c, http.StatusOK, "register", model.AuthPageData{
		BasePageData: model.BasePageData{CurrentUser: h.mustCurrentUser(c)},
		Next:         safeNext(c.Query("next")),
	})
}

func (h *HttpHandler) loginHandler(c *gin.Context) {
	var input struct {
		Email    string `form:"email"`
		Password string `form:"password"`
		Next     string `form:"next"`
	}
	if err := c.ShouldBind(&input); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	email := strings.TrimSpace(input.Email)
	next := safeNext(input.Next)

	user, err := h.svc.Login(c.Request.Context(), email, input.Password)
	if err != nil {
		h.renderPage(c, http.StatusOK, "login", model.AuthPageData{
			BasePageData: model.BasePageData{Error: "Neispravan email ili lozinka."},
			Next:         next,
			Email:        email,
		})
		return
	}

	h.sessions.Create(c.Writer, user.ID)
	if next == "" {
		next = "/"
	}
	c.Redirect(http.StatusSeeOther, next)
}

func (h *HttpHandler) registerHandler(c *gin.Context) {
	var input struct {
		Email    string `form:"email"`
		Password string `form:"password"`
		Confirm  string `form:"confirm_password"`
		Next     string `form:"next"`
	}
	if err := c.ShouldBind(&input); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	email := strings.TrimSpace(input.Email)
	next := safeNext(input.Next)

	user, err := h.svc.Register(c.Request.Context(), email, input.Password, input.Confirm)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrEmailRequired):
			h.renderPage(c, http.StatusOK, "register", model.AuthPageData{
				BasePageData: model.BasePageData{Error: "Email je obavezan."},
				Next:         next,
				Email:        email,
			})
			return
		case errors.Is(err, service.ErrPasswordTooShort):
			h.renderPage(c, http.StatusOK, "register", model.AuthPageData{
				BasePageData: model.BasePageData{Error: "Lozinka mora imati najmanje 8 karaktera."},
				Next:         next,
				Email:        email,
			})
			return
		case errors.Is(err, service.ErrPasswordMismatch):
			h.renderPage(c, http.StatusOK, "register", model.AuthPageData{
				BasePageData: model.BasePageData{Error: "Lozinke se ne poklapaju."},
				Next:         next,
				Email:        email,
			})
			return
		case errors.Is(err, service.ErrDuplicateEmail):
			h.renderPage(c, http.StatusOK, "register", model.AuthPageData{
				BasePageData: model.BasePageData{Error: "Korisnik sa tim emailom već postoji."},
				Next:         next,
				Email:        email,
			})
			return
		default:
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
	}

	h.sessions.Create(c.Writer, user.ID)
	if next == "" {
		next = "/"
	}
	c.Redirect(http.StatusSeeOther, next)
}

func (h *HttpHandler) logoutHandler(c *gin.Context) {
	h.sessions.Destroy(c.Writer, c.Request)
	c.Redirect(http.StatusSeeOther, "/")
}

func (h *HttpHandler) adminDashboardHandler(c *gin.Context) {
	trainings, err := h.svc.HomeTrainings(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	h.renderPage(c, http.StatusOK, "admin", model.AdminPageData{
		BasePageData: model.BasePageData{
			CurrentUser: h.mustCurrentUser(c),
			Success:     map[string]string{"1": "Trening je kreiran."}[c.Query("created")],
		},
		Trainings: trainings,
	})
}

func (h *HttpHandler) createTrainingHandler(c *gin.Context) {
	var input model.TrainingForm
	if err := c.ShouldBind(&input); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	user, _ := h.currentUser(c)
	if user == nil {
		c.String(http.StatusUnauthorized, "unauthorized")
		return
	}

	if _, err := h.svc.CreateTraining(c.Request.Context(), input, user.ID); err != nil {
		if errors.Is(err, service.ErrMissingField) {
			trainings, _ := h.svc.HomeTrainings(c.Request.Context())
			h.renderPage(c, http.StatusOK, "admin", model.AdminPageData{
				BasePageData: model.BasePageData{
					CurrentUser: h.mustCurrentUser(c),
					Error:       "Sva polja su obavezna.",
				},
				Trainings: trainings,
				Form:      input,
			})
			return
		}
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	c.Redirect(http.StatusSeeOther, "/admin?created=1")
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

func (h *HttpHandler) roomSocketHandler(c *gin.Context) {
	user := h.mustCurrentUser(c)
	if user == nil {
		c.String(http.StatusUnauthorized, "unauthorized")
		return
	}

	roomID := c.Param("id")
	if _, err := h.svc.Training(c.Request.Context(), roomID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.Status(http.StatusNotFound)
			return
		}
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	conn, err := room.Upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	client := room.NewClient(roomID, user, conn, h.hub)

	participants, history, err := h.hub.Join(roomID, client)
	if err != nil {
		_ = conn.WriteJSON(room.Message{Type: "room-full"})
		_ = conn.Close()
		return
	}

	client.Send() <- room.MustJSON(room.Message{
		Type:         "welcome",
		ClientID:     client.ID(),
		Participants: participants,
		History:      history,
	})
	log.Printf("room=%s welcome user=%s client=%s history=%d participants=%d", roomID, user.Email, client.ID(), len(history), len(participants))
	h.hub.BroadcastParticipants(roomID)

	go client.WritePump()
	client.ReadPump()
}
