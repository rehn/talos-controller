// Package auth implements a minimal single-admin session login.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const cookieName = "tc_session"

// Auth validates credentials and issues signed session cookies.
type Auth struct {
	user       string
	password   string
	bcryptHash []byte
	secret     []byte
	ttl        time.Duration
	secure     bool

	mu       sync.Mutex
	failures map[string][]time.Time
}

// New creates an Auth.
func New(user, password, bcryptHash string, secret []byte, ttl time.Duration, secure bool) (*Auth, error) {
	if password == "" && bcryptHash == "" {
		return nil, errors.New("ADMIN_PASSWORD or ADMIN_PASSWORD_BCRYPT must be set")
	}

	return &Auth{
		user:       user,
		password:   password,
		bcryptHash: []byte(bcryptHash),
		secret:     secret,
		ttl:        ttl,
		secure:     secure,
		failures:   map[string][]time.Time{},
	}, nil
}

func (a *Auth) checkCredentials(user, password string) bool {
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(a.user)) == 1

	var passOK bool
	if len(a.bcryptHash) > 0 {
		passOK = bcrypt.CompareHashAndPassword(a.bcryptHash, []byte(password)) == nil
	} else {
		passOK = subtle.ConstantTimeCompare([]byte(password), []byte(a.password)) == 1
	}

	return userOK && passOK
}

// tooManyFailures implements a tiny per-IP lockout: 5 failures in 5 minutes.
func (a *Auth) tooManyFailures(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	cutoff := time.Now().Add(-5 * time.Minute)
	kept := a.failures[ip][:0]

	for _, t := range a.failures[ip] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}

	a.failures[ip] = kept

	return len(kept) >= 5
}

func (a *Auth) recordFailure(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.failures[ip] = append(a.failures[ip], time.Now())
}

func (a *Auth) sign(payload string) string {
	m := hmac.New(sha256.New, a.secret)
	m.Write([]byte(payload))

	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (a *Auth) issue(w http.ResponseWriter) {
	exp := time.Now().Add(a.ttl).Unix()
	payload := base64.RawURLEncoding.EncodeToString([]byte(a.user + "|" + strconv.FormatInt(exp, 10)))

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    payload + "." + a.sign(payload),
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Unix(exp, 0),
	})
}

// User returns the authenticated user for the request, or "".
func (a *Auth) User(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}

	payload, sig, ok := strings.Cut(c.Value, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(a.sign(payload))) {
		return ""
	}

	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}

	user, expStr, ok := strings.Cut(string(raw), "|")
	if !ok {
		return ""
	}

	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return ""
	}

	return user
}

// Middleware rejects unauthenticated API calls and enforces JSON bodies on
// state-changing requests (a cheap CSRF guard on top of SameSite=Strict).
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.User(r) == "" {
			writeErr(w, http.StatusUnauthorized, "not authenticated")

			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				writeErr(w, http.StatusUnsupportedMediaType, "content-type must be application/json")

				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// LoginHandler handles POST /api/auth/login.
func (a *Auth) LoginHandler(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if a.tooManyFailures(ip) {
		writeErr(w, http.StatusTooManyRequests, "too many failed attempts, try again in a few minutes")

		return
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")

		return
	}

	if !a.checkCredentials(body.Username, body.Password) {
		a.recordFailure(ip)
		time.Sleep(500 * time.Millisecond)
		writeErr(w, http.StatusUnauthorized, "invalid username or password")

		return
	}

	a.issue(w)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"user": a.user})
}

// LogoutHandler handles POST /api/auth/logout.
func (a *Auth) LogoutHandler(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

// MeHandler handles GET /api/auth/me.
func (a *Auth) MeHandler(w http.ResponseWriter, r *http.Request) {
	u := a.User(r)
	if u == "" {
		writeErr(w, http.StatusUnauthorized, "not authenticated")

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"user": u})
}

func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}

	return host
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
