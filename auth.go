package dbadminer

import (
	"crypto/sha256"
	"crypto/subtle"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	authFailLimit  = 8
	authFailWindow = time.Minute
)

type authenticator struct {
	enabled bool
	user    string
	pass    string
	limiter *attemptLimiter
}

func newAuthenticator(user, pass string) *authenticator {
	return &authenticator{
		enabled: user != "" || pass != "",
		user:    user,
		pass:    pass,
		limiter: newAttemptLimiter(authFailLimit, authFailWindow),
	}
}

func constTimeEqual(got, want string) bool {
	sumGot := sha256.Sum256([]byte(got))
	sumWant := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(sumGot[:], sumWant[:]) == 1
}

func (a *authenticator) wrap(next http.Handler) http.Handler {
	if !a.enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := remoteIP(r)
		if a.limiter.blocked(ip) {
			http.Error(w, "too many attempts", http.StatusTooManyRequests)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || !constTimeEqual(user, a.user) || !constTimeEqual(pass, a.pass) {
			a.limiter.fail(ip)
			if a.limiter.blocked(ip) {
				http.Error(w, "too many attempts", http.StatusTooManyRequests)
				return
			}
			w.Header().Set("WWW-Authenticate", `Basic realm="dbadminer"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		a.limiter.success(ip)
		next.ServeHTTP(w, r)
	})
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type attemptLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
	now    func() time.Time
}

func newAttemptLimiter(limit int, window time.Duration) *attemptLimiter {
	return &attemptLimiter{
		hits:   map[string][]time.Time{},
		limit:  limit,
		window: window,
		now:    time.Now,
	}
}

func (l *attemptLimiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(key)
	return len(l.hits[key]) >= l.limit
}

func (l *attemptLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(key)
	l.hits[key] = append(l.hits[key], l.now())
}

func (l *attemptLimiter) success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}

func (l *attemptLimiter) prune(key string) {
	cutoff := l.now().Add(-l.window)
	hits := l.hits[key]
	kept := hits[:0]
	for _, ts := range hits {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	if len(kept) == 0 {
		delete(l.hits, key)
		return
	}
	l.hits[key] = kept
}
