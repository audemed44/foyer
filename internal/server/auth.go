package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	cookieName = "foyer_session"
	sessionTTL = 30 * 24 * time.Hour
)

// Auth guards edit mode with a single password (FOYER_PASSWORD). Sessions
// are stateless signed cookies, so they survive restarts; changing the
// password signs everyone out. Without a password, editing is disabled.
type Auth struct {
	password string
	key      []byte

	mu       sync.Mutex
	failures int
	lockout  time.Time
	now      func() time.Time
}

func NewAuth(password string) *Auth {
	key := sha256.Sum256([]byte("foyer-session:" + password))
	return &Auth{password: password, key: key[:], now: time.Now}
}

func (a *Auth) Enabled() bool { return a.password != "" }

func (a *Auth) sign(expiry int64) string {
	mac := hmac.New(sha256.New, a.key)
	mac.Write([]byte(strconv.FormatInt(expiry, 10)))
	return strconv.FormatInt(expiry, 10) + "." + hex.EncodeToString(mac.Sum(nil))
}

func (a *Auth) Valid(r *http.Request) bool {
	if !a.Enabled() {
		return false
	}
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	raw, _, ok := strings.Cut(c.Value, ".")
	if !ok {
		return false
	}
	expiry, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || a.now().Unix() > expiry {
		return false
	}
	return hmac.Equal([]byte(c.Value), []byte(a.sign(expiry)))
}

func (a *Auth) Require(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.Enabled() {
			writeError(w, http.StatusForbidden, "editing is disabled: set FOYER_PASSWORD to enable it")
			return
		}
		if !a.Valid(r) {
			writeError(w, http.StatusUnauthorized, "sign in to edit")
			return
		}
		next(w, r)
	}
}

func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	if !a.Enabled() {
		writeError(w, http.StatusForbidden, "editing is disabled: set FOYER_PASSWORD to enable it")
		return
	}
	a.mu.Lock()
	locked := a.now().Before(a.lockout)
	a.mu.Unlock()
	if locked {
		writeError(w, http.StatusTooManyRequests, "too many attempts, try again in a minute")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
	if subtle.ConstantTimeCompare([]byte(body.Password), []byte(a.password)) != 1 {
		a.mu.Lock()
		a.failures++
		if a.failures >= 5 {
			a.failures, a.lockout = 0, a.now().Add(time.Minute)
		}
		a.mu.Unlock()
		writeError(w, http.StatusUnauthorized, "wrong password")
		return
	}
	a.mu.Lock()
	a.failures = 0
	a.mu.Unlock()
	expiry := a.now().Add(sessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: a.sign(expiry.Unix()), Path: "/", Expires: expiry,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r),
	})
	w.WriteHeader(http.StatusNoContent)
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
