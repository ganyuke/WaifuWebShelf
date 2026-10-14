package web

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	loginMaxFails = 5
	loginWindow   = 15 * time.Minute
)

// loginThrottle counts failed logins per address. After loginMaxFails misses
// inside loginWindow the address is refused until the window has passed.
type loginThrottle struct {
	mu    sync.Mutex
	fails map[string]*loginFails
}

type loginFails struct {
	count int
	since time.Time
}

func newLoginThrottle() *loginThrottle {
	return &loginThrottle{fails: map[string]*loginFails{}}
}

// wait returns how long the address must wait, or zero if it may try.
func (t *loginThrottle) wait(ip string, now time.Time) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	f := t.fails[ip]
	if f == nil {
		return 0
	}
	if now.Sub(f.since) >= loginWindow {
		delete(t.fails, ip)
		return 0
	}
	if f.count >= loginMaxFails {
		return f.since.Add(loginWindow).Sub(now)
	}
	return 0
}

func (t *loginThrottle) fail(ip string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, f := range t.fails {
		if now.Sub(f.since) >= loginWindow {
			delete(t.fails, k)
		}
	}
	f := t.fails[ip]
	if f == nil {
		f = &loginFails{since: now}
		t.fails[ip] = f
	}
	f.count++
}

func (t *loginThrottle) clear(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.fails, ip)
}

func (s *Server) refuseLoginWait(w http.ResponseWriter, r *http.Request, next string, wait time.Duration) {
	secs := int(wait.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusTooManyRequests)
	s.renderErr(w, r, "login", "Log in", "Too many wrong passwords. Wait a few minutes and try again.", map[string]any{
		"Next": next,
	})
}
