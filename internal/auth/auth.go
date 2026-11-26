// Package auth signs the owner in: one account, created at first run with a
// setup code printed in the server log, so nobody who reaches a fresh
// install before its owner can claim it.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/datafluxor/datafluxor-seo/internal/store"
)

// CookieName is the session cookie.
const CookieName = "dfseo_session"

// SessionTTL is how long a sign-in lasts.
const SessionTTL = 30 * 24 * time.Hour

// MinPassword is the shortest password accepted.
const MinPassword = 10

// Auth handles setup, sign-in and sessions.
type Auth struct {
	Store        *store.Store
	CookieSecure bool
	// SetupCode must be entered to create the first account.
	SetupCode string

	limiter limiter
}

// NewSetupCode returns a random code to print in the log.
func NewSetupCode() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	s := strings.ToUpper(hex.EncodeToString(b))
	return s[:4] + "-" + s[4:8] + "-" + s[8:]
}

// HashPassword hashes a password with bcrypt.
func HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	return string(h), err
}

var (
	dummyOnce sync.Once
	dummy     []byte
)

// dummyHash is a real bcrypt hash compared against when the email is unknown.
func dummyHash() []byte {
	dummyOnce.Do(func() { dummy, _ = bcrypt.GenerateFromPassword([]byte("not-a-password"), 12) })
	return dummy
}

func hashToken(t string) string {
	s := sha256.Sum256([]byte(t))
	return hex.EncodeToString(s[:])
}

// Errors shown to the user.
var (
	ErrBadLogin = errors.New("wrong email or password")
	ErrTooMany  = errors.New("too many attempts: wait 15 minutes")
	ErrBadSetup = errors.New("wrong setup code: it is printed in the server log")
	ErrWeakPass = errors.New("use a password of at least 10 characters")
	ErrBadEmail = errors.New("enter a valid email address")
)

// Setup creates the owner account and signs it in.
func (a *Auth) Setup(ctx context.Context, w http.ResponseWriter, r *http.Request, code, email, pw string) (store.User, error) {
	if !a.limiter.allow(clientIP(r)) {
		return store.User{}, ErrTooMany
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if a.SetupCode == "" || subtle.ConstantTimeCompare([]byte(code), []byte(a.SetupCode)) != 1 {
		a.limiter.fail(clientIP(r))
		return store.User{}, ErrBadSetup
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") || len(email) > 200 {
		return store.User{}, ErrBadEmail
	}
	if len(pw) < MinPassword {
		return store.User{}, ErrWeakPass
	}
	hash, err := HashPassword(pw)
	if err != nil {
		return store.User{}, err
	}
	u, err := a.Store.CreateFirstUser(ctx, email, hash)
	if err != nil {
		return u, err
	}
	a.SetupCode = ""
	return u, a.startSession(ctx, w, u)
}

// Login checks a password and starts a session.
func (a *Auth) Login(ctx context.Context, w http.ResponseWriter, r *http.Request, email, pw string) (store.User, error) {
	ip := clientIP(r)
	if !a.limiter.allow(ip) {
		return store.User{}, ErrTooMany
	}
	u, err := a.Store.UserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		// Spend the same time as a real check so timing does not reveal accounts.
		_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(pw))
		a.limiter.fail(ip)
		return store.User{}, ErrBadLogin
	}
	if bcrypt.CompareHashAndPassword([]byte(u.Hash()), []byte(pw)) != nil {
		a.limiter.fail(ip)
		return store.User{}, ErrBadLogin
	}
	a.limiter.reset(ip)
	return u, a.startSession(ctx, w, u)
}

// ChangePassword replaces the password after checking the current one, and
// signs every other session out.
func (a *Auth) ChangePassword(ctx context.Context, w http.ResponseWriter, u store.User, current, next string) error {
	if bcrypt.CompareHashAndPassword([]byte(u.Hash()), []byte(current)) != nil {
		return ErrBadLogin
	}
	if len(next) < MinPassword {
		return ErrWeakPass
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	if err := a.Store.SetPassword(ctx, u.ID, hash); err != nil {
		return err
	}
	return a.startSession(ctx, w, u)
}

func (a *Auth) startSession(ctx context.Context, w http.ResponseWriter, u store.User) error {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	tok := hex.EncodeToString(b)
	if err := a.Store.CreateSession(ctx, hashToken(tok), u.ID, SessionTTL); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: tok, Path: "/", HttpOnly: true, Secure: a.CookieSecure,
		SameSite: http.SameSiteLaxMode, MaxAge: int(SessionTTL.Seconds()),
	})
	return nil
}

// Logout ends the current session.
func (a *Auth) Logout(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		_ = a.Store.DeleteSession(ctx, hashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: a.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

// User returns the signed-in user, if any.
func (a *Auth) User(r *http.Request) (store.User, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil || len(c.Value) != 64 {
		return store.User{}, false
	}
	u, err := a.Store.SessionUser(r.Context(), hashToken(c.Value))
	return u, err == nil
}

// clientIP is the connecting address. X-Forwarded-For is not trusted: a
// client could set it to dodge the attempt limit.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limiter allows 10 failed attempts per address per 15 minutes.
type limiter struct {
	mu    sync.Mutex
	fails map[string][]time.Time
}

func (l *limiter) recent(ip string) []time.Time {
	cut := time.Now().Add(-15 * time.Minute)
	kept := l.fails[ip][:0]
	for _, t := range l.fails[ip] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	l.fails[ip] = kept
	return kept
}

func (l *limiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fails == nil {
		l.fails = map[string][]time.Time{}
	}
	return len(l.recent(ip)) < 10
}

func (l *limiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fails == nil {
		l.fails = map[string][]time.Time{}
	}
	l.fails[ip] = append(l.recent(ip), time.Now())
}

func (l *limiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, ip)
}
