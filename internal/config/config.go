// Package config reads the server's settings from the environment.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is everything the server needs at start-up. Settings that a user
// changes in the app (the API key, Google sign-in, the check hour) live in the
// database; an environment value, when set, takes precedence over them.
type Config struct {
	Addr          string // listen address, e.g. ":8080"
	DataDir       string // where the SQLite database lives
	PublicURL     string // the address people open, used for Google sign-in redirects
	APIURL        string // DataFluxor API base URL
	APIKey        string // optional: DATAFLUXOR_API_KEY overrides the key saved in Settings
	GoogleID      string // optional: Google OAuth client for Search Console
	GoogleSecret  string
	CookieSecure  bool // send the session cookie only over HTTPS
	CrawlMaxPages int  // upper bound a user may choose for one site audit
}

// Load reads the environment.
func Load() (Config, error) {
	c := Config{
		Addr:          env("DFSEO_ADDR", ":8080"),
		DataDir:       env("DFSEO_DATA_DIR", "./data"),
		PublicURL:     strings.TrimRight(env("DFSEO_PUBLIC_URL", "http://localhost:8080"), "/"),
		APIURL:        strings.TrimRight(env("DATAFLUXOR_API_URL", "https://datafluxor.com"), "/"),
		APIKey:        strings.TrimSpace(os.Getenv("DATAFLUXOR_API_KEY")),
		GoogleID:      strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID")),
		GoogleSecret:  strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET")),
		CrawlMaxPages: 5000,
	}
	pub, err := url.Parse(c.PublicURL)
	if err != nil || (pub.Scheme != "http" && pub.Scheme != "https") || pub.Host == "" {
		return c, fmt.Errorf("DFSEO_PUBLIC_URL must be an http(s) address, got %q", c.PublicURL)
	}
	c.CookieSecure = pub.Scheme == "https"
	if v := os.Getenv("DFSEO_COOKIE_SECURE"); v != "" {
		c.CookieSecure, _ = strconv.ParseBool(v)
	}
	if v := os.Getenv("DFSEO_CRAWL_MAX_PAGES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return c, fmt.Errorf("DFSEO_CRAWL_MAX_PAGES must be a positive number")
		}
		c.CrawlMaxPages = n
	}
	api, err := url.Parse(c.APIURL)
	if err != nil || (api.Scheme != "https" && api.Scheme != "http") || api.Host == "" {
		return c, fmt.Errorf("DATAFLUXOR_API_URL must be an http(s) address")
	}
	if c.DataDir, err = filepath.Abs(c.DataDir); err != nil {
		return c, err
	}
	return c, nil
}

func env(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}
