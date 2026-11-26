package server

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/datafluxor/datafluxor-seo/internal/crawler"
	"github.com/datafluxor/datafluxor-seo/internal/dfapi"
	"github.com/datafluxor/datafluxor-seo/internal/gsc"
	"github.com/datafluxor/datafluxor-seo/internal/ranks"
	"github.com/datafluxor/datafluxor-seo/internal/store"
)

// ---- projects ----

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request, _ store.User) {
	ps, err := s.Store.Projects(r.Context())
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func cleanDomain(raw string) (string, error) {
	d := ranks.NormalizeDomain(raw)
	if d == "" || !strings.Contains(d, ".") || strings.ContainsAny(d, " /?#@") || len(d) > 253 {
		return "", errors.New("enter a website domain, e.g. example.com")
	}
	return d, nil
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request, _ store.User) {
	var body struct{ Name, Domain string }
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	d, err := cleanDomain(body.Domain)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = d
	}
	p, err := s.Store.CreateProject(r.Context(), truncate(name, 100), d)
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func (s *Server) project(w http.ResponseWriter, r *http.Request) (store.Project, bool) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad project id")
		return store.Project{}, false
	}
	p, err := s.Store.Project(r.Context(), id)
	if err != nil {
		s.writeAPIErr(w, err)
		return p, false
	}
	return p, true
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request, _ store.User) {
	if p, ok := s.project(w, r); ok {
		writeJSON(w, http.StatusOK, p)
	}
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request, _ store.User) {
	p, ok := s.project(w, r)
	if !ok {
		return
	}
	var body struct {
		Name        *string `json:"name"`
		Domain      *string `json:"domain"`
		GSCProperty *string `json:"gsc_property"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Name != nil && strings.TrimSpace(*body.Name) != "" {
		p.Name = truncate(strings.TrimSpace(*body.Name), 100)
	}
	if body.Domain != nil {
		d, err := cleanDomain(*body.Domain)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		p.Domain = d
	}
	if body.GSCProperty != nil {
		p.GSCProperty = strings.TrimSpace(*body.GSCProperty)
	}
	if err := s.Store.UpdateProject(r.Context(), p); err != nil {
		s.writeAPIErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request, _ store.User) {
	p, ok := s.project(w, r)
	if !ok {
		return
	}
	s.auditsMu.Lock()
	audits, _ := s.Store.Audits(r.Context(), p.ID, 5)
	for _, a := range audits {
		if cancel := s.audits[a.ID]; cancel != nil {
			cancel()
		}
	}
	s.auditsMu.Unlock()
	if err := s.Store.DeleteProject(r.Context(), p.ID); err != nil {
		s.writeAPIErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// overview is the project dashboard: audit health and keyword movement.
func (s *Server) overview(w http.ResponseWriter, r *http.Request, _ store.User) {
	p, ok := s.project(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	audits, err := s.Store.Audits(ctx, p.ID, 10)
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	kws, err := s.Store.Keywords(ctx, p.ID)
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	type summary struct {
		Tracked  int      `json:"tracked"`
		Checked  int      `json:"checked"`
		Top3     int      `json:"top3"`
		Top10    int      `json:"top10"`
		Ranking  int      `json:"ranking"`
		Average  *float64 `json:"average_position"`
		Improved int      `json:"improved"`
		Declined int      `json:"declined"`
	}
	sm := summary{Tracked: len(kws)}
	total := 0
	for _, k := range kws {
		if k.Latest == nil {
			continue
		}
		sm.Checked++
		if k.Latest.Position != nil {
			pos := *k.Latest.Position
			sm.Ranking++
			total += pos
			if pos <= 3 {
				sm.Top3++
			}
			if pos <= 10 {
				sm.Top10++
			}
		}
		if mv := movement(k); mv > 0 {
			sm.Improved++
		} else if mv < 0 {
			sm.Declined++
		}
	}
	if sm.Ranking > 0 {
		avg := float64(total) / float64(sm.Ranking)
		sm.Average = &avg
	}
	tok, _ := s.GoogleToken(ctx)
	writeJSON(w, http.StatusOK, map[string]any{
		"project": p, "audits": audits, "keywords": sm,
		"google_connected": tok.RefreshToken != "", "has_api_key": s.API.HasKey(ctx),
	})
}

// movement is positive when a keyword moved up since its previous reading.
// Entering the results counts as up, dropping out as down.
func movement(k store.Keyword) int {
	if k.Latest == nil || k.Previous == nil {
		return 0
	}
	a, b := k.Previous.Position, k.Latest.Position
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	}
	return *a - *b
}

// ---- site audits ----

func (s *Server) listAudits(w http.ResponseWriter, r *http.Request, _ store.User) {
	p, ok := s.project(w, r)
	if !ok {
		return
	}
	as, err := s.Store.Audits(r.Context(), p.ID, 50)
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, as)
}

func (s *Server) startAudit(w http.ResponseWriter, r *http.Request, _ store.User) {
	p, ok := s.project(w, r)
	if !ok {
		return
	}
	var body struct {
		MaxPages int    `json:"max_pages"`
		StartURL string `json:"start_url"`
	}
	_ = readJSON(r, &body)
	if body.MaxPages <= 0 {
		body.MaxPages = 500
	}
	if body.MaxPages > s.Cfg.CrawlMaxPages {
		body.MaxPages = s.Cfg.CrawlMaxPages
	}
	start := "https://" + p.Domain + "/"
	if body.StartURL != "" {
		u, err := url.Parse(body.StartURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !ranks.SameSite(u.Hostname(), p.Domain) {
			writeErr(w, http.StatusBadRequest, "the start address must be on "+p.Domain)
			return
		}
		start = u.String()
	}
	ctx := r.Context()
	running, _ := s.Store.Audits(ctx, p.ID, 1)
	if len(running) > 0 && running[0].Status == "running" {
		writeErr(w, http.StatusConflict, "an audit of this project is already running")
		return
	}
	a, err := s.Store.StartAudit(ctx, p.ID, start, body.MaxPages)
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	actx, cancel := context.WithTimeout(s.bgCtx, 2*time.Hour)
	s.auditsMu.Lock()
	s.audits[a.ID] = cancel
	s.auditsMu.Unlock()
	go s.runAudit(actx, cancel, a)
	writeJSON(w, http.StatusAccepted, a)
}

func (s *Server) runAudit(ctx context.Context, cancel context.CancelFunc, a store.Audit) {
	defer func() {
		cancel()
		s.auditsMu.Lock()
		delete(s.audits, a.ID)
		s.auditsMu.Unlock()
	}()
	last := time.Now()
	res, err := crawler.Crawl(ctx, a.StartURL, crawler.Options{
		MaxPages:     a.MaxPages,
		AllowPrivate: s.CrawlAllowPrivate,
		Progress: func(n int) {
			if time.Since(last) > 2*time.Second {
				last = time.Now()
				_ = s.Store.AuditProgress(context.Background(), a.ID, n)
			}
		},
	})
	status, msg := "done", ""
	switch {
	case errors.Is(err, context.Canceled):
		status, msg = "stopped", "stopped before the end"
	case errors.Is(err, context.DeadlineExceeded):
		status, msg = "stopped", "stopped after 2 hours"
	case err != nil:
		status, msg = "failed", err.Error()
	case len(res.Pages) == 0:
		status, msg = "failed", "no page could be crawled (robots.txt may block it)"
	}
	if err := s.Store.FinishAudit(context.Background(), a.ID, status, msg, res.Pages, res.Issues); err != nil {
		s.Log.Error("save audit", "audit", a.ID, "err", err)
	}
	s.Log.Info("audit finished", "audit", a.ID, "status", status, "pages", len(res.Pages), "issues", len(res.Issues))
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) (store.Audit, bool) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad audit id")
		return store.Audit{}, false
	}
	a, err := s.Store.Audit(r.Context(), id)
	if err != nil {
		s.writeAPIErr(w, err)
		return a, false
	}
	return a, true
}

func (s *Server) getAudit(w http.ResponseWriter, r *http.Request, _ store.User) {
	if a, ok := s.audit(w, r); ok {
		writeJSON(w, http.StatusOK, a)
	}
}

func (s *Server) stopAudit(w http.ResponseWriter, r *http.Request, _ store.User) {
	a, ok := s.audit(w, r)
	if !ok {
		return
	}
	s.auditsMu.Lock()
	cancel := s.audits[a.ID]
	s.auditsMu.Unlock()
	if cancel == nil {
		writeErr(w, http.StatusConflict, "this audit is not running")
		return
	}
	cancel()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) auditIssues(w http.ResponseWriter, r *http.Request, _ store.User) {
	a, ok := s.audit(w, r)
	if !ok {
		return
	}
	is, err := s.Store.AuditIssues(r.Context(), a.ID)
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, is)
}

func (s *Server) auditPages(w http.ResponseWriter, r *http.Request, _ store.User) {
	a, ok := s.audit(w, r)
	if !ok {
		return
	}
	ps, err := s.Store.AuditPages(r.Context(), a.ID)
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (s *Server) auditIssuesCSV(w http.ResponseWriter, r *http.Request, _ store.User) {
	a, ok := s.audit(w, r)
	if !ok {
		return
	}
	is, err := s.Store.AuditIssues(r.Context(), a.ID)
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="audit-%d-issues.csv"`, a.ID))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"severity", "issue", "page", "detail"})
	for _, i := range is {
		_ = cw.Write([]string{i.Severity, i.Code, csvSafe(i.PageURL), csvSafe(i.Detail)})
	}
	cw.Flush()
}

// csvSafe stops a spreadsheet treating a cell as a formula.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// ---- keywords and rank checks ----

func (s *Server) listKeywords(w http.ResponseWriter, r *http.Request, _ store.User) {
	p, ok := s.project(w, r)
	if !ok {
		return
	}
	ks, err := s.Store.Keywords(r.Context(), p.ID)
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ks)
}

func (s *Server) addKeywords(w http.ResponseWriter, r *http.Request, _ store.User) {
	p, ok := s.project(w, r)
	if !ok {
		return
	}
	var body struct {
		Keywords []string `json:"keywords"`
		Country  string   `json:"country"`
		Language string   `json:"language"`
		Device   string   `json:"device"`
		Tags     string   `json:"tags"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	country := strings.ToUpper(strings.TrimSpace(body.Country))
	if len(country) != 2 {
		writeErr(w, http.StatusBadRequest, "choose a country (two letters, e.g. US)")
		return
	}
	lang := strings.ToLower(strings.TrimSpace(body.Language))
	if lang == "" {
		lang = "en"
	}
	device := body.Device
	if device != "mobile" {
		device = "desktop"
	}
	if len(body.Keywords) > 500 {
		writeErr(w, http.StatusBadRequest, "add at most 500 keywords at a time")
		return
	}
	added, existing := []store.Keyword{}, 0
	for _, raw := range body.Keywords {
		kw := strings.Join(strings.Fields(strings.ToLower(raw)), " ")
		if kw == "" || len(kw) > 200 {
			continue
		}
		k, isNew, err := s.Store.AddKeyword(r.Context(), store.Keyword{ProjectID: p.ID, Keyword: kw, Country: country, Language: lang, Device: device, Tags: truncate(body.Tags, 100)})
		if err != nil {
			s.writeAPIErr(w, err)
			return
		}
		if isNew {
			added = append(added, k)
		} else {
			existing++
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"added": added, "already_tracked": existing})
}

func (s *Server) deleteKeyword(w http.ResponseWriter, r *http.Request, _ store.User) {
	p, ok := s.project(w, r)
	if !ok {
		return
	}
	kid, ok := pathID(r, "kid")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad keyword id")
		return
	}
	if err := s.Store.DeleteKeyword(r.Context(), p.ID, kid); err != nil {
		s.writeAPIErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) checkKeyword(w http.ResponseWriter, r *http.Request, _ store.User) {
	kid, ok := pathID(r, "kid")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad keyword id")
		return
	}
	k, err := s.Store.KeywordByID(r.Context(), kid)
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	c, err := s.Ranks.Check(r.Context(), k, 0)
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// checkAll checks every keyword of a project now, in the background.
func (s *Server) checkAll(w http.ResponseWriter, r *http.Request, _ store.User) {
	p, ok := s.project(w, r)
	if !ok {
		return
	}
	if !s.API.HasKey(r.Context()) {
		s.writeAPIErr(w, dfapi.ErrNoKey)
		return
	}
	ks, err := s.Store.Keywords(r.Context(), p.ID)
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	go func() {
		for i, k := range ks {
			if s.bgCtx.Err() != nil {
				return
			}
			if _, err := s.Ranks.Check(s.bgCtx, k, 0); err != nil {
				s.Log.Warn("rank check failed", "keyword", k.Keyword, "err", err)
				var e *dfapi.Error
				if errors.Is(err, dfapi.ErrNoKey) || (errors.As(err, &e) && (e.Code == "INSUFFICIENT_CREDITS" || e.Code == "UNAUTHORIZED" || e.Code == "DAILY_LIVE_LIMIT")) {
					return
				}
			}
			if i < len(ks)-1 {
				time.Sleep(7 * time.Second) // inside the API's per-minute limit
			}
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": len(ks)})
}

func (s *Server) keywordHistory(w http.ResponseWriter, r *http.Request, _ store.User) {
	kid, ok := pathID(r, "kid")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad keyword id")
		return
	}
	k, err := s.Store.KeywordByID(r.Context(), kid)
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	checks, err := s.Store.RankChecks(r.Context(), kid, 365, false)
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	latest, _ := s.Store.RankChecks(r.Context(), kid, 1, true)
	out := map[string]any{"keyword": k, "history": checks}
	if len(latest) > 0 {
		out["latest_page"] = latest[0]
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) keywordsCSV(w http.ResponseWriter, r *http.Request, _ store.User) {
	p, ok := s.project(w, r)
	if !ok {
		return
	}
	ks, err := s.Store.Keywords(r.Context(), p.ID)
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-rankings.csv"`, p.Domain))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"keyword", "country", "language", "device", "position", "previous", "url", "checked_at"})
	pos := func(c *store.RankCheck) string {
		if c == nil {
			return ""
		}
		if c.Position == nil {
			return "not in results"
		}
		return strconv.Itoa(*c.Position)
	}
	for _, k := range ks {
		row := []string{csvSafe(k.Keyword), k.Country, k.Language, k.Device, pos(k.Latest), pos(k.Previous), "", ""}
		if k.Latest != nil {
			row[6], row[7] = csvSafe(k.Latest.URL), k.Latest.CheckedAt.Format(time.RFC3339)
		}
		_ = cw.Write(row)
	}
	cw.Flush()
}

// ---- tools that call DataFluxor ----

// tools maps the app's tool names to DataFluxor endpoints. The body the
// browser sends is passed through; DataFluxor validates it.
var tools = map[string]string{
	"serp":             "/api/v1/scrape/serp",
	"keyword-research": "/api/v1/keyword-research",
	"keyword-metrics":  "/api/v1/keyword-metrics",
	"ai-visibility":    "/api/v1/scrape/ai-visibility",
	"domain-overview":  "/api/v1/domain-overview",
	"backlinks":        "/api/v1/backlinks",
	"suggest":          "/api/v1/scrape/suggest",
	"content-brief":    "/api/v1/scrape/content-brief",
}

func (s *Server) tool(w http.ResponseWriter, r *http.Request, _ store.User) {
	name := r.PathValue("tool")
	path, ok := tools[name]
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown tool")
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.API.Post(r.Context(), path, body)
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	note := ""
	for _, k := range []string{"keyword", "seed", "target", "query", "brand_name", "keywords"} {
		switch v := body[k].(type) {
		case string:
			note = v
		case []any:
			parts := []string{}
			for _, x := range v {
				if s, ok := x.(string); ok {
					parts = append(parts, s)
				}
			}
			note = strings.Join(parts, ", ")
		}
		if note != "" {
			note = truncate(note, 80)
			break
		}
	}
	_ = s.Store.RecordSpend(r.Context(), name, res.CreditsUsed, note)
	writeJSON(w, http.StatusOK, map[string]any{"data": res.Data, "credits_used": res.CreditsUsed, "cache_hit": res.CacheHit, "run_id": res.RunID})
}

func (s *Server) markets(w http.ResponseWriter, r *http.Request, _ store.User) {
	s.passGet(w, r, "/api/v1/serp/markets")
}

func (s *Server) aiEngines(w http.ResponseWriter, r *http.Request, _ store.User) {
	s.passGet(w, r, "/api/v1/scrape/ai-visibility/engines")
}

func (s *Server) passGet(w http.ResponseWriter, r *http.Request, path string) {
	res, err := s.API.Get(r.Context(), path)
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": res.Data})
}

// ---- Google Search Console ----

const googleStateCookie = "dfseo_google_state"

func (s *Server) GoogleToken(ctx context.Context) (gsc.Token, error) {
	var t gsc.Token
	raw, err := s.Store.Setting(ctx, settingGoogleToken)
	if err != nil || raw == "" {
		return t, err
	}
	err = json.Unmarshal([]byte(raw), &t)
	return t, err
}

func (s *Server) SaveGoogleToken(ctx context.Context, t gsc.Token) error {
	b, _ := json.Marshal(t)
	return s.Store.SetSetting(ctx, settingGoogleToken, string(b))
}

func (s *Server) googleConnect(w http.ResponseWriter, r *http.Request, _ store.User) {
	g := s.GoogleClient(r.Context())
	if !g.Configured() {
		writeErr(w, http.StatusConflict, "add your Google OAuth client ID and secret in Settings first")
		return
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	state := hex.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{Name: googleStateCookie, Value: state, Path: "/api/google", HttpOnly: true, Secure: s.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	http.Redirect(w, r, g.AuthURL(state), http.StatusFound)
}

func (s *Server) googleCallback(w http.ResponseWriter, r *http.Request, _ store.User) {
	c, err := r.Cookie(googleStateCookie)
	if err != nil || c.Value == "" || r.URL.Query().Get("state") != c.Value {
		http.Redirect(w, r, "/settings?google=state", http.StatusFound)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: googleStateCookie, Value: "", Path: "/api/google", MaxAge: -1})
	if e := r.URL.Query().Get("error"); e != "" {
		http.Redirect(w, r, "/settings?google="+url.QueryEscape(e), http.StatusFound)
		return
	}
	if err := s.GoogleClient(r.Context()).Exchange(r.Context(), r.URL.Query().Get("code")); err != nil {
		s.Log.Warn("google sign-in failed", "err", err)
		http.Redirect(w, r, "/settings?google=failed", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/settings?google=connected", http.StatusFound)
}

func (s *Server) googleDisconnect(w http.ResponseWriter, r *http.Request, _ store.User) {
	_ = s.Store.SetSetting(r.Context(), settingGoogleToken, "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) googleSites(w http.ResponseWriter, r *http.Request, _ store.User) {
	sites, err := s.GoogleClient(r.Context()).Sites(r.Context())
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sites)
}

func (s *Server) searchConsole(w http.ResponseWriter, r *http.Request, _ store.User) {
	p, ok := s.project(w, r)
	if !ok {
		return
	}
	if p.GSCProperty == "" {
		writeJSON(w, http.StatusConflict, apiError{Error: "choose this project's Search Console property first", Code: "NO_PROPERTY"})
		return
	}
	q := r.URL.Query()
	end := time.Now().UTC().AddDate(0, 0, -1)
	start := end.AddDate(0, 0, -27)
	if v, err := time.Parse("2006-01-02", q.Get("start")); err == nil {
		start = v
	}
	if v, err := time.Parse("2006-01-02", q.Get("end")); err == nil {
		end = v
	}
	if end.Before(start) || end.Sub(start) > 490*24*time.Hour {
		writeErr(w, http.StatusBadRequest, "choose a date range of up to 16 months")
		return
	}
	dim := q.Get("dimension")
	if dim == "" {
		dim = "query"
	}
	country := ""
	if cc := q.Get("country"); cc != "" {
		if country = gsc.Alpha3(cc); country == "" {
			writeErr(w, http.StatusBadRequest, "unknown country "+cc)
			return
		}
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	rows, err := s.GoogleClient(r.Context()).Report(r.Context(), p.GSCProperty, gsc.Query{
		Start: start.Format("2006-01-02"), End: end.Format("2006-01-02"), Dimension: dim, Country: country, Limit: limit,
	})
	if err != nil {
		s.writeAPIErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"start": start.Format("2006-01-02"), "end": end.Format("2006-01-02"), "dimension": dim, "rows": rows})
}
