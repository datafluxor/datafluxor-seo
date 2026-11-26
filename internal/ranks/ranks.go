// Package ranks checks where a project's site ranks on Google for its
// keywords, through the DataFluxor API, once a day and on request.
package ranks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/datafluxor/datafluxor-seo/internal/dfapi"
	"github.com/datafluxor/datafluxor-seo/internal/store"
)

// Depths a user may choose: how many Google results each check reads.
var Depths = []int{10, 20, 30, 50, 100}

// Settings keys.
const (
	SettingDepth      = "rank_depth"      // results read per check
	SettingHour       = "rank_check_hour" // UTC hour of the daily check
	SettingMonthlyCap = "monthly_credit_cap"
	SettingPaused     = "rank_schedule_paused"
)

// Tracker runs rank checks.
type Tracker struct {
	Store *store.Store
	API   *dfapi.Client
	Log   *slog.Logger
	Now   func() time.Time

	// Gap is the pause between scheduled checks (default 7 s, inside the
	// API's per-minute limit).
	Gap time.Duration

	mu      sync.Mutex
	running bool
}

// serpData is the part of a /api/v1/scrape/serp answer a rank check uses.
type serpData struct {
	Items []struct {
		Position int    `json:"position"`
		URL      string `json:"url"`
		Domain   string `json:"domain"`
		Title    string `json:"title"`
	} `json:"items"`
	PeopleAlsoAsk      []string `json:"people_also_ask"`
	HasLocalPack       bool     `json:"has_local_pack"`
	HasFeaturedSnippet bool     `json:"has_featured_snippet"`
	HasKnowledgePanel  bool     `json:"has_knowledge_panel"`
	HasAIOverview      bool     `json:"has_ai_overview"`
	CreditsUsed        int      `json:"credits_used"`
	CacheHit           bool     `json:"cache_hit"`
	Complete           bool     `json:"complete"`
	Status             string   `json:"status"`
}

// Depth reads the configured depth.
func (t *Tracker) Depth(ctx context.Context) int {
	v, _ := t.Store.Setting(ctx, SettingDepth)
	n, _ := strconv.Atoi(v)
	for _, d := range Depths {
		if d == n {
			return n
		}
	}
	return 10
}

// Check reads one keyword's Google page now and stores the reading.
// maxAge, when positive, accepts a stored page no older than that.
func (t *Tracker) Check(ctx context.Context, kw store.Keyword, maxAge time.Duration) (store.RankCheck, error) {
	project, err := t.Store.Project(ctx, kw.ProjectID)
	if err != nil {
		return store.RankCheck{}, err
	}
	depth := t.Depth(ctx)
	body := map[string]any{
		"keyword":  kw.Keyword,
		"country":  kw.Country,
		"language": kw.Language,
		"device":   kw.Device,
		"depth":    depth,
	}
	if maxAge > 0 {
		body["max_age_seconds"] = int(maxAge.Seconds())
	}
	now := t.now()
	res, err := t.API.Post(ctx, "/api/v1/scrape/serp", body)
	if err != nil {
		if errors.Is(err, dfapi.ErrNoKey) {
			return store.RankCheck{}, err
		}
		failed, _ := t.Store.AddRankCheck(ctx, store.RankCheck{KeywordID: kw.ID, CheckedAt: now, Depth: depth, Error: err.Error()})
		return failed, err
	}
	check, err := Reading(res.Data, project.Domain)
	if err != nil {
		return store.RankCheck{}, err
	}
	check.KeywordID, check.CheckedAt, check.Depth = kw.ID, now, depth
	if res.CreditsUsed > check.CreditsUsed {
		check.CreditsUsed = res.CreditsUsed
	}
	_ = t.Store.RecordSpend(ctx, "rank check", check.CreditsUsed, kw.Keyword+" ("+kw.Country+")")
	return t.Store.AddRankCheck(ctx, check)
}

// Reading turns a SERP answer into a rank reading for domain.
func Reading(data json.RawMessage, domain string) (store.RankCheck, error) {
	var d serpData
	if err := json.Unmarshal(data, &d); err != nil {
		return store.RankCheck{}, fmt.Errorf("unexpected Google results format: %w", err)
	}
	c := store.RankCheck{CreditsUsed: d.CreditsUsed, CacheHit: d.CacheHit, Features: []string{}}
	for _, it := range d.Items {
		host := it.Domain
		if host == "" {
			if u, err := url.Parse(it.URL); err == nil {
				host = u.Hostname()
			}
		}
		c.Results = append(c.Results, store.RankResult{Position: it.Position, URL: it.URL, Domain: host, Title: it.Title})
		if c.Position == nil && SameSite(host, domain) {
			p := it.Position
			c.Position = &p
			c.URL = it.URL
		}
	}
	if d.HasAIOverview {
		c.Features = append(c.Features, "ai_overview")
	}
	if d.HasFeaturedSnippet {
		c.Features = append(c.Features, "featured_snippet")
	}
	if d.HasLocalPack {
		c.Features = append(c.Features, "local_pack")
	}
	if d.HasKnowledgePanel {
		c.Features = append(c.Features, "knowledge_panel")
	}
	if len(d.PeopleAlsoAsk) > 0 {
		c.Features = append(c.Features, "people_also_ask")
	}
	return c, nil
}

// SameSite says whether host belongs to the project's domain (any subdomain).
func SameSite(host, domain string) bool {
	h := NormalizeDomain(host)
	d := NormalizeDomain(domain)
	return h != "" && d != "" && (h == d || strings.HasSuffix(h, "."+d))
}

// NormalizeDomain reduces "https://www.Example.com/path" to "example.com".
func NormalizeDomain(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil {
			s = u.Hostname()
		}
	}
	s = strings.SplitN(s, "/", 2)[0]
	s = strings.SplitN(s, ":", 2)[0]
	s = strings.TrimSuffix(s, ".")
	return strings.TrimPrefix(s, "www.")
}

// ---- the daily schedule ----

// Run checks due keywords every few minutes until ctx ends.
func (t *Tracker) Run(ctx context.Context) {
	tick := time.NewTicker(5 * time.Minute)
	defer tick.Stop()
	for {
		t.RunDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// RunDue checks every keyword not yet checked since today's check time.
func (t *Tracker) RunDue(ctx context.Context) {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return
	}
	t.running = true
	t.mu.Unlock()
	defer func() { t.mu.Lock(); t.running = false; t.mu.Unlock() }()

	if paused, _ := t.Store.Setting(ctx, SettingPaused); paused == "1" || !t.API.HasKey(ctx) {
		return
	}
	due := t.dueTime(ctx)
	keywords, err := t.Store.AllKeywords(ctx)
	if err != nil {
		t.log().Error("list keywords", "err", err)
		return
	}
	for _, kw := range keywords {
		if ctx.Err() != nil {
			return
		}
		last, err := t.Store.LastCheck(ctx, kw.ID, false)
		if err != nil || !last.Before(due) {
			continue
		}
		// A failed check is tried again an hour later, not at once.
		if failed, err := t.Store.LastCheck(ctx, kw.ID, true); err != nil || t.now().Sub(failed) < time.Hour {
			continue
		}
		if over, cap, spent := t.overBudget(ctx); over {
			t.log().Warn("monthly credit cap reached: daily rank checks paused until next month", "cap", cap, "spent", spent)
			return
		}
		// A page Google served in the last 12 hours is as good as a new one.
		if _, err := t.Check(ctx, kw, 12*time.Hour); err != nil {
			t.log().Warn("rank check failed", "keyword", kw.Keyword, "err", err)
			var apiErr *dfapi.Error
			if errors.As(err, &apiErr) && (apiErr.Code == "INSUFFICIENT_CREDITS" || apiErr.Code == "UNAUTHORIZED" || apiErr.Code == "DAILY_LIVE_LIMIT") {
				return // every other keyword would fail the same way
			}
		}
		// Stay well inside the API's per-minute limit.
		select {
		case <-ctx.Done():
			return
		case <-time.After(t.gap()):
		}
	}
}

func (t *Tracker) dueTime(ctx context.Context) time.Time {
	v, _ := t.Store.Setting(ctx, SettingHour)
	hour, err := strconv.Atoi(v)
	if err != nil || hour < 0 || hour > 23 {
		hour = 6
	}
	now := t.now().UTC()
	due := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, time.UTC)
	if now.Before(due) {
		due = due.AddDate(0, 0, -1)
	}
	return due
}

func (t *Tracker) overBudget(ctx context.Context) (bool, int, int) {
	v, _ := t.Store.Setting(ctx, SettingMonthlyCap)
	cap, _ := strconv.Atoi(v)
	if cap <= 0 {
		return false, 0, 0
	}
	now := t.now().UTC()
	spent, err := t.Store.SpendSince(ctx, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return true, cap, 0
	}
	return spent >= cap, cap, spent
}

func (t *Tracker) gap() time.Duration {
	if t.Gap > 0 {
		return t.Gap
	}
	return 7 * time.Second
}

func (t *Tracker) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

func (t *Tracker) log() *slog.Logger {
	if t.Log != nil {
		return t.Log
	}
	return slog.Default()
}
