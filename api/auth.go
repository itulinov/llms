package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type authService struct {
	store   authStore
	config  authConfig
	limiter *loginLimiter
	dummy   string
}
type userKey struct{}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if status != 204 {
		_ = json.NewEncoder(w).Encode(v)
	}
}
func fail(w http.ResponseWriter, status int) {
	reply(w, status, map[string]string{"error": http.StatusText(status)})
}
func (a *authService) cookie(w http.ResponseWriter, value string, clear bool) {
	c := &http.Cookie{Name: "lms_session", Value: value, Path: "/", HttpOnly: true, Secure: a.config.secure, SameSite: http.SameSiteLaxMode}
	if clear {
		c.MaxAge = -1
		c.Expires = time.Unix(1, 0)
	}
	http.SetCookie(w, c)
}
func token(r *http.Request) ([]byte, error) {
	c, e := r.Cookie("lms_session")
	if e != nil {
		return nil, e
	}
	b, e := base64.RawURLEncoding.Strict().DecodeString(c.Value)
	if e != nil || len(b) != 32 {
		return nil, errors.New("invalid token")
	}
	return b, nil
}
func (a *authService) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		t, e := token(r)
		if e != nil {
			fail(w, 401)
			return
		}
		u, e := a.store.session(r.Context(), t)
		if errors.Is(e, pgx.ErrNoRows) {
			fail(w, 401)
			return
		}
		if e != nil {
			fail(w, 503)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, u)))
	})
}
func RequireRole(role string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := r.Context().Value(userKey{}).(user)
		if !ok {
			fail(w, 401)
			return
		}
		if u.Role != role {
			fail(w, 403)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *authService) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			values := r.Header.Values("Origin")
			if len(values) != 1 {
				fail(w, 403)
				return
			}
			o, e := canonicalOrigin(values[0])
			if e != nil || !a.config.origins[o] {
				fail(w, 403)
				return
			}
			body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
			if e != nil {
				fail(w, 413)
				return
			}
			if len(body) > 0 {
				mt, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if e != nil || mt != "application/json" {
					fail(w, 415)
					return
				}
				if !utf8.Valid(body) {
					fail(w, 400)
					return
				}
			}
			r.Body = io.NopCloser(strings.NewReader(string(body)))
		}
		next.ServeHTTP(w, r)
	})
}
func (a *authService) clientIP(r *http.Request) string {
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		return r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if !a.config.trusted(ip) {
		return host
	}
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	parsed := make([]net.IP, len(parts))
	for i, p := range parts {
		parsed[i] = net.ParseIP(strings.TrimSpace(p))
		if parsed[i] == nil {
			return host
		}
	}
	for i := len(parsed) - 1; i >= 0; i-- {
		ip = parsed[i]
		if !a.config.trusted(ip) {
			return ip.String()
		}
	}
	return ip.String()
}
func (a *authService) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	d := json.NewDecoder(r.Body)
	if d.Decode(&input) != nil {
		fail(w, 400)
		return
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		fail(w, 400)
		return
	}
	login, e := normalizeLogin(input.Login)
	if e != nil {
		login = strings.ToLower(strings.TrimSpace(input.Login))
	}
	id, ok := a.limiter.reserve(a.clientIP(r), login)
	if !ok {
		w.Header().Set("Retry-After", "300")
		fail(w, 429)
		return
	}
	if e != nil || !validPassword(input.Password) {
		fail(w, 401)
		return
	}
	u, hash, active, e := a.store.credentials(r.Context(), login)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		a.limiter.release(login, id)
		fail(w, 503)
		return
	}
	if errors.Is(e, pgx.ErrNoRows) || !active {
		_ = checkPassword(a.dummy, input.Password)
		fail(w, 401)
		return
	}
	if !checkPassword(hash, input.Password) {
		fail(w, 401)
		return
	}
	a.limiter.release(login, id)
	t := make([]byte, 32)
	if _, e = rand.Read(t); e != nil {
		fail(w, 503)
		return
	}
	if e = a.store.create(context.WithValue(r.Context(), verifiedHashKey{}, hash), u.ID, t, a.config.ttl); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			fail(w, 401)
			return
		}
		fail(w, 503)
		return
	}
	a.cookie(w, base64.RawURLEncoding.EncodeToString(t), false)
	reply(w, 200, u)
}
func (a *authService) logout(w http.ResponseWriter, r *http.Request) {
	a.cookie(w, "", true)
	if t, e := token(r); e == nil {
		if a.store.revoke(r.Context(), t) != nil {
			fail(w, 503)
			return
		}
	}
	reply(w, 204, nil)
}
func (a *authService) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth/login", a.login)
	mux.HandleFunc("POST /auth/logout", a.logout)
	mux.Handle("GET /auth/me", a.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reply(w, 200, r.Context().Value(userKey{})) })))
	mux.Handle("GET /admin/health", a.RequireAuth(RequireRole("admin", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if s, ok := a.store.(pgAuthStore); ok {
			if s.pool.Ping(ctx) != nil {
				fail(w, 503)
				return
			}
		}
		reply(w, 200, map[string]string{"status": "ok", "database": "connected"})
	}))))
}
