package handler

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"io"
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
		trainings.POST("/:id/recordings/sign-upload", h.requireAuth(), h.requireAdmin(), h.signRecordingUploadHandler)
		trainings.PUT("/:id/recordings/uploads/:uploadID", h.requireAuth(), h.requireAdmin(), h.uploadRecordingChunkHandler)
		trainings.POST("/:id/recordings/complete", h.requireAuth(), h.requireAdmin(), h.completeRecordingHandler)
		trainings.GET("/:id/recordings/:recordingID/view", h.requireAuth(), h.recordingAccessHandler(false))
		trainings.GET("/:id/recordings/:recordingID/download", h.requireAuth(), h.recordingAccessHandler(true))
	}

	billing := router.Group("/billing", h.requireAuth())
	{
		billing.GET("/", h.billingPageHandler)
		billing.POST("/checkout", h.billingCheckoutHandler)
		billing.POST("/portal", h.billingPortalHandler)
		billing.GET("/complete", h.billingCompleteHandler)
	}

	router.POST("/webhooks/stripe", h.stripeWebhookHandler)

	admin := router.Group("/admin", h.requireAuth(), h.requireAdmin())
	{
		admin.GET("/", h.adminDashboardHandler)
		admin.GET("/live-stats", h.adminLiveStatsHandler)
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
		"billing":       "web/templates/billing.html",
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
			if c.Request.Method != http.MethodGet {
				c.String(http.StatusUnauthorized, "unauthorized")
				c.Abort()
				return
			}
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
			if c.Request.Method != http.MethodGet {
				c.String(http.StatusUnauthorized, "unauthorized")
				c.Abort()
				return
			}
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

	canAccess, _, err := h.userAccess(c)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	h.renderPage(c, http.StatusOK, "index", model.HomePageData{
		BasePageData: model.BasePageData{CurrentUser: h.mustCurrentUser(c)},
		Trainings:    trainings,
		CanAccess:    canAccess,
	})
}

func (h *HttpHandler) trainingsHandler(c *gin.Context) {
	trainings, err := h.svc.HomeTrainings(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	canAccess, _, err := h.userAccess(c)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	h.renderPage(c, http.StatusOK, "trainings", model.TrainingsPageData{
		BasePageData: model.BasePageData{CurrentUser: h.mustCurrentUser(c)},
		Trainings:    trainings,
		CanAccess:    canAccess,
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

	canAccess, subscription, err := h.userAccess(c)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	data := model.TrainingRoomPageData{
		BasePageData:          model.BasePageData{CurrentUser: h.mustCurrentUser(c)},
		Training:              training,
		ICEServers:            h.cfg.WebRTCICEServers,
		RecordingEnabled:      h.svc.RecordingStorageEnabled(),
		RecordingDisabled:     h.svc.RecordingStorageDisabledReason(),
		RecordingStorageMode:  h.svc.RecordingStorageModeLabel(),
		CanAccess:             canAccess,
		BillingEnabled:        h.svc.BillingEnabled(),
		BillingDisabledReason: h.svc.BillingDisabledReason(),
		Subscription:          subscription,
		ReturnTo:              "/trainings/" + id,
	}

	if canAccess {
		recordings, err := h.svc.Recordings(c.Request.Context(), id)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		data.Recordings = recordings
	}

	if msg := strings.TrimSpace(c.Query("billing")); msg != "" {
		switch msg {
		case "success":
			data.SuccessMessage = "Pretplata je aktivirana."
		case "canceled":
			data.BillingDisabledReason = "Checkout je otkazan."
		}
	}

	h.renderPage(c, http.StatusOK, "training_room", data)
}

func (h *HttpHandler) signRecordingUploadHandler(c *gin.Context) {
	var input struct {
		ContentType string `json:"contentType"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	upload, err := h.svc.SignRecordingUpload(c.Request.Context(), c.Param("id"), input.ContentType)
	if err != nil {
		if errors.Is(err, service.ErrStorageDisabled) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": h.svc.RecordingStorageDisabledReason()})
			return
		}
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "training not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, upload)
}

func (h *HttpHandler) completeRecordingHandler(c *gin.Context) {
	var input struct {
		RecordingID      string `json:"recordingId"`
		UploadID         string `json:"uploadId"`
		ObjectName       string `json:"objectName"`
		OriginalFilename string `json:"originalFilename"`
		ContentType      string `json:"contentType"`
		SizeBytes        int64  `json:"sizeBytes"`
		DurationSeconds  int    `json:"durationSeconds"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user := h.mustCurrentUser(c)
	recording, err := h.svc.CompleteRecording(c.Request.Context(), model.Recording{
		ID:               strings.TrimSpace(input.RecordingID),
		TrainingID:       c.Param("id"),
		ObjectName:       strings.TrimSpace(input.ObjectName),
		OriginalFilename: strings.TrimSpace(input.OriginalFilename),
		ContentType:      strings.TrimSpace(input.ContentType),
		SizeBytes:        input.SizeBytes,
		DurationSeconds:  input.DurationSeconds,
		CreatedBy:        user.ID,
	}, strings.TrimSpace(input.UploadID))
	if err != nil {
		if errors.Is(err, service.ErrStorageDisabled) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": h.svc.RecordingStorageDisabledReason()})
			return
		}
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "training not found"})
			return
		}
		if errors.Is(err, service.ErrInvalidRecording) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid recording"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, recording)
}

func (h *HttpHandler) uploadRecordingChunkHandler(c *gin.Context) {
	user := h.mustCurrentUser(c)
	if user == nil {
		c.String(http.StatusUnauthorized, "unauthorized")
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	if err := h.svc.UploadLocalRecordingChunk(c.Param("id"), c.Param("uploadID"), c.GetHeader("Content-Range"), body); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.Status(http.StatusNotFound)
			return
		}
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *HttpHandler) recordingAccessHandler(download bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := h.mustCurrentUser(c)
		if user == nil {
			c.String(http.StatusUnauthorized, "unauthorized")
			return
		}
		if user.Role != "admin" {
			canAccess, _, err := h.userAccess(c)
			if err != nil {
				c.String(http.StatusInternalServerError, err.Error())
				return
			}
			if !canAccess {
				c.Status(http.StatusForbidden)
				return
			}
		}

		access, err := h.svc.RecordingAccess(c.Request.Context(), c.Param("id"), c.Param("recordingID"), download)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				c.Status(http.StatusNotFound)
				return
			}
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		if access.Mode == "local" {
			if download {
				c.FileAttachment(access.FilePath, access.Recording.OriginalFilename)
				return
			}
			c.File(access.FilePath)
			return
		}

		c.Redirect(http.StatusTemporaryRedirect, access.AccessURL)
	}
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
	adminTrainings := h.adminTrainingsWithLiveStats(trainings)

	h.renderPage(c, http.StatusOK, "admin", model.AdminPageData{
		BasePageData: model.BasePageData{
			CurrentUser: h.mustCurrentUser(c),
			Success:     map[string]string{"1": "Trening je kreiran."}[c.Query("created")],
		},
		Trainings:             adminTrainings,
		BillingEnabled:        h.svc.BillingEnabled(),
		BillingDisabledReason: h.svc.BillingDisabledReason(),
		StripePriceID:         h.cfg.Billing.StripePriceID,
	})
}

func (h *HttpHandler) adminLiveStatsHandler(c *gin.Context) {
	trainings, err := h.svc.HomeTrainings(c.Request.Context())
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	roomIDs := make([]string, 0, len(trainings))
	for _, training := range trainings {
		roomIDs = append(roomIDs, training.ID)
	}
	stats := h.hub.Stats(roomIDs)

	response := make(map[string]model.LiveStats, len(stats))
	for roomID, stat := range stats {
		response[roomID] = model.LiveStats{
			Online:   stat.Online,
			Watching: stat.Watching,
		}
	}
	c.JSON(http.StatusOK, response)
}

func (h *HttpHandler) adminTrainingsWithLiveStats(trainings []model.Training) []model.AdminTraining {
	roomIDs := make([]string, 0, len(trainings))
	for _, training := range trainings {
		roomIDs = append(roomIDs, training.ID)
	}
	stats := h.hub.Stats(roomIDs)

	adminTrainings := make([]model.AdminTraining, 0, len(trainings))
	for _, training := range trainings {
		stat := stats[training.ID]
		adminTrainings = append(adminTrainings, model.AdminTraining{
			Training: training,
			LiveStats: model.LiveStats{
				Online:   stat.Online,
				Watching: stat.Watching,
			},
		})
	}
	return adminTrainings
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
				Trainings: h.adminTrainingsWithLiveStats(trainings),
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

func safeReturnPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/billing"
	}
	u, err := url.Parse(value)
	if err != nil || u.IsAbs() || !strings.HasPrefix(value, "/") {
		return "/billing"
	}
	return value
}

func (h *HttpHandler) userAccess(c *gin.Context) (bool, *model.Subscription, error) {
	user := h.mustCurrentUser(c)
	return h.svc.UserCanAccess(c.Request.Context(), user)
}

func (h *HttpHandler) billingPageHandler(c *gin.Context) {
	user := h.mustCurrentUser(c)
	canAccess, subscription, err := h.userAccess(c)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	data := model.BillingPageData{
		BasePageData:          model.BasePageData{CurrentUser: h.mustCurrentUser(c)},
		BillingEnabled:        h.svc.BillingEnabled(),
		BillingDisabledReason: h.svc.BillingDisabledReason(),
		Subscription:          subscription,
		ReturnTo:              "/billing",
	}
	if h.svc.BillingEnabled() && canAccess && user != nil && user.Role != "admin" && subscription != nil {
		data.Success = "Pretplata je aktivna."
	}
	if msg := strings.TrimSpace(c.Query("billing")); msg != "" {
		switch msg {
		case "success":
			data.Success = "Pretplata je aktivirana."
		case "canceled":
			data.Error = "Checkout je otkazan."
		case "missing":
			data.Error = "Nema spremljenog billing naloga. Pokreni checkout prvo."
		}
	}

	h.renderPage(c, http.StatusOK, "billing", data)
}

func (h *HttpHandler) billingCheckoutHandler(c *gin.Context) {
	user := h.mustCurrentUser(c)
	if user == nil {
		c.String(http.StatusUnauthorized, "unauthorized")
		return
	}

	canAccess, _, err := h.userAccess(c)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if canAccess && user.Role != "admin" {
		c.Redirect(http.StatusSeeOther, "/billing")
		return
	}

	returnTo := safeReturnPath(c.PostForm("return_to"))
	url, err := h.svc.CreateCheckoutSession(c.Request.Context(), user, returnTo)
	if err != nil {
		if errors.Is(err, service.ErrStorageDisabled) {
			c.String(http.StatusServiceUnavailable, h.svc.BillingDisabledReason())
			return
		}
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	c.Redirect(http.StatusSeeOther, url)
}

func (h *HttpHandler) billingPortalHandler(c *gin.Context) {
	user := h.mustCurrentUser(c)
	if user == nil {
		c.String(http.StatusUnauthorized, "unauthorized")
		return
	}

	returnTo := safeReturnPath(c.PostForm("return_to"))
	url, err := h.svc.CreateBillingPortalSession(c.Request.Context(), user, returnTo)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			c.Redirect(http.StatusSeeOther, "/billing?billing=missing")
			return
		}
		if errors.Is(err, service.ErrStorageDisabled) {
			c.String(http.StatusServiceUnavailable, h.svc.BillingDisabledReason())
			return
		}
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	c.Redirect(http.StatusSeeOther, url)
}

func (h *HttpHandler) billingCompleteHandler(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Query("session_id"))
	if sessionID == "" {
		c.String(http.StatusBadRequest, "session_id is required")
		return
	}

	returnTo := safeReturnPath(c.Query("return_to"))
	if err := h.svc.FinalizeCheckoutSession(c.Request.Context(), sessionID); err != nil {
		if errors.Is(err, service.ErrStorageDisabled) {
			c.String(http.StatusServiceUnavailable, h.svc.BillingDisabledReason())
			return
		}
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	target := returnTo
	if strings.Contains(target, "?") {
		target += "&billing=success"
	} else {
		target += "?billing=success"
	}
	c.Redirect(http.StatusSeeOther, target)
}

func (h *HttpHandler) stripeWebhookHandler(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	if err := h.svc.HandleStripeWebhook(c.Request.Context(), body, c.GetHeader("Stripe-Signature")); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	c.Status(http.StatusOK)
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

	canAccess, _, err := h.userAccess(c)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if !canAccess && user.Role != "admin" {
		c.Status(http.StatusForbidden)
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
