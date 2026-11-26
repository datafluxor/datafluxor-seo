// Package gsc reads Google Search Console with the user's own Google sign-in.
// It needs no DataFluxor key: the data is the user's own, from Google.
//
// Each install uses its own Google OAuth client (created in Google Cloud
// Console, see docs/search-console.md). The refresh token is kept in the
// local database.
package gsc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Scope is read-only Search Console access.
const Scope = "https://www.googleapis.com/auth/webmasters.readonly"

var (
	authURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	tokenURL = "https://oauth2.googleapis.com/token"
	apiBase  = "https://www.googleapis.com/webmasters/v3"
)

// ErrNotConnected means no Google account is connected yet.
var ErrNotConnected = errors.New("Search Console is not connected: connect Google in Settings")

// Token is a stored Google sign-in.
type Token struct {
	RefreshToken string    `json:"refresh_token"`
	AccessToken  string    `json:"access_token"`
	Expiry       time.Time `json:"expiry"`
}

// Client talks to Google for one install.
type Client struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	HTTP         *http.Client
	// Load and Save keep the token in the database.
	Load func(context.Context) (Token, error)
	Save func(context.Context, Token) error
}

// tokenMu serialises token refreshes (a Client is copied per request).
var tokenMu sync.Mutex

// Configured says whether a Google OAuth client is set up.
func (c *Client) Configured() bool { return c.ClientID != "" && c.ClientSecret != "" }

// AuthURL is where the user signs in to Google.
func (c *Client) AuthURL(state string) string {
	q := url.Values{
		"client_id":     {c.ClientID},
		"redirect_uri":  {c.RedirectURL},
		"response_type": {"code"},
		"scope":         {Scope},
		"access_type":   {"offline"},
		"prompt":        {"consent"}, // always return a refresh token
		"state":         {state},
	}
	return authURL + "?" + q.Encode()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

// Exchange turns the code Google sent back into a stored token.
func (c *Client) Exchange(ctx context.Context, code string) error {
	tr, err := c.token(ctx, url.Values{
		"code": {code}, "redirect_uri": {c.RedirectURL}, "grant_type": {"authorization_code"},
	})
	if err != nil {
		return err
	}
	if tr.RefreshToken == "" {
		return errors.New("Google returned no refresh token; remove the app's access at myaccount.google.com/permissions and connect again")
	}
	return c.Save(ctx, Token{RefreshToken: tr.RefreshToken, AccessToken: tr.AccessToken, Expiry: time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)})
}

func (c *Client) token(ctx context.Context, form url.Values) (tokenResponse, error) {
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http().Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer resp.Body.Close()
	var tr tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tr); err != nil {
		return tr, fmt.Errorf("Google token answer: %w", err)
	}
	if resp.StatusCode != http.StatusOK || tr.Error != "" {
		return tr, fmt.Errorf("Google refused the sign-in: %s %s", tr.Error, tr.Description)
	}
	return tr, nil
}

// accessToken returns a valid access token, refreshing it when needed.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	t, err := c.Load(ctx)
	if err != nil {
		return "", err
	}
	if t.RefreshToken == "" {
		return "", ErrNotConnected
	}
	if t.AccessToken != "" && time.Until(t.Expiry) > time.Minute {
		return t.AccessToken, nil
	}
	tr, err := c.token(ctx, url.Values{"refresh_token": {t.RefreshToken}, "grant_type": {"refresh_token"}})
	if err != nil {
		return "", err
	}
	t.AccessToken = tr.AccessToken
	t.Expiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	if tr.RefreshToken != "" {
		t.RefreshToken = tr.RefreshToken
	}
	return t.AccessToken, c.Save(ctx, t)
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (c *Client) call(ctx context.Context, method, path string, body any, out any) error {
	tok, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error.Message == "" {
			e.Error.Message = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("Search Console: %s", e.Error.Message)
	}
	return json.Unmarshal(raw, out)
}

// Site is one Search Console property.
type Site struct {
	SiteURL         string `json:"siteUrl"`
	PermissionLevel string `json:"permissionLevel"`
}

// Sites lists the properties the connected account can read.
func (c *Client) Sites(ctx context.Context) ([]Site, error) {
	var out struct {
		SiteEntry []Site `json:"siteEntry"`
	}
	if err := c.call(ctx, http.MethodGet, "/sites", nil, &out); err != nil {
		return nil, err
	}
	sites := []Site{}
	for _, s := range out.SiteEntry {
		if s.PermissionLevel != "siteUnverifiedUser" {
			sites = append(sites, s)
		}
	}
	return sites, nil
}

// Row is one line of a Search Console report.
type Row struct {
	Keys        []string `json:"keys"`
	Clicks      float64  `json:"clicks"`
	Impressions float64  `json:"impressions"`
	CTR         float64  `json:"ctr"`
	Position    float64  `json:"position"`
}

// Query is a report request.
type Query struct {
	Start     string // YYYY-MM-DD
	End       string
	Dimension string // query, page, country, device, date
	Country   string // ISO-3166 alpha-3 as Google expects ("gbr"); empty for all
	Limit     int
}

// Report reads clicks, impressions, CTR and position for a property.
func (c *Client) Report(ctx context.Context, site string, q Query) ([]Row, error) {
	switch q.Dimension {
	case "query", "page", "country", "device", "date":
	default:
		return nil, fmt.Errorf("unknown dimension %q", q.Dimension)
	}
	if q.Limit <= 0 || q.Limit > 25000 {
		q.Limit = 1000
	}
	body := map[string]any{
		"startDate": q.Start, "endDate": q.End, "dimensions": []string{q.Dimension},
		"rowLimit": q.Limit, "dataState": "all",
	}
	if q.Country != "" {
		body["dimensionFilterGroups"] = []any{map[string]any{"filters": []any{
			map[string]any{"dimension": "country", "operator": "equals", "expression": strings.ToLower(q.Country)},
		}}}
	}
	var out struct {
		Rows []Row `json:"rows"`
	}
	if err := c.call(ctx, http.MethodPost, "/sites/"+url.PathEscape(site)+"/searchAnalytics/query", body, &out); err != nil {
		return nil, err
	}
	if out.Rows == nil {
		out.Rows = []Row{}
	}
	return out.Rows, nil
}

// Alpha3 converts the two-letter countries rank tracking uses to the three
// letters Search Console uses.
func Alpha3(cc string) string {
	return alpha3[strings.ToUpper(cc)]
}

var alpha3 = map[string]string{
	"US": "usa", "GB": "gbr", "UK": "gbr", "CA": "can", "AU": "aus", "IE": "irl", "NZ": "nzl", "DE": "deu", "FR": "fra",
	"ES": "esp", "IT": "ita", "NL": "nld", "BE": "bel", "SE": "swe", "NO": "nor", "DK": "dnk", "FI": "fin", "PL": "pol",
	"PT": "prt", "AT": "aut", "CH": "che", "IN": "ind", "BR": "bra", "MX": "mex", "AR": "arg", "ZA": "zaf", "SG": "sgp",
	"JP": "jpn", "KR": "kor", "AE": "are", "SA": "sau", "TR": "tur", "PH": "phl", "MY": "mys", "ID": "idn", "TH": "tha",
	"VN": "vnm", "NG": "nga", "KE": "ken", "EG": "egy", "IL": "isr", "GR": "grc", "CZ": "cze", "RO": "rou", "HU": "hun",
	"UA": "ukr", "CL": "chl", "CO": "col", "PE": "per", "HK": "hkg", "TW": "twn", "PK": "pak", "BD": "bgd",
}
