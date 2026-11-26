package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datafluxor/datafluxor-seo/internal/auth"
	"github.com/datafluxor/datafluxor-seo/internal/config"
	"github.com/datafluxor/datafluxor-seo/internal/dfapi"
	"github.com/datafluxor/datafluxor-seo/internal/gsc"
	"github.com/datafluxor/datafluxor-seo/internal/ranks"
	"github.com/datafluxor/datafluxor-seo/internal/store"
)

type harness struct {
	t      *testing.T
	srv    *httptest.Server
	client *http.Client
	app    *Server
}

func newHarness(t *testing.T, apiURL string) *harness {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := config.Config{PublicURL: "http://localhost", APIURL: apiURL, CrawlMaxPages: 100}
	s := &Server{Cfg: cfg, Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "test"}
	s.Auth = &auth.Auth{Store: st, SetupCode: "ABCD-EFGH-IJKL"}
	s.API = dfapi.New(apiURL, s.APIKey)
	s.Ranks = &ranks.Tracker{Store: st, API: s.API}
	s.Google = &gsc.Client{Load: s.GoogleToken, Save: s.SaveGoogleToken}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srv := httptest.NewServer(s.Handler(ctx))
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &harness{t: t, srv: srv, client: &http.Client{Jar: jar}, app: s}
}

func (h *harness) do(method, path, body string, csrf bool) (int, map[string]any) {
	h.t.Helper()
	req, _ := http.NewRequest(method, h.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if csrf {
		req.Header.Set("X-Requested-With", "datafluxor-seo")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]any{"raw": string(raw)}
	}
	return resp.StatusCode, out
}

func (h *harness) setup() {
	if code, out := h.do("POST", "/api/setup", `{"code":"abcd-efgh-ijkl","email":"Owner@Example.com","password":"a long password"}`, false); code != 200 {
		h.t.Fatalf("setup: %d %v", code, out)
	}
}

func TestSetupNeedsTheCodeAndHappensOnce(t *testing.T) {
	h := newHarness(t, "http://127.0.0.1:1")
	if _, out := h.do("GET", "/api/status", "", false); out["setup_needed"] != true {
		t.Fatalf("status: %v", out)
	}
	if code, _ := h.do("POST", "/api/setup", `{"code":"WRONG","email":"a@b.c","password":"a long password"}`, false); code != 400 {
		t.Fatal("a wrong setup code must be refused")
	}
	if code, _ := h.do("POST", "/api/setup", `{"code":"ABCD-EFGH-IJKL","email":"a@b.c","password":"short"}`, false); code != 400 {
		t.Fatal("a short password must be refused")
	}
	h.setup()
	if _, out := h.do("GET", "/api/status", "", false); out["signed_in"] != true || out["setup_needed"] != false {
		t.Fatalf("after setup: %v", out)
	}
	if code, _ := h.do("POST", "/api/setup", `{"code":"ABCD-EFGH-IJKL","email":"x@y.z","password":"a long password"}`, false); code != 400 {
		t.Fatal("setup must not run twice")
	}
}

func TestSignInAndProtection(t *testing.T) {
	h := newHarness(t, "http://127.0.0.1:1")
	h.setup()
	if code, _ := h.do("POST", "/api/projects", `{"domain":"example.com"}`, false); code != 403 {
		t.Fatal("a write without the app's header must be refused (CSRF)")
	}
	if code, _ := h.do("POST", "/api/logout", ``, true); code != 200 {
		t.Fatal("logout")
	}
	if code, _ := h.do("GET", "/api/projects", "", false); code != 401 {
		t.Fatal("signed-out requests must be refused")
	}
	if code, _ := h.do("POST", "/api/login", `{"email":"owner@example.com","password":"wrong password"}`, false); code != 401 {
		t.Fatal("a wrong password must be refused")
	}
	if code, _ := h.do("POST", "/api/login", `{"email":"OWNER@example.com","password":"a long password"}`, false); code != 200 {
		t.Fatal("sign in")
	}
}

func TestProjectsKeywordsAndNoKey(t *testing.T) {
	h := newHarness(t, "http://127.0.0.1:1")
	h.setup()
	code, p := h.do("POST", "/api/projects", `{"domain":"https://www.Example.com/shop"}`, true)
	if code != 201 || p["domain"] != "example.com" {
		t.Fatalf("project: %d %v", code, p)
	}
	if code, _ := h.do("POST", "/api/projects", `{"domain":"not a domain"}`, true); code != 400 {
		t.Fatal("a bad domain must be refused")
	}
	code, out := h.do("POST", "/api/projects/1/keywords", `{"keywords":["Running Shoes","running  shoes","trail shoes"],"country":"gb"}`, true)
	if code != 201 || len(out["added"].([]any)) != 2 || out["already_tracked"].(float64) != 1 {
		t.Fatalf("keywords: %d %v", code, out)
	}
	code, out = h.do("POST", "/api/keywords/1/check", ``, true)
	if code != 402 || out["code"] != "NO_API_KEY" || out["signup_url"] == nil {
		t.Fatalf("without a key the app must point to sign-up: %d %v", code, out)
	}
	code, out = h.do("POST", "/api/tools/serp", `{"keyword":"x"}`, true)
	if code != 402 || out["code"] != "NO_API_KEY" {
		t.Fatalf("tool without key: %d %v", code, out)
	}
	if code, _ := h.do("POST", "/api/tools/delete-everything", `{}`, true); code != 404 {
		t.Fatal("only known tools are passed through")
	}
	if code, _ := h.do("GET", "/api/projects/1/overview", "", false); code != 200 {
		t.Fatal("overview")
	}
}

func TestToolsUseTheKeyAndRecordSpend(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/account":
			w.Write([]byte(`{"success":true,"data":{"credits_remaining":49}}`))
		case "/api/v1/scrape/serp":
			w.Write([]byte(`{"success":true,"data":{"items":[{"position":2,"url":"https://example.com/","domain":"example.com"}],"credits_used":5}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	h := newHarness(t, api.URL)
	h.setup()
	if code, out := h.do("PUT", "/api/settings", `{"api_key":"df_live_0123456789abcdef0123"}`, true); code != 200 || out["api_key_set"] != true || out["api_key_suffix"] != "0123" {
		t.Fatalf("save key: %d %v", code, out)
	}
	h.do("POST", "/api/projects", `{"domain":"example.com"}`, true)
	h.do("POST", "/api/projects/1/keywords", `{"keywords":["shoes"],"country":"US"}`, true)
	code, out := h.do("POST", "/api/keywords/1/check", ``, true)
	if code != 200 || out["position"].(float64) != 2 {
		t.Fatalf("check: %d %v", code, out)
	}
	_, acct := h.do("GET", "/api/account", "", false)
	if acct["spent_this_month"].(float64) != 5 {
		t.Fatalf("spend: %v", acct)
	}
	// The key is never sent back to the browser.
	_, settings := h.do("GET", "/api/settings", "", false)
	b, _ := json.Marshal(settings)
	if strings.Contains(string(b), "df_live_0123456789abcdef0123") {
		t.Fatal("settings leaked the API key")
	}
}

func TestWebAppIsServed(t *testing.T) {
	if _, err := dist.Open("dist/index.html"); err != nil {
		t.Skip("web app not built (make web)")
	}
	h := newHarness(t, "http://127.0.0.1:1")
	for _, path := range []string{"/", "/projects/1/audit"} {
		resp, err := http.Get(h.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") || resp.Header.Get("Content-Security-Policy") == "" {
			t.Fatalf("%s: %d %v", path, resp.StatusCode, resp.Header)
		}
	}
}
