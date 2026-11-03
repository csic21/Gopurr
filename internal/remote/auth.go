package remote

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie = "gopurr_remote"
	sessionTTL    = 7 * 24 * time.Hour
	maxSessions   = 8
)

type session struct {
	csrf    string
	expires time.Time
	ip      string
}

type loginAttempt struct {
	failures     int
	blockedUntil time.Time
	lastSeen     time.Time
}

type authState struct {
	mu       sync.Mutex
	sessions map[string]session
	logins   map[string]*loginAttempt
	now      func() time.Time
}

func newAuthState() *authState {
	return &authState{
		sessions: map[string]session{},
		logins:   map[string]*loginAttempt{},
		now:      time.Now,
	}
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func sessionKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (a *authState) create(ip string) (token, csrf string, err error) {
	token, err = randomToken()
	if err != nil {
		return "", "", err
	}
	csrf, err = randomToken()
	if err != nil {
		return "", "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	a.pruneLocked(now)
	if len(a.sessions) >= maxSessions {
		var oldestKey string
		var oldest time.Time
		for key, sess := range a.sessions {
			if oldestKey == "" || sess.expires.Before(oldest) {
				oldestKey, oldest = key, sess.expires
			}
		}
		delete(a.sessions, oldestKey)
	}
	a.sessions[sessionKey(token)] = session{csrf: csrf, expires: now.Add(sessionTTL), ip: ip}
	return token, csrf, nil
}

func (a *authState) get(token string) (session, bool) {
	if token == "" {
		return session{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	key := sessionKey(token)
	s, ok := a.sessions[key]
	if !ok {
		return session{}, false
	}
	if now.After(s.expires) {
		delete(a.sessions, key)
		return session{}, false
	}
	// Refresh active sessions so a phone that is used regularly does not have
	// to re-enter the token after a week.
	if s.expires.Sub(now) < sessionTTL/2 {
		s.expires = now.Add(sessionTTL)
		a.sessions[key] = s
	}
	return s, true
}

func (a *authState) delete(token string) {
	if token == "" {
		return
	}
	a.mu.Lock()
	delete(a.sessions, sessionKey(token))
	a.mu.Unlock()
}

func (a *authState) clear() {
	a.mu.Lock()
	a.sessions = map[string]session{}
	a.mu.Unlock()
}

func (a *authState) pruneLocked(now time.Time) {
	for key, s := range a.sessions {
		if now.After(s.expires) {
			delete(a.sessions, key)
		}
	}
	for ip, attempt := range a.logins {
		if now.Sub(attempt.lastSeen) > 15*time.Minute {
			delete(a.logins, ip)
		}
	}
}

func (a *authState) loginAllowed(ip string) (bool, time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	attempt := a.logins[ip]
	if attempt == nil {
		return true, 0
	}
	if now.Before(attempt.blockedUntil) {
		return false, attempt.blockedUntil.Sub(now)
	}
	return true, 0
}

func (a *authState) loginFailed(ip string) time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	attempt := a.logins[ip]
	if attempt == nil || now.Sub(attempt.lastSeen) > 15*time.Minute {
		attempt = &loginAttempt{}
		a.logins[ip] = attempt
	}
	attempt.failures++
	attempt.lastSeen = now
	if len(a.logins) > 1024 {
		for key, item := range a.logins {
			if now.Sub(item.lastSeen) > 15*time.Minute {
				delete(a.logins, key)
			}
		}
	}
	if attempt.failures < 5 {
		return 0
	}
	delay := 30 * time.Second
	for i := 5; i < attempt.failures && delay < 15*time.Minute; i++ {
		delay *= 2
	}
	if delay > 15*time.Minute {
		delay = 15 * time.Minute
	}
	attempt.blockedUntil = now.Add(delay)
	return delay
}

func (a *authState) loginSucceeded(ip string) {
	a.mu.Lock()
	delete(a.logins, ip)
	a.mu.Unlock()
}

func requestIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func sameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

func csrfMatches(want, got string) bool {
	if want == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

func tokenMatches(want, got string) bool {
	if want == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}
