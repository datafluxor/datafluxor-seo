package dfapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func key(context.Context) string { return "df_live_test" }

func TestPostSendsKeyAndReadsCost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "df_live_test" || r.Header.Get("Idempotency-Key") == "" || !strings.HasPrefix(r.Header.Get("User-Agent"), "datafluxor-seo/") {
			t.Errorf("headers: %v", r.Header)
		}
		w.Write([]byte(`{"success":true,"data":{"items":[],"credits_used":5,"cache_hit":false}}`))
	}))
	defer srv.Close()
	res, err := New(srv.URL, key).Post(context.Background(), "/api/v1/scrape/serp", map[string]any{"keyword": "x"})
	if err != nil || res.CreditsUsed != 5 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestErrorsKeepTheirCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(`{"success":false,"code":"INSUFFICIENT_CREDITS","error":"the balance does not cover the request"}`))
	}))
	defer srv.Close()
	_, err := New(srv.URL, key).Post(context.Background(), "/api/v1/scrape/serp", map[string]any{})
	var e *Error
	if !errors.As(err, &e) || e.Code != "INSUFFICIENT_CREDITS" || e.Status != 402 || e.RetryAfter != 30 || !strings.Contains(e.Message, "balance") {
		t.Fatalf("err=%#v", err)
	}
}

func TestNoKey(t *testing.T) {
	_, err := New("http://127.0.0.1:1", func(context.Context) string { return "" }).Get(context.Background(), "/api/v1/account")
	if !errors.Is(err, ErrNoKey) {
		t.Fatalf("err=%v", err)
	}
}

func TestQueuedRunIsFollowed(t *testing.T) {
	var polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"success":true,"data":{"accepted":true,"run_id":"run_1"}}`))
		case r.URL.Path == "/api/v1/scrape/runs/run_1":
			if polls.Add(1) < 2 {
				w.Write([]byte(`{"success":true,"data":{"run":{"id":"run_1"}}}`))
				return
			}
			w.Write([]byte(`{"success":true,"data":{"run":{"id":"run_1","finished_at":"2026-11-26T00:00:00Z","outcome":"completed","credit_cost":5,"result":{"answer":42}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, key)
	c.PollEvery = 10 * time.Millisecond
	res, err := c.Post(context.Background(), "/api/v1/scrape/ai-visibility", map[string]any{})
	if err != nil || string(res.Data) != `{"answer":42}` || res.CreditsUsed != 5 || res.RunID != "run_1" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
