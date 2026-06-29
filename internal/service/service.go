package service

import (
	"context"
	"crypto"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

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
	ErrStorageDisabled    = errors.New("recording storage disabled")
	ErrInvalidRecording   = errors.New("invalid recording")
)

type Service struct {
	store             repository.Store
	seedAdminEmail    string
	seedAdminPassword string
	recordingStorage  config.RecordingStorage
	billing           config.Billing
	localUploads      map[string]*localRecordingUpload
	localUploadsMu    sync.Mutex
}

func New(store repository.Store, cfg config.Config) *Service {
	return &Service{
		store:             store,
		seedAdminEmail:    cfg.SeedAdminEmail,
		seedAdminPassword: cfg.SeedAdminPassword,
		recordingStorage:  cfg.RecordingStorage,
		billing:           cfg.Billing,
		localUploads:      map[string]*localRecordingUpload{},
	}
}

type RecordingUpload struct {
	RecordingID string `json:"recordingId"`
	UploadID    string `json:"uploadId"`
	ObjectName  string `json:"objectName"`
	UploadURL   string `json:"uploadUrl"`
	ContentType string `json:"contentType"`
	ExpiresAt   string `json:"expiresAt"`
	Mode        string `json:"mode"`
}

type RecordingAccess struct {
	Recording model.Recording
	AccessURL string
	FilePath  string
	Mode      string
}

type localRecordingUpload struct {
	uploadID    string
	trainingID  string
	recordingID string
	objectName  string
	filePath    string
	contentType string
	sizeBytes   int64
}

func (s *Service) Init(ctx context.Context) error {
	if err := s.store.Init(ctx); err != nil {
		return err
	}

	if err := os.MkdirAll(s.recordingStorage.LocalDir, 0o755); err != nil {
		return fmt.Errorf("prepare recordings dir: %w", err)
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

func (s *Service) Recordings(ctx context.Context, trainingID string) ([]model.Recording, error) {
	return s.store.ListRecordings(ctx, trainingID)
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

func (s *Service) RecordingStorageEnabled() bool {
	return true
}

func (s *Service) RecordingStorageDisabledReason() string {
	if s.recordingStorage.Enabled() {
		return ""
	}

	return ""
}

func (s *Service) RecordingStorageModeLabel() string {
	return s.recordingStorage.ModeLabel()
}

func (s *Service) SignRecordingUpload(ctx context.Context, trainingID, contentType string) (RecordingUpload, error) {
	if _, err := s.store.FindTrainingByID(ctx, trainingID); err != nil {
		return RecordingUpload{}, err
	}

	contentType = normalizeRecordingContentType(contentType)
	recordingID := randomID()
	now := time.Now().UTC()
	objectName := path.Join(
		"recordings",
		now.Format("2006"),
		now.Format("01"),
		now.Format("02"),
		trainingID,
		recordingID+".webm",
	)
	expiresAt := now.Add(15 * time.Minute)

	if s.recordingStorage.UsesGCS() {
		sessionStartURL, err := s.signStorageURL("POST", objectName, map[string]string{
			"content-type":     contentType,
			"x-goog-resumable": "start",
		}, nil, expiresAt)
		if err != nil {
			return RecordingUpload{}, err
		}

		uploadURL, err := s.openResumableUploadSession(ctx, sessionStartURL, contentType)
		if err != nil {
			return RecordingUpload{}, err
		}

		return RecordingUpload{
			RecordingID: recordingID,
			ObjectName:  objectName,
			UploadURL:   uploadURL,
			ContentType: contentType,
			ExpiresAt:   expiresAt.Format(time.RFC3339),
			Mode:        "gcs",
		}, nil
	}

	uploadID := randomID()
	filePath, err := s.localUploadPath(uploadID)
	if err != nil {
		return RecordingUpload{}, err
	}

	s.localUploadsMu.Lock()
	s.localUploads[uploadID] = &localRecordingUpload{
		uploadID:    uploadID,
		trainingID:  trainingID,
		recordingID: recordingID,
		objectName:  objectName,
		filePath:    filePath,
		contentType: contentType,
	}
	s.localUploadsMu.Unlock()

	return RecordingUpload{
		RecordingID: recordingID,
		UploadID:    uploadID,
		ObjectName:  objectName,
		UploadURL:   "/trainings/" + trainingID + "/recordings/uploads/" + uploadID,
		ContentType: contentType,
		ExpiresAt:   expiresAt.Format(time.RFC3339),
		Mode:        "local",
	}, nil
}

func (s *Service) openResumableUploadSession(ctx context.Context, sessionStartURL, contentType string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sessionStartURL, nil)
	if err != nil {
		return "", fmt.Errorf("create storage session request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("x-goog-resumable", "start")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("open storage upload session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("open storage upload session: status %d", resp.StatusCode)
	}

	sessionURL := strings.TrimSpace(resp.Header.Get("Location"))
	if sessionURL == "" {
		return "", fmt.Errorf("open storage upload session: missing Location header")
	}

	return sessionURL, nil
}

func (s *Service) CompleteRecording(ctx context.Context, recording model.Recording) (model.Recording, error) {
	if recording.ID == "" || recording.TrainingID == "" || recording.ObjectName == "" || recording.CreatedBy == "" {
		return model.Recording{}, ErrInvalidRecording
	}
	if _, err := s.store.FindTrainingByID(ctx, recording.TrainingID); err != nil {
		return model.Recording{}, err
	}

	expectedPrefix := "recordings/"
	expectedSuffix := "/" + recording.TrainingID + "/" + recording.ID + ".webm"
	if !strings.HasPrefix(recording.ObjectName, expectedPrefix) || !strings.HasSuffix(recording.ObjectName, expectedSuffix) {
		return model.Recording{}, ErrInvalidRecording
	}

	recording.ContentType = normalizeRecordingContentType(recording.ContentType)
	recording.OriginalFilename = safeRecordingFilename(recording.OriginalFilename, recording.ID)
	if recording.SizeBytes < 0 || recording.DurationSeconds < 0 {
		return model.Recording{}, ErrInvalidRecording
	}

	if !s.recordingStorage.UsesGCS() {
		if err := s.finalizeLocalRecording(recording.ObjectName, recording.SizeBytes); err != nil {
			return model.Recording{}, err
		}
	}

	return s.store.CreateRecording(ctx, recording)
}

func (s *Service) RecordingAccess(ctx context.Context, trainingID, recordingID string, download bool) (RecordingAccess, error) {
	recording, err := s.store.FindRecordingByID(ctx, recordingID)
	if err != nil {
		return RecordingAccess{}, err
	}
	if recording.TrainingID != trainingID {
		return RecordingAccess{}, repository.ErrNotFound
	}

	if !s.recordingStorage.UsesGCS() {
		filePath := filepath.Join(s.recordingStorage.LocalDir, filepath.FromSlash(recording.ObjectName))
		return RecordingAccess{
			Recording: recording,
			FilePath:  filePath,
			Mode:      "local",
		}, nil
	}

	query := url.Values{}
	if download {
		query.Set("response-content-disposition", fmt.Sprintf(`attachment; filename="%s"`, safeRecordingFilename(recording.OriginalFilename, recording.ID)))
	} else {
		query.Set("response-content-disposition", fmt.Sprintf(`inline; filename="%s"`, safeRecordingFilename(recording.OriginalFilename, recording.ID)))
	}

	accessURL, err := s.signStorageURL("GET", recording.ObjectName, nil, query, time.Now().UTC().Add(30*time.Minute))
	if err != nil {
		return RecordingAccess{}, err
	}

	return RecordingAccess{
		Recording: recording,
		AccessURL: accessURL,
		Mode:      "gcs",
	}, nil
}

func (s *Service) RecordingUploadChunk(ctx context.Context, trainingID, uploadID, contentRange string, body []byte) error {
	if s.recordingStorage.UsesGCS() {
		return fmt.Errorf("recording upload chunk only applies to local storage")
	}
	_ = ctx
	return s.UploadLocalRecordingChunk(trainingID, uploadID, contentRange, body)
}

func (s *Service) localUploadPath(uploadID string) (string, error) {
	if uploadID == "" {
		return "", fmt.Errorf("upload id is required")
	}

	dir := filepath.Join(s.recordingStorage.LocalDir, ".uploads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("prepare upload dir: %w", err)
	}

	return filepath.Join(dir, uploadID+".part"), nil
}

func (s *Service) UploadLocalRecordingChunk(trainingID, uploadID string, contentRange string, body []byte) error {
	start, end, total, _, err := parseContentRange(contentRange)
	if err != nil {
		return err
	}

	s.localUploadsMu.Lock()
	session, ok := s.localUploads[uploadID]
	s.localUploadsMu.Unlock()
	if !ok {
		return fmt.Errorf("local upload not found")
	}
	if session.trainingID != trainingID {
		return repository.ErrNotFound
	}
	if len(body) == 0 {
		return fmt.Errorf("empty upload chunk")
	}
	if int64(start) != session.sizeBytes {
		return fmt.Errorf("unexpected upload offset")
	}
	if int64(len(body)) != int64(end-start+1) {
		return fmt.Errorf("chunk size mismatch")
	}
	if total >= 0 && int64(total) != int64(end+1) {
		return fmt.Errorf("invalid content range")
	}
	if err := os.MkdirAll(filepath.Dir(session.filePath), 0o755); err != nil {
		return fmt.Errorf("prepare upload file path: %w", err)
	}

	file, err := os.OpenFile(session.filePath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open upload file: %w", err)
	}
	defer file.Close()

	if _, err := file.WriteAt(body, int64(start)); err != nil {
		return fmt.Errorf("write upload chunk: %w", err)
	}
	session.sizeBytes = int64(end + 1)

	return nil
}

func (s *Service) finalizeLocalRecording(objectName string, sizeBytes int64) error {
	if sizeBytes < 0 {
		return ErrInvalidRecording
	}

	s.localUploadsMu.Lock()
	var session *localRecordingUpload
	for _, candidate := range s.localUploads {
		if candidate.objectName == objectName {
			session = candidate
			break
		}
	}
	s.localUploadsMu.Unlock()
	if session == nil {
		return fmt.Errorf("local recording session not found")
	}
	if session.sizeBytes != sizeBytes {
		return fmt.Errorf("local recording size mismatch")
	}

	if err := os.MkdirAll(filepath.Dir(filepath.Join(s.recordingStorage.LocalDir, filepath.FromSlash(objectName))), 0o755); err != nil {
		return fmt.Errorf("prepare recording dir: %w", err)
	}

	finalPath := filepath.Join(s.recordingStorage.LocalDir, filepath.FromSlash(objectName))
	if err := os.Rename(session.filePath, finalPath); err != nil {
		return fmt.Errorf("finalize recording: %w", err)
	}

	s.localUploadsMu.Lock()
	delete(s.localUploads, session.uploadID)
	s.localUploadsMu.Unlock()
	return nil
}

func (s *Service) signStorageURL(method, objectName string, headers map[string]string, extraQuery url.Values, expiresAt time.Time) (string, error) {
	privateKey, err := parseRSAPrivateKey(s.recordingStorage.PrivateKey)
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	expires := int(expiresAt.Sub(now).Seconds())
	if expires <= 0 {
		return "", fmt.Errorf("signed URL expiry must be in the future")
	}
	if expires > 604800 {
		return "", fmt.Errorf("signed URL expiry cannot exceed seven days")
	}

	date := now.Format("20060102")
	timestamp := now.Format("20060102T150405Z")
	scope := date + "/auto/storage/goog4_request"
	credential := s.recordingStorage.ServiceAccountEmail + "/" + scope

	query := url.Values{}
	for key, values := range extraQuery {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	query.Set("X-Goog-Algorithm", "GOOG4-RSA-SHA256")
	query.Set("X-Goog-Credential", credential)
	query.Set("X-Goog-Date", timestamp)
	query.Set("X-Goog-Expires", fmt.Sprintf("%d", expires))

	canonicalHeaderValues := map[string]string{"host": "storage.googleapis.com"}
	for key, value := range headers {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		canonicalHeaderValues[key] = strings.Join(strings.Fields(value), " ")
	}

	headerNames := make([]string, 0, len(canonicalHeaderValues))
	for key := range canonicalHeaderValues {
		headerNames = append(headerNames, key)
	}
	sort.Strings(headerNames)

	var canonicalHeaders strings.Builder
	for _, key := range headerNames {
		canonicalHeaders.WriteString(key)
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(canonicalHeaderValues[key])
		canonicalHeaders.WriteString("\n")
	}
	signedHeaders := strings.Join(headerNames, ";")
	query.Set("X-Goog-SignedHeaders", signedHeaders)

	canonicalURI := "/" + escapePathPart(s.recordingStorage.Bucket) + "/" + escapeObjectPath(objectName)
	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI,
		canonicalQuery(query),
		canonicalHeaders.String(),
		signedHeaders,
		"UNSIGNED-PAYLOAD",
	}, "\n")

	hashedCanonicalRequest := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{
		"GOOG4-RSA-SHA256",
		timestamp,
		scope,
		hex.EncodeToString(hashedCanonicalRequest[:]),
	}, "\n")

	hashedStringToSign := sha256.Sum256([]byte(stringToSign))
	signature, err := rsa.SignPKCS1v15(crand.Reader, privateKey, crypto.SHA256, hashedStringToSign[:])
	if err != nil {
		return "", fmt.Errorf("sign storage URL: %w", err)
	}

	query.Set("X-Goog-Signature", hex.EncodeToString(signature))

	return "https://storage.googleapis.com" + canonicalURI + "?" + canonicalQuery(query), nil
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
		{Title: "Trening Goran Krstic", Trainer: "Goran Krstic", Description: "Intenzivan trening za celo telo.", Time: "09:00"},
		{Title: "Trening Emanuela Krstic", Trainer: "fitnes instruktor Emanuela", Description: "Program za snagu, fleksibilnost i kontrolu pokreta.", Time: "18:00"},
	}

	for _, input := range defaults {
		if _, err := s.store.CreateTraining(ctx, input, admin.ID); err != nil {
			return err
		}
	}

	return nil
}

func normalizeRecordingContentType(contentType string) string {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch contentType {
	case "video/webm":
		return contentType
	default:
		return "video/webm"
	}
}

func safeRecordingFilename(filename, fallbackID string) string {
	filename = strings.TrimSpace(path.Base(filename))
	filename = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, filename)
	filename = strings.Trim(filename, ".-")
	if filename == "" {
		filename = fallbackID + ".webm"
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".webm") {
		filename += ".webm"
	}
	return filename
}

func parseRSAPrivateKey(value string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, fmt.Errorf("GCS_PRIVATE_KEY must be a PEM encoded RSA private key")
	}

	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("GCS_PRIVATE_KEY must contain an RSA private key")
		}
		return rsaKey, nil
	}

	rsaKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse GCS_PRIVATE_KEY: %w", err)
	}
	return rsaKey, nil
}

func canonicalQuery(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var parts []string
	for _, key := range keys {
		keyValues := append([]string(nil), values[key]...)
		sort.Strings(keyValues)
		for _, value := range keyValues {
			parts = append(parts, rfc3986Escape(key)+"="+rfc3986Escape(value))
		}
	}

	return strings.Join(parts, "&")
}

func escapeObjectPath(value string) string {
	parts := strings.Split(value, "/")
	for i, part := range parts {
		parts[i] = escapePathPart(part)
	}
	return strings.Join(parts, "/")
}

func escapePathPart(value string) string {
	return rfc3986Escape(value)
}

func rfc3986Escape(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func parseContentRange(value string) (start int64, end int64, total int64, final bool, err error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "bytes ") {
		return 0, 0, -1, false, fmt.Errorf("invalid content range")
	}

	rest := strings.TrimSpace(strings.TrimPrefix(value, "bytes "))
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		return 0, 0, -1, false, fmt.Errorf("invalid content range")
	}

	rangePart := strings.TrimSpace(parts[0])
	totalPart := strings.TrimSpace(parts[1])
	bounds := strings.Split(rangePart, "-")
	if len(bounds) != 2 {
		return 0, 0, -1, false, fmt.Errorf("invalid content range")
	}

	start, err = parseInt64(bounds[0])
	if err != nil {
		return 0, 0, -1, false, fmt.Errorf("invalid content range")
	}
	end, err = parseInt64(bounds[1])
	if err != nil || end < start {
		return 0, 0, -1, false, fmt.Errorf("invalid content range")
	}

	if totalPart == "*" {
		return start, end, -1, false, nil
	}

	total, err = parseInt64(totalPart)
	if err != nil || total <= 0 {
		return 0, 0, -1, false, fmt.Errorf("invalid content range")
	}

	final = end+1 == total
	return start, end, total, final, nil
}

func parseInt64(value string) (int64, error) {
	var n int64
	_, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func randomID() string {
	var b [16]byte
	if _, err := crand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("generate id: %v", err))
	}

	return hex.EncodeToString(b[:])
}
