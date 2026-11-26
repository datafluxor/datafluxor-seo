// Package dfapi calls the DataFluxor API (https://datafluxor.com/api/v1).
//
// Every call that needs live data (Google results, AI answers, keyword and
// backlink data) goes through here with the user's API key. A key is created
// at https://datafluxor.com/app/access.
package dfapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Version is sent in the User-Agent so DataFluxor can tell self-hosted calls apart.
var Version = "dev"

// ErrNoKey means no API key is configured.
var ErrNoKey = errors.New("no DataFluxor API key is set: add one in Settings")

// Error is a refusal from the API, with its machine-readable code.
type Error struct {
	Status     int    `json:"status"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	RetryAfter int    `json:"retry_after_seconds,omitempty"`
}

func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("DataFluxor %s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("DataFluxor HTTP %d: %s", e.Status, e.Message)
}

// Result is the data of a successful call plus what it cost.
type Result struct {
	Data        json.RawMessage
	CreditsUsed int
	CacheHit    bool
	RunID       string
}

// Client is a DataFluxor API client. Key is read for each call so a key
// changed in Settings takes effect at once.
type Client struct {
	BaseURL string
	Key     func(context.Context) string
	HTTP    *http.Client
	// PollEvery is how often a queued run is polled.
	PollEvery time.Duration
	// MaxWait bounds how long a call waits for a queued run.
	MaxWait time.Duration
}

// New returns a client for baseURL.
func New(baseURL string, key func(context.Context) string) *Client {
	return &Client{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		Key:       key,
		HTTP:      &http.Client{Timeout: 4 * time.Minute},
		PollEvery: 3 * time.Second,
		MaxWait:   8 * time.Minute,
	}
}

// HasKey says whether a key is configured.
func (c *Client) HasKey(ctx context.Context) bool { return c.Key(ctx) != "" }

// Get calls a read endpoint.
func (c *Client) Get(ctx context.Context, path string) (Result, error) {
	return c.do(ctx, http.MethodGet, path, nil, "")
}

// Post calls a tool. A fresh idempotency key makes a network retry safe: the
// API never charges the same key twice.
func (c *Client) Post(ctx context.Context, path string, body any) (Result, error) {
	return c.do(ctx, http.MethodPost, path, body, newIdempotencyKey())
}

func newIdempotencyKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "dfseo-" + hex.EncodeToString(b)
}

type envelope struct {
	Success           bool            `json:"success"`
	Data              json.RawMessage `json:"data"`
	Code              string          `json:"code"`
	Error             json.RawMessage `json:"error"`
	Message           string          `json:"message"`
	RetryAfterSeconds int             `json:"retry_after_seconds"`
	CreditCost        *int            `json:"credit_cost"`
}

type usage struct {
	CreditsUsed *int   `json:"credits_used"`
	CacheHit    bool   `json:"cache_hit"`
	RunID       string `json:"run_id"`
	Status      string `json:"status"`
	State       string `json:"state"`
	Accepted    bool   `json:"accepted"`
	ResultURL   string `json:"result_url"`
	StatusURL   string `json:"status_url"`
}

func (c *Client) do(ctx context.Context, method, path string, body any, idem string) (Result, error) {
	key := c.Key(ctx)
	if key == "" {
		return Result{}, ErrNoKey
	}
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return Result{}, err
		}
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		res, status, err := c.once(ctx, method, path, payload, key, idem)
		if err == nil {
			if status == http.StatusAccepted {
				return c.waitForRun(ctx, res)
			}
			return res, nil
		}
		lastErr = err
		// Only a network failure is retried here, and only with the same
		// idempotency key (or a read): the API then answers from its record.
		var apiErr *Error
		if errors.As(err, &apiErr) || (method != http.MethodGet && idem == "") {
			return Result{}, err
		}
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 2 * time.Second):
		}
	}
	return Result{}, lastErr
}

func (c *Client) once(ctx context.Context, method, path string, payload []byte, key, idem string) (Result, int, error) {
	var rd io.Reader
	if payload != nil {
		rd = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
	if err != nil {
		return Result{}, 0, err
	}
	req.Header.Set("X-API-Key", key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "datafluxor-seo/"+Version+" (+https://github.com/datafluxor/datafluxor-seo)")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Result{}, 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return Result{}, resp.StatusCode, err
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Result{}, resp.StatusCode, &Error{Status: resp.StatusCode, Message: "the API answered with something that is not JSON"}
	}
	if resp.StatusCode >= 400 || !env.Success {
		return Result{}, resp.StatusCode, apiError(resp, env)
	}
	res := Result{Data: env.Data}
	var u usage
	_ = json.Unmarshal(env.Data, &u)
	if u.CreditsUsed != nil {
		res.CreditsUsed = *u.CreditsUsed
	} else if env.CreditCost != nil {
		res.CreditsUsed = *env.CreditCost
	} else if h := resp.Header.Get("X-DataFluxor-Credit-Cost"); h != "" {
		res.CreditsUsed, _ = strconv.Atoi(h)
	}
	res.CacheHit, res.RunID = u.CacheHit, u.RunID
	return res, resp.StatusCode, nil
}

func apiError(resp *http.Response, env envelope) *Error {
	e := &Error{Status: resp.StatusCode, Code: env.Code, RetryAfter: env.RetryAfterSeconds}
	// The error is a string on tool endpoints and may be an object elsewhere.
	var s string
	if json.Unmarshal(env.Error, &s) == nil {
		e.Message = s
	} else {
		var o struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(env.Error, &o) == nil {
			e.Message = o.Message
			if e.Code == "" {
				e.Code = o.Code
			}
		}
	}
	if e.Message == "" {
		e.Message = env.Message
	}
	if e.Message == "" {
		e.Message = http.StatusText(resp.StatusCode)
	}
	if e.RetryAfter == 0 {
		e.RetryAfter, _ = strconv.Atoi(resp.Header.Get("Retry-After"))
	}
	return e
}

// waitForRun polls a run the API queued instead of answering at once.
func (c *Client) waitForRun(ctx context.Context, accepted Result) (Result, error) {
	var a usage
	_ = json.Unmarshal(accepted.Data, &a)
	if a.RunID == "" {
		return Result{}, &Error{Status: http.StatusAccepted, Message: "the API queued the call but gave no run to follow"}
	}
	deadline := time.Now().Add(c.MaxWait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(c.PollEvery):
		}
		res, _, err := c.once(ctx, http.MethodGet, "/api/v1/scrape/runs/"+a.RunID, nil, c.Key(ctx), "")
		if err != nil {
			var apiErr *Error
			if errors.As(err, &apiErr) {
				return Result{}, err
			}
			continue // a network blip: poll again
		}
		var view struct {
			Run struct {
				FinishedAt string          `json:"finished_at"`
				Outcome    string          `json:"outcome"`
				Result     json.RawMessage `json:"result"`
				CreditCost int             `json:"credit_cost"`
				ErrorCode  string          `json:"error_code"`
				Failure    json.RawMessage `json:"failure"`
				HTTPStatus int             `json:"http_status"`
			} `json:"run"`
		}
		if json.Unmarshal(res.Data, &view) != nil || view.Run.FinishedAt == "" {
			continue
		}
		if view.Run.Outcome == "failed" || len(view.Run.Result) == 0 || string(view.Run.Result) == "null" {
			msg := strings.TrimSpace(string(view.Run.Failure))
			if msg == "" || msg == "null" {
				msg = "the queued run finished without a result"
			}
			return Result{}, &Error{Status: view.Run.HTTPStatus, Code: view.Run.ErrorCode, Message: msg}
		}
		return Result{Data: view.Run.Result, CreditsUsed: view.Run.CreditCost, RunID: a.RunID}, nil
	}
	return Result{}, &Error{Status: http.StatusGatewayTimeout, Code: "RUN_STILL_QUEUED",
		Message: "DataFluxor is still working on it (run " + a.RunID + "); try again in a few minutes"}
}
