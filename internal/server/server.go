// Package server is the HTTP server: a JSON API under /api and the web app.
package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/datafluxor/datafluxor-seo/internal/auth"
	"github.com/datafluxor/datafluxor-seo/internal/config"
	"github.com/datafluxor/datafluxor-seo/internal/dfapi"
	"github.com/datafluxor/datafluxor-seo/internal/gsc"
	"github.com/datafluxor/datafluxor-seo/internal/ranks"
	"github.com/datafluxor/datafluxor-seo/internal/store"
)

//go:embed all:dist
var dist embed.FS

// SignupURL is where a user gets a DataFluxor key.
const SignupURL = "https://datafluxor.com/signup?utm_source=github&utm_medium=self-hosted&utm_campaign=datafluxor-seo"

// Server holds everything a request needs.
type Server struct {
	Cfg     config.Config
	Store   *store.Store
	Auth    *auth.Auth
	API     *dfapi.Client
	Ranks   *ranks.Tracker
	Google  *gsc.Client
	Log     *slog.Logger
	Version string
	// CrawlAllowPrivate lets audits read private network addresses.
	CrawlAllowPrivate bool

	auditsMu sync.Mutex
	audits   map[int64]context.CancelFunc
	bgCtx    context.Context
}

// Settings keys kept in the database.
const (
	settingAPIKey       = "api_key"
	settingGoogleID     = "google_client_id"
	settingGoogleSecret = "google_client_secret"
	settingGoogleToken  = "google_token"
)

// APIKey is the key in use: the environment's, else the one saved in Settings.
func (s *Server) APIKey(ctx context.Context) string {
	if s.Cfg.APIKey != "" {
		return s.Cfg.APIKey
	}
	k, _ := s.Store.Setting(ctx, settingAPIKey)
	return k
}

// GoogleClient is the Search Console client with the current OAuth settings.
func (s *Server) GoogleClient(ctx context.Context) *gsc.Client {
	id, secret := s.Cfg.GoogleID, s.Cfg.GoogleSecret
	if id == "" {
		id, _ = s.Store.Setting(ctx, settingGoogleID)
		secret, _ = s.Store.Setting(ctx, settingGoogleSecret)
	}
	g := *s.Google
	g.ClientID, g.ClientSecret = id, secret
	return &g
}

// Handler returns the full HTTP handler.
func (s *Server) Handler(ctx context.Context) http.Handler {
	s.bgCtx = ctx
	s.audits = map[int64]context.CancelFunc{}
	mux := http.NewServeMux()

	// Public.
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("POST /api/setup", s.setup)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	// Signed in.
	a := s.authed
	mux.Handle("GET /api/settings", a(s.getSettings))
	mux.Handle("PUT /api/settings", a(s.putSettings))
	mux.Handle("POST /api/password", a(s.changePassword))
	mux.Handle("GET /api/account", a(s.account))

	mux.Handle("GET /api/projects", a(s.listProjects))
	mux.Handle("POST /api/projects", a(s.createProject))
	mux.Handle("GET /api/projects/{id}", a(s.getProject))
	mux.Handle("PATCH /api/projects/{id}", a(s.updateProject))
	mux.Handle("DELETE /api/projects/{id}", a(s.deleteProject))
	mux.Handle("GET /api/projects/{id}/overview", a(s.overview))

	mux.Handle("GET /api/projects/{id}/audits", a(s.listAudits))
	mux.Handle("POST /api/projects/{id}/audits", a(s.startAudit))
	mux.Handle("GET /api/audits/{id}", a(s.getAudit))
	mux.Handle("POST /api/audits/{id}/stop", a(s.stopAudit))
	mux.Handle("GET /api/audits/{id}/issues", a(s.auditIssues))
	mux.Handle("GET /api/audits/{id}/pages", a(s.auditPages))
	mux.Handle("GET /api/audits/{id}/issues.csv", a(s.auditIssuesCSV))

	mux.Handle("GET /api/projects/{id}/keywords", a(s.listKeywords))
	mux.Handle("POST /api/projects/{id}/keywords", a(s.addKeywords))
	mux.Handle("DELETE /api/projects/{id}/keywords/{kid}", a(s.deleteKeyword))
	mux.Handle("POST /api/projects/{id}/check", a(s.checkAll))
	mux.Handle("GET /api/projects/{id}/keywords.csv", a(s.keywordsCSV))
	mux.Handle("POST /api/keywords/{kid}/check", a(s.checkKeyword))
	mux.Handle("GET /api/keywords/{kid}/history", a(s.keywordHistory))

	mux.Handle("GET /api/tools/markets", a(s.markets))
	mux.Handle("GET /api/tools/ai-engines", a(s.aiEngines))
	mux.Handle("POST /api/tools/{tool}", a(s.tool))

	mux.Handle("GET /api/google/connect", a(s.googleConnect))
	mux.Handle("GET /api/google/callback", a(s.googleCallback))
	mux.Handle("POST /api/google/disconnect", a(s.googleDisconnect))
	mux.Handle("GET /api/google/sites", a(s.googleSites))
	mux.Handle("GET /api/projects/{id}/search-console", a(s.searchConsole))

	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "no such API route")
	}))
	mux.Handle("/", s.static())
	return securityHeaders(mux)
}

// ---- helpers ----

type userKey struct{}

// authed requires a signed-in user and, for writes, the app's own request
// header: a cross-site form cannot set it, so it stops CSRF.
func (s *Server) authed(h func(http.ResponseWriter, *http.Request, store.User)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.Auth.User(r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "sign in first")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Requested-With") != "datafluxor-seo" {
			writeErr(w, http.StatusForbidden, "missing X-Requested-With header")
			return
		}
		h(w, r, u)
	})
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "same-origin")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: https:; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self' https://accounts.google.com")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			hd.Set("Cache-Control", "no-store")
		}
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Error      string `json:"error"`
	Code       string `json:"code,omitempty"`
	RetryAfter int    `json:"retry_after_seconds,omitempty"`
	SignupURL  string `json:"signup_url,omitempty"`
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

// writeAPIErr turns a DataFluxor or local error into an answer.
func (s *Server) writeAPIErr(w http.ResponseWriter, err error) {
	var e *dfapi.Error
	switch {
	case errors.Is(err, dfapi.ErrNoKey):
		writeJSON(w, http.StatusPaymentRequired, apiError{Error: err.Error(), Code: "NO_API_KEY", SignupURL: SignupURL})
	case errors.As(err, &e):
		status := e.Status
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		out := apiError{Error: e.Message, Code: e.Code, RetryAfter: e.RetryAfter}
		if e.Code == "INSUFFICIENT_CREDITS" || e.Code == "UNAUTHORIZED" {
			out.SignupURL = SignupURL
		}
		writeJSON(w, status, out)
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, gsc.ErrNotConnected):
		writeJSON(w, http.StatusConflict, apiError{Error: err.Error(), Code: "GOOGLE_NOT_CONNECTED"})
	default:
		s.Log.Error("request failed", "err", err)
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 256<<10))
	if err := dec.Decode(v); err != nil {
		return errors.New("invalid JSON body")
	}
	return nil
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

// ---- static web app ----

func (s *Server) static() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if f, err := sub.Open(path); err == nil {
				f.Close()
				if strings.HasPrefix(path, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		// Every other path is a page of the single-page app.
		index, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			http.Error(w, "web app not built: run `make web`", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(index)
	})
}

// ---- status, setup and sign-in ----

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.UserCount(r.Context())
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	out := map[string]any{"setup_needed": n == 0, "signed_in": false, "version": s.Version, "signup_url": SignupURL}
	if u, ok := s.Auth.User(r); ok {
		out["signed_in"] = true
		out["user"] = u
		out["has_api_key"] = s.APIKey(r.Context()) != ""
		out["google_configured"] = s.GoogleClient(r.Context()).Configured()
	}
	writeJSON(w, http.StatusOK, out)
}

type credentials struct {
	Code     string `json:"code"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if err := readJSON(r, &c); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := s.Auth.Setup(r.Context(), w, r, c.Code, c.Email, c.Password)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if err := readJSON(r, &c); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := s.Auth.Login(r.Context(), w, r, c.Email, c.Password)
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, auth.ErrTooMany) {
			status = http.StatusTooManyRequests
		}
		writeErr(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Requested-With") != "datafluxor-seo" {
		writeErr(w, http.StatusForbidden, "missing X-Requested-With header")
		return
	}
	s.Auth.Logout(r.Context(), w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, u store.User) {
	var body struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Auth.ChangePassword(r.Context(), w, u, body.Current, body.New); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- settings and account ----

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request, _ store.User) {
	ctx := r.Context()
	key := s.APIKey(ctx)
	suffix := ""
	if len(key) >= 4 {
		suffix = key[len(key)-4:]
	}
	get := func(k string) string { v, _ := s.Store.Setting(ctx, k); return v }
	g := s.GoogleClient(ctx)
	tok, _ := s.GoogleToken(ctx)
	writeJSON(w, http.StatusOK, map[string]any{
		"api_key_set":          key != "",
		"api_key_from_env":     s.Cfg.APIKey != "",
		"api_key_suffix":       suffix,
		"api_url":              s.Cfg.APIURL,
		"rank_depth":           s.Ranks.Depth(ctx),
		"rank_depths":          ranks.Depths,
		"rank_check_hour":      atoiDefault(get(ranks.SettingHour), 6),
		"monthly_credit_cap":   atoiDefault(get(ranks.SettingMonthlyCap), 0),
		"rank_schedule_paused": get(ranks.SettingPaused) == "1",
		"public_url":           s.Cfg.PublicURL,
		"google": map[string]any{
			"configured":   g.Configured(),
			"from_env":     s.Cfg.GoogleID != "",
			"client_id":    g.ClientID,
			"connected":    tok.RefreshToken != "",
			"redirect_url": s.Cfg.PublicURL + "/api/google/callback",
		},
		"signup_url": SignupURL,
	})
}

func atoiDefault(s string, d int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return d
	}
	return n
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request, _ store.User) {
	ctx := r.Context()
	var body struct {
		APIKey             *string `json:"api_key"`
		RankDepth          *int    `json:"rank_depth"`
		RankCheckHour      *int    `json:"rank_check_hour"`
		MonthlyCreditCap   *int    `json:"monthly_credit_cap"`
		RankSchedulePaused *bool   `json:"rank_schedule_paused"`
		GoogleClientID     *string `json:"google_client_id"`
		GoogleClientSecret *string `json:"google_client_secret"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.APIKey != nil {
		k := strings.TrimSpace(*body.APIKey)
		if k != "" && (len(k) < 20 || len(k) > 200 || strings.ContainsAny(k, " \t\r\n")) {
			writeErr(w, http.StatusBadRequest, "that does not look like a DataFluxor API key (df_live_…)")
			return
		}
		if k != "" {
			// Check the key before saving it, so a typo is caught at once.
			probe := dfapi.New(s.Cfg.APIURL, func(context.Context) string { return k })
			if _, err := probe.Get(ctx, "/api/v1/account"); err != nil {
				var e *dfapi.Error
				if errors.As(err, &e) && (e.Status == 401 || e.Status == 403) {
					writeErr(w, http.StatusBadRequest, "DataFluxor did not accept that key: "+e.Message)
					return
				}
				writeErr(w, http.StatusBadGateway, "could not reach DataFluxor to check the key: "+err.Error())
				return
			}
		}
		if err := s.Store.SetSetting(ctx, settingAPIKey, k); err != nil {
			s.writeAPIErr(w, err)
			return
		}
	}
	if body.RankDepth != nil {
		ok := false
		for _, d := range ranks.Depths {
			ok = ok || d == *body.RankDepth
		}
		if !ok {
			writeErr(w, http.StatusBadRequest, "rank depth must be one of 10, 20, 30, 50, 100")
			return
		}
		_ = s.Store.SetSetting(ctx, ranks.SettingDepth, strconv.Itoa(*body.RankDepth))
	}
	if body.RankCheckHour != nil {
		if *body.RankCheckHour < 0 || *body.RankCheckHour > 23 {
			writeErr(w, http.StatusBadRequest, "check hour must be 0-23 (UTC)")
			return
		}
		_ = s.Store.SetSetting(ctx, ranks.SettingHour, strconv.Itoa(*body.RankCheckHour))
	}
	if body.MonthlyCreditCap != nil {
		if *body.MonthlyCreditCap < 0 {
			writeErr(w, http.StatusBadRequest, "the cap cannot be negative")
			return
		}
		v := ""
		if *body.MonthlyCreditCap > 0 {
			v = strconv.Itoa(*body.MonthlyCreditCap)
		}
		_ = s.Store.SetSetting(ctx, ranks.SettingMonthlyCap, v)
	}
	if body.RankSchedulePaused != nil {
		v := ""
		if *body.RankSchedulePaused {
			v = "1"
		}
		_ = s.Store.SetSetting(ctx, ranks.SettingPaused, v)
	}
	if body.GoogleClientID != nil {
		_ = s.Store.SetSetting(ctx, settingGoogleID, strings.TrimSpace(*body.GoogleClientID))
	}
	if body.GoogleClientSecret != nil && *body.GoogleClientSecret != "" {
		_ = s.Store.SetSetting(ctx, settingGoogleSecret, strings.TrimSpace(*body.GoogleClientSecret))
	}
	s.getSettings(w, r, store.User{})
}

func (s *Server) account(w http.ResponseWriter, r *http.Request, _ store.User) {
	ctx := r.Context()
	now := time.Now().UTC()
	month, _ := s.Store.SpendSince(ctx, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC))
	recent, _ := s.Store.RecentSpend(ctx, 50)
	out := map[string]any{"spent_this_month": month, "recent_spend": recent, "has_api_key": s.API.HasKey(ctx)}
	if s.API.HasKey(ctx) {
		res, err := s.API.Get(ctx, "/api/v1/account")
		if err != nil {
			var e *dfapi.Error
			if errors.As(err, &e) {
				out["account_error"] = e.Message
			} else {
				out["account_error"] = err.Error()
			}
		} else {
			out["account"] = res.Data
		}
	}
	writeJSON(w, http.StatusOK, out)
}
