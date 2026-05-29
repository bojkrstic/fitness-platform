package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
)

const sessionCookieName = "fitness_session"

type sessionData struct {
	UserID string
}

type SessionManager struct {
	mu         sync.RWMutex
	sessions   map[string]sessionData
	cookieName string
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions:   make(map[string]sessionData),
		cookieName: sessionCookieName,
	}
}

func (s *SessionManager) Create(w http.ResponseWriter, userID string) {
	token := randomToken()

	s.mu.Lock()
	s.sessions[token] = sessionData{UserID: userID}
	s.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *SessionManager) Current(r *http.Request) (sessionData, bool) {
	cookie, err := r.Cookie(s.cookieName)
	if err != nil {
		return sessionData{}, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	data, ok := s.sessions[cookie.Value]
	return data, ok
}

func (s *SessionManager) Destroy(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(s.cookieName)
	if err == nil {
		s.mu.Lock()
		delete(s.sessions, cookie.Value)
		s.mu.Unlock()
	}

	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}

	return hex.EncodeToString(b[:])
}
