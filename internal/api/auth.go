package api

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type proxyConfig struct {
	all      bool
	prefixes []netip.Prefix
}

var trustedProxyConfig = struct {
	sync.RWMutex
	config proxyConfig
}{
	config: proxyConfig{prefixes: []netip.Prefix{
		netip.MustParsePrefix("127.0.0.0/8"),
		netip.MustParsePrefix("::1/128"),
	}},
}

// SetTrustedProxies configures which peers may provide forwarded client IPs.
func SetTrustedProxies(spec string) error {
	spec = strings.TrimSpace(spec)
	config := proxyConfig{all: spec == "*"}
	if spec != "" && !config.all {
		for _, value := range strings.Split(spec, ",") {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if strings.EqualFold(value, "loopback") {
				config.prefixes = append(config.prefixes,
					netip.MustParsePrefix("127.0.0.0/8"),
					netip.MustParsePrefix("::1/128"),
				)
				continue
			}

			var prefix netip.Prefix
			var err error
			if strings.Contains(value, "/") {
				prefix, err = netip.ParsePrefix(value)
			} else {
				var addr netip.Addr
				addr, err = netip.ParseAddr(value)
				if err == nil {
					addr = addr.WithZone("").Unmap()
					prefix = netip.PrefixFrom(addr, addr.BitLen())
				}
			}
			if err != nil {
				return err
			}
			config.prefixes = append(config.prefixes, prefix.Masked())
		}
	}

	trustedProxyConfig.Lock()
	trustedProxyConfig.config = config
	trustedProxyConfig.Unlock()
	return nil
}

func currentProxyConfig() proxyConfig {
	trustedProxyConfig.RLock()
	config := trustedProxyConfig.config
	trustedProxyConfig.RUnlock()
	return config
}

func (c proxyConfig) contains(ip netip.Addr) bool {
	if c.all {
		return true
	}
	for _, prefix := range c.prefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func parseRemoteIP(remoteAddr string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = strings.Trim(strings.TrimSpace(remoteAddr), "[]")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return ip.WithZone("").Unmap(), true
}

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
	peer, ok := parseRemoteIP(r.RemoteAddr)
	if !ok {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			return strings.TrimSpace(host)
		}
		return strings.TrimSpace(r.RemoteAddr)
	}

	config := currentProxyConfig()
	if !config.contains(peer) {
		return peer.String()
	}

	parts := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	var forwarded netip.Addr
	for i := len(parts) - 1; i >= 0; i-- {
		ip, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			continue
		}
		forwarded = ip.WithZone("").Unmap()
		if !config.contains(forwarded) {
			return forwarded.String()
		}
	}
	if forwarded.IsValid() {
		return forwarded.String()
	}

	if ip, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); err == nil {
		return ip.WithZone("").Unmap().String()
	}
	return peer.String()
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
