package session

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
)

type Data struct {
	UserID string
}

type Manager struct {
	mu         sync.RWMutex
	sessions   map[string]Data
	cookieName string
}

const CookieName = "fitness_session"

func NewManager() *Manager {
	return &Manager{
		sessions:   make(map[string]Data),
		cookieName: CookieName,
	}
}

func (s *Manager) Create(w http.ResponseWriter, userID string) {
	token := randomToken()

	s.mu.Lock()
	s.sessions[token] = Data{UserID: userID}
	s.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Manager) Current(r *http.Request) (Data, bool) {
	cookie, err := r.Cookie(s.cookieName)
	if err != nil {
		return Data{}, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	data, ok := s.sessions[cookie.Value]
	return data, ok
}

func (s *Manager) Destroy(w http.ResponseWriter, r *http.Request) {
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
