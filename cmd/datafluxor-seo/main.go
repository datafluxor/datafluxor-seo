// Command datafluxor-seo runs the self-hosted DataFluxor SEO app.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/datafluxor/datafluxor-seo/internal/auth"
	"github.com/datafluxor/datafluxor-seo/internal/config"
	"github.com/datafluxor/datafluxor-seo/internal/dfapi"
	"github.com/datafluxor/datafluxor-seo/internal/gsc"
	"github.com/datafluxor/datafluxor-seo/internal/ranks"
	"github.com/datafluxor/datafluxor-seo/internal/server"
	"github.com/datafluxor/datafluxor-seo/internal/store"
)

// version is set at build time: -ldflags "-X main.version=v1.0.0".
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	healthcheck := flag.Bool("healthcheck", false, "check that the local server answers, for container health checks")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *healthcheck {
		os.Exit(checkHealth())
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log); err != nil {
		log.Error("stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := st.FailRunningAudits(ctx); err != nil {
		return err
	}
	dfapi.Version = version

	srv := &server.Server{Cfg: cfg, Store: st, Log: log, Version: version}
	srv.CrawlAllowPrivate, _ = strconv.ParseBool(os.Getenv("DFSEO_CRAWL_ALLOW_PRIVATE"))
	srv.Auth = &auth.Auth{Store: st, CookieSecure: cfg.CookieSecure}
	srv.API = dfapi.New(cfg.APIURL, srv.APIKey)
	srv.Ranks = &ranks.Tracker{Store: st, API: srv.API, Log: log}
	srv.Google = &gsc.Client{
		RedirectURL: cfg.PublicURL + "/api/google/callback",
		Load:        srv.GoogleToken,
		Save:        srv.SaveGoogleToken,
	}

	users, err := st.UserCount(ctx)
	if err != nil {
		return err
	}
	if users == 0 {
		srv.Auth.SetupCode = auth.NewSetupCode()
		log.Info("first run: open the app and create your account with this setup code", "url", cfg.PublicURL, "setup code", srv.Auth.SetupCode)
	}

	go srv.Ranks.Run(ctx)
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = st.PurgeSessions(ctx)
			}
		}
	}()

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(ctx),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	errc := make(chan error, 1)
	go func() { errc <- httpSrv.ListenAndServe() }()
	log.Info("DataFluxor SEO is running", "version", version, "listen", cfg.Addr, "url", cfg.PublicURL, "data", cfg.DataDir, "api_key", srv.APIKey(ctx) != "")
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdown)
}

// checkHealth asks the running server for /healthz. The container image has
// no shell or curl, so the binary checks itself.
func checkHealth() int {
	addr := os.Getenv("DFSEO_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	c := &http.Client{Timeout: 4 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
