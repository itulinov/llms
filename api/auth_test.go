package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeStore struct {
	err    error
	u      user
	hash   string
	active bool
}

func (f *fakeStore) credentials(context.Context, string) (user, string, bool, error) {
	return f.u, f.hash, f.active, f.err
}
func (f *fakeStore) session(context.Context, []byte) (user, error)               { return f.u, f.err }
func (f *fakeStore) create(context.Context, string, []byte, time.Duration) error { return f.err }
func (f *fakeStore) revoke(context.Context, []byte) error                        { return f.err }
func TestHTTP(t *testing.T) {
	h, _ := hashPassword("valid-password-123")
	f := &fakeStore{u: user{ID: "test", Login: "admin", Role: "admin"}, hash: h, active: true}
	origin, _ := canonicalOrigin("http://localhost:8082")
	a := &authService{store: f, config: authConfig{ttl: time.Hour, origins: map[string]bool{origin: true}}, limiter: newLimiter(), dummy: h}
	mux := http.NewServeMux()
	a.register(mux)
	handler := a.csrf(mux)
	call := func(method, path, body string, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := call("POST", "/auth/login", `{"login":"admin","password":"valid-password-123"}`, nil, ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	w := call("POST", "/auth/login", `{"login":"ADMIN","password":"valid-password-123"}`, nil, "http://localhost:8082")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.Path != "/" {
		t.Fatal(cookie)
	}
	if w := call("GET", "/auth/me", "", cookie, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	f.u.Role = "employee"
	if w := call("GET", "/admin/health", "", cookie, ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	f.err = pgx.ErrNoRows
	if w := call("GET", "/auth/me", "", cookie, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	f.err = errors.New("db down")
	if w := call("GET", "/auth/me", "", cookie, ""); w.Code != 503 {
		t.Fatal(w.Code)
	}
	w = call("POST", "/auth/logout", "", cookie, "http://localhost:8082")
	if w.Code != 503 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal(w.Code)
	}
	f.err = nil
	w = call("POST", "/auth/logout", "", nil, "http://localhost:8082")
	if w.Code != 204 || w.Body.Len() != 0 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal(w.Code)
	}
}
func TestConcurrentLimiter(t *testing.T) {
	l := newLimiter()
	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := []uint64{}
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if id, ok := l.reserve("ip", "unknown"); ok {
				mu.Lock()
				ids = append(ids, id)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(ids) != 5 {
		t.Fatal(len(ids))
	}
	l.release("unknown", ids[0])
	if _, ok := l.reserve("ip", "unknown"); !ok {
		t.Fatal("own reservation not released")
	}
	if _, ok := l.reserve("ip", "unknown"); ok {
		t.Fatal("other reservations reset")
	}
}
func TestOrigins(t *testing.T) {
	a, _ := canonicalOrigin("HTTP://LOCALHOST:80")
	b, _ := canonicalOrigin("http://localhost")
	if a != b {
		t.Fatal(a, b)
	}
	for _, s := range []string{"null", "http://host/path", "http://user@host", "https://host?x"} {
		if _, e := canonicalOrigin(s); e == nil {
			t.Fatal(s)
		}
	}
}
func TestConfig(t *testing.T) {
	t.Setenv("AUTH_ALLOWED_ORIGINS", "")
	if _, e := loadAuthConfig(); e == nil {
		t.Fatal("empty origins")
	}
	t.Setenv("AUTH_ALLOWED_ORIGINS", "http://localhost:8082")
	t.Setenv("AUTH_SESSION_TTL", "12h")
	t.Setenv("AUTH_COOKIE_SECURE", "false")
	t.Setenv("AUTH_TRUSTED_PROXIES", "127.0.0.0/8")
	c, e := loadAuthConfig()
	if e != nil {
		t.Fatal(e)
	}
	a := authService{config: c}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:123"
	r.Header.Set("X-Forwarded-For", "198.51.100.5, 127.0.0.2")
	if a.clientIP(r) != "198.51.100.5" {
		t.Fatal(a.clientIP(r))
	}
	r.RemoteAddr = "192.0.2.1:123"
	if a.clientIP(r) != "192.0.2.1" {
		t.Fatal("untrusted forwarded header")
	}
}

func TestLimiterExpiryAndCapacity(t *testing.T) {
	l := newLimiter()
	for i := 0; i < 10000; i++ {
		l.ips[string(rune(i))] = []attempt{{1, time.Now()}}
	}
	if _, ok := l.reserve("new-ip", "login"); ok {
		t.Fatal("capacity bypass")
	}
	l.ips = map[string][]attempt{"old": {{1, time.Now().Add(-6 * time.Minute)}}}
	l.logins["login"] = []attempt{{1, time.Now().Add(-6 * time.Minute)}}
	if _, ok := l.reserve("new-ip", "login"); !ok {
		t.Fatal("expiry failed")
	}
}

type failingCreateStore struct{ fakeStore }

func (f *failingCreateStore) create(context.Context, string, []byte, time.Duration) error {
	return errors.New("database unavailable")
}
func TestCreateSessionFailure(t *testing.T) {
	h, _ := hashPassword("valid-password-123")
	f := &failingCreateStore{fakeStore{u: user{ID: "test"}, hash: h, active: true}}
	a := authService{store: f, limiter: newLimiter(), dummy: h}
	r := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"login":"admin","password":"valid-password-123"}`))
	w := httptest.NewRecorder()
	a.login(w, r)
	if w.Code != 503 || len(w.Result().Cookies()) != 0 {
		t.Fatal(w.Code, w.Header())
	}
}
