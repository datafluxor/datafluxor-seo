package gsc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRefreshAndReport(t *testing.T) {
	google := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_ = r.ParseForm()
			if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "rt" || r.Form.Get("client_secret") != "secret" {
				t.Errorf("token form: %v", r.Form)
			}
			w.Write([]byte(`{"access_token":"at","expires_in":3600}`))
		case r.URL.Path == "/sites":
			w.Write([]byte(`{"siteEntry":[{"siteUrl":"sc-domain:example.com","permissionLevel":"siteOwner"},{"siteUrl":"https://x.test/","permissionLevel":"siteUnverifiedUser"}]}`))
		case strings.HasSuffix(r.URL.Path, "/searchAnalytics/query"):
			if r.Header.Get("Authorization") != "Bearer at" {
				t.Errorf("auth: %q", r.Header.Get("Authorization"))
			}
			if !strings.Contains(r.URL.EscapedPath(), "sc-domain:example.com") {
				t.Errorf("property not in path: %s", r.URL.EscapedPath())
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !strings.Contains(mustJSON(body["dimensionFilterGroups"]), `"gbr"`) {
				t.Errorf("country filter missing: %v", body)
			}
			w.Write([]byte(`{"rows":[{"keys":["running shoes"],"clicks":12,"impressions":400,"ctr":0.03,"position":8.4}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer google.Close()
	tokenURL, apiBase = google.URL+"/token", google.URL
	saved := Token{RefreshToken: "rt"}
	c := &Client{ClientID: "id", ClientSecret: "secret",
		Load: func(context.Context) (Token, error) { return saved, nil },
		Save: func(_ context.Context, t Token) error { saved = t; return nil }}
	sites, err := c.Sites(context.Background())
	if err != nil || len(sites) != 1 {
		t.Fatalf("sites=%v err=%v", sites, err)
	}
	if saved.AccessToken != "at" || time.Until(saved.Expiry) < 30*time.Minute {
		t.Fatalf("token not saved: %+v", saved)
	}
	rows, err := c.Report(context.Background(), "sc-domain:example.com", Query{Start: "2026-10-01", End: "2026-10-28", Dimension: "query", Country: Alpha3("GB")})
	if err != nil || len(rows) != 1 || rows[0].Position != 8.4 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if _, err := c.Report(context.Background(), "x", Query{Dimension: "bogus"}); err == nil {
		t.Fatal("unknown dimension accepted")
	}
}

func TestNotConnected(t *testing.T) {
	c := &Client{ClientID: "id", ClientSecret: "s", Load: func(context.Context) (Token, error) { return Token{}, nil }}
	if _, err := c.Sites(context.Background()); err != ErrNotConnected {
		t.Fatalf("err=%v", err)
	}
	if u := c.AuthURL("st"); !strings.Contains(u, "access_type=offline") || !strings.Contains(u, "webmasters.readonly") {
		t.Fatalf("auth url: %s", u)
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
