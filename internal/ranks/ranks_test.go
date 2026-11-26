package ranks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/datafluxor/datafluxor-seo/internal/dfapi"
	"github.com/datafluxor/datafluxor-seo/internal/store"
)

func TestReadingFromARealAnswer(t *testing.T) {
	raw, err := os.ReadFile("testdata/serp-api-us.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Reading(raw, "https://www.ScraperAPI.com/")
	if err != nil {
		t.Fatal(err)
	}
	if c.Position == nil || *c.Position != 7 || c.URL == "" {
		t.Fatalf("scraperapi.com is 7th: %+v", c.Position)
	}
	if len(c.Results) != 10 {
		t.Fatalf("results: %d", len(c.Results))
	}
	found := false
	for _, f := range c.Features {
		found = found || f == "ai_overview"
	}
	if !found {
		t.Fatalf("the page had an AI Overview: %v", c.Features)
	}
	none, _ := Reading(raw, "example.org")
	if none.Position != nil {
		t.Fatal("a site not on the page has no position")
	}
}

func TestSameSite(t *testing.T) {
	cases := []struct {
		host, domain string
		want         bool
	}{
		{"www.example.com", "example.com", true},
		{"blog.example.com", "https://example.com/", true},
		{"example.com", "www.example.com", true},
		{"notexample.com", "example.com", false},
		{"example.com.evil.net", "example.com", false},
		{"", "example.com", false},
	}
	for _, c := range cases {
		if got := SameSite(c.host, c.domain); got != c.want {
			t.Errorf("SameSite(%q,%q) = %v", c.host, c.domain, got)
		}
	}
}

func TestDailyScheduleChecksEachKeywordOnce(t *testing.T) {
	var calls atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["max_age_seconds"] == nil {
			t.Error("scheduled checks should accept a recent page")
		}
		w.Write([]byte(`{"success":true,"data":{"items":[{"position":3,"url":"https://example.com/a","domain":"example.com"}],"credits_used":5}}`))
	}))
	defer api.Close()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	p, _ := st.CreateProject(ctx, "Example", "example.com")
	for _, kw := range []string{"one", "two"} {
		if _, _, err := st.AddKeyword(ctx, store.Keyword{ProjectID: p.ID, Keyword: kw, Country: "US", Language: "en", Device: "desktop"}); err != nil {
			t.Fatal(err)
		}
	}
	client := dfapi.New(api.URL, func(context.Context) string { return "df_live_test" })
	now := time.Date(2026, 11, 26, 7, 0, 0, 0, time.UTC)
	tr := &Tracker{Store: st, API: client, Now: func() time.Time { return now }, Gap: time.Millisecond}
	tr.RunDue(ctx)
	if calls.Load() != 2 {
		t.Fatalf("expected 2 checks, got %d", calls.Load())
	}
	tr.RunDue(ctx)
	if calls.Load() != 2 {
		t.Fatal("keywords already checked today must not be checked again")
	}
	ks, _ := st.Keywords(ctx, p.ID)
	if ks[0].Latest == nil || ks[0].Latest.Position == nil || *ks[0].Latest.Position != 3 {
		t.Fatalf("reading not stored: %+v", ks[0].Latest)
	}
	if spent, _ := st.SpendSince(ctx, now.AddDate(0, 0, -1)); spent != 10 {
		t.Fatalf("spend recorded: %d", spent)
	}
	// The monthly cap stops the next day's checks.
	_ = st.SetSetting(ctx, SettingMonthlyCap, "10")
	now = now.AddDate(0, 0, 1)
	tr.RunDue(ctx)
	if calls.Load() != 2 {
		t.Fatal("the monthly cap was ignored")
	}
}
