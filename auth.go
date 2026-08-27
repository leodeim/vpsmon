package main

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"sync"
	"time"
)

// --- Rate Limiting ---
type loginAttempt struct {
	count int
	last  time.Time
}

var (
	loginMu       sync.Mutex
	loginAttempts = make(map[string]*loginAttempt)
)

func getIP(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.Header.Get("X-Real-IP")
	}
	if ip == "" {
		ip, _, _ = net.SplitHostPort(r.RemoteAddr)
	}
	return ip
}

func rateLimit(ip string) bool {
	loginMu.Lock()
	defer loginMu.Unlock()

	now := time.Now()
	attempt, ok := loginAttempts[ip]
	if !ok {
		loginAttempts[ip] = &loginAttempt{count: 1, last: now}
		return true
	}

	if now.Sub(attempt.last) > 5*time.Minute {
		attempt.count = 1
		attempt.last = now
		return true
	}

	attempt.count++
	attempt.last = now

	if attempt.count > 5 {
		return false
	}
	return true
}

// --- Session store ---
type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]time.Time
}

var sessions = &sessionStore{sessions: make(map[string]time.Time)}

const sessionTTL = 24 * time.Hour

func (s *sessionStore) create() string {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(sessionTTL)
	s.mu.Unlock()
	return token
}

func (s *sessionStore) valid(token string) bool {
	s.mu.RLock()
	exp, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return false
	}
	return true
}

func (s *sessionStore) destroy(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

func authenticated(r *http.Request) bool {
	c, err := r.Cookie("session")
	if err != nil {
		return false
	}
	return sessions.valid(c.Value)
}

func init() {
	go func() {
		for {
			time.Sleep(1 * time.Hour)
			now := time.Now()

			// Cleanup expired sessions
			sessions.mu.Lock()
			for token, exp := range sessions.sessions {
				if now.After(exp) {
					delete(sessions.sessions, token)
				}
			}
			sessions.mu.Unlock()

			// Cleanup old rate limit entries
			loginMu.Lock()
			for ip, attempt := range loginAttempts {
				if now.Sub(attempt.last) > 5*time.Minute {
					delete(loginAttempts, ip)
				}
			}
			loginMu.Unlock()
		}
	}()
}
