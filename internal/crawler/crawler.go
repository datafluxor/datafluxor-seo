// Package crawler audits a website by crawling it from this server. It needs
// no API key: everything it reports comes from the site itself.
package crawler

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/datafluxor/datafluxor-seo/internal/store"
)

// UserAgent identifies the crawler to the sites it reads.
const UserAgent = "Mozilla/5.0 (compatible; datafluxor-seo/1.0; +https://github.com/datafluxor/datafluxor-seo)"

// Options bound one crawl.
type Options struct {
	MaxPages    int
	Concurrency int
	Timeout     time.Duration // per request
	MaxBytes    int64         // per page
	// Progress, when set, is told how many pages are done.
	Progress func(pages int)
	// HTTP overrides the client (tests).
	HTTP *http.Client
	// AllowPrivate lets the crawler read private network addresses. Off by
	// default so a shared server cannot be used to probe its own network.
	AllowPrivate bool
}

// Result is a finished crawl.
type Result struct {
	Pages  []store.AuditPage
	Issues []store.AuditIssue
}

type fetched struct {
	page        store.AuditPage
	info        pageInfo
	isHTML      bool
	err         error
	redirectTo  string
	sourceDepth int
}

type crawl struct {
	opt      Options
	client   *http.Client
	hosts    map[string]bool // hosts treated as the site
	robots   robots
	start    *url.URL
	mu       sync.Mutex
	seen     map[string]bool
	results  map[string]*fetched
	order    []string
	inlinks  map[string]int
	linkedBy map[string][]string // target -> source pages (first few)
	noRobots bool
	sitemap  int // URLs listed in sitemaps (-1 when none found)
}

// Crawl audits the site at startURL.
func Crawl(ctx context.Context, startURL string, opt Options) (Result, error) {
	if opt.MaxPages <= 0 {
		opt.MaxPages = 500
	}
	if opt.Concurrency <= 0 {
		opt.Concurrency = 4
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 20 * time.Second
	}
	if opt.MaxBytes <= 0 {
		opt.MaxBytes = 5 << 20
	}
	start, err := url.Parse(startURL)
	if err != nil || (start.Scheme != "http" && start.Scheme != "https") || start.Host == "" {
		return Result{}, fmt.Errorf("start address must be an http(s) URL")
	}
	c := &crawl{
		opt: opt, start: start, hosts: map[string]bool{},
		seen: map[string]bool{}, results: map[string]*fetched{}, inlinks: map[string]int{}, linkedBy: map[string][]string{},
	}
	c.client = opt.HTTP
	if c.client == nil {
		c.client = newClient(opt)
	}
	c.addHost(start.Hostname())

	// Follow the start address's redirects first: https://example.com often
	// lands on https://www.example.com, which is then the site.
	if final, err := c.finalAddress(ctx, start.String()); err == nil {
		if u, err := url.Parse(final); err == nil {
			c.addHost(u.Hostname())
			start = u
			c.start = u
		}
	}
	c.loadRobots(ctx)
	queue := []queued{{url: start.String(), depth: 0}}
	for _, s := range c.sitemapURLs(ctx) {
		queue = append(queue, queued{url: s, depth: 1})
	}
	c.run(ctx, queue)
	if ctx.Err() != nil {
		return c.result(), ctx.Err()
	}
	return c.result(), nil
}

type queued struct {
	url   string
	depth int
	from  string
}

func (c *crawl) addHost(h string) {
	h = strings.ToLower(h)
	c.hosts[h] = true
	if strings.HasPrefix(h, "www.") {
		c.hosts[strings.TrimPrefix(h, "www.")] = true
	} else {
		c.hosts["www."+h] = true
	}
}

func (c *crawl) internal(u *url.URL) bool { return c.hosts[strings.ToLower(u.Hostname())] }

// normalize drops fragments and default ports so one page is crawled once.
func normalize(raw string) (string, *url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", nil, false
	}
	u.Fragment, u.RawFragment = "", ""
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		u.Host = u.Hostname()
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), u, true
}

func (c *crawl) run(ctx context.Context, initial []queued) {
	work := make(chan queued)
	found := make(chan []queued)
	var wg sync.WaitGroup
	for i := 0; i < c.opt.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for q := range work {
				found <- c.visit(ctx, q)
			}
		}()
	}
	pending := []queued{}
	for _, q := range initial {
		if c.claim(q) {
			pending = append(pending, q)
		}
	}
	inFlight := 0
	for len(pending) > 0 || inFlight > 0 {
		if ctx.Err() != nil && inFlight == 0 {
			break
		}
		var next chan queued
		var head queued
		if len(pending) > 0 && ctx.Err() == nil {
			next, head = work, pending[0]
		}
		select {
		case next <- head:
			pending = pending[1:]
			inFlight++
		case more := <-found:
			inFlight--
			for _, q := range more {
				if c.claim(q) {
					pending = append(pending, q)
				}
			}
			if c.opt.Progress != nil {
				c.mu.Lock()
				n := len(c.results)
				c.mu.Unlock()
				c.opt.Progress(n)
			}
		}
	}
	close(work)
	wg.Wait()
}

// claim marks an address as queued, within the page budget and robots.txt.
func (c *crawl) claim(q queued) bool {
	key, u, ok := normalize(q.url)
	if !ok || !c.internal(u) {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen[key] || len(c.seen) >= c.opt.MaxPages {
		return false
	}
	path := u.EscapedPath()
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	if !c.robots.allowed(path) {
		return false
	}
	c.seen[key] = true
	c.order = append(c.order, key)
	return true
}

func (c *crawl) visit(ctx context.Context, q queued) []queued {
	key, u, _ := normalize(q.url)
	f := c.fetch(ctx, key, u)
	f.page.Depth = q.depth
	c.mu.Lock()
	c.results[key] = f
	c.mu.Unlock()

	var out []queued
	if f.redirectTo != "" {
		out = append(out, queued{url: f.redirectTo, depth: q.depth, from: key})
		c.noteLink(key, f.redirectTo)
	}
	if f.isHTML && !f.info.nofollow {
		for _, l := range f.info.links {
			target, tu, ok := normalize(l.href)
			if !ok || !c.internal(tu) {
				continue
			}
			c.noteLink(key, target)
			if !l.nofollow {
				out = append(out, queued{url: target, depth: q.depth + 1, from: key})
			}
		}
	}
	return out
}

func (c *crawl) noteLink(from, to string) {
	key, _, ok := normalize(to)
	if !ok || key == from {
		return
	}
	c.mu.Lock()
	c.inlinks[key]++
	if len(c.linkedBy[key]) < 5 && !contains(c.linkedBy[key], from) {
		c.linkedBy[key] = append(c.linkedBy[key], from)
	}
	c.mu.Unlock()
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (c *crawl) fetch(ctx context.Context, key string, u *url.URL) *fetched {
	f := &fetched{page: store.AuditPage{URL: key}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, key, nil)
	if err != nil {
		f.err = err
		return f
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	begin := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		f.err = err
		f.page.ResponseMS = int(time.Since(begin).Milliseconds())
		return f
	}
	defer resp.Body.Close()
	f.page.StatusCode = resp.StatusCode
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	f.page.ContentType = ct
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if loc := resp.Header.Get("Location"); loc != "" {
			f.redirectTo = resolve(u, loc)
			f.page.RedirectTo = f.redirectTo
		}
		f.page.ResponseMS = int(time.Since(begin).Milliseconds())
		return f
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.opt.MaxBytes+1))
	f.page.ResponseMS = int(time.Since(begin).Milliseconds())
	f.page.Bytes = len(body)
	if err != nil {
		f.err = err
		return f
	}
	if ct == "text/html" || ct == "application/xhtml+xml" {
		f.isHTML = true
		f.info = parseHTML(bytes.NewReader(body), u)
		f.page.Title = truncate(f.info.title, 300)
		f.page.MetaDescription = truncate(f.info.description, 500)
		f.page.H1 = truncate(f.info.h1, 300)
		f.page.Canonical = f.info.canonical
		f.page.Words = f.info.words
		if f.info.noindex || strings.Contains(strings.ToLower(resp.Header.Get("X-Robots-Tag")), "noindex") {
			f.page.Noindex = true
			f.info.noindex = true
		}
	}
	return f
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// finalAddress follows the start address's redirects (at most 5).
func (c *crawl) finalAddress(ctx context.Context, start string) (string, error) {
	cur := start
	for i := 0; i < 5; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, cur, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", UserAgent)
		resp, err := c.client.Do(req)
		if err != nil {
			return "", err
		}
		resp.Body.Close()
		if resp.StatusCode < 300 || resp.StatusCode >= 400 {
			return cur, nil
		}
		u, _ := url.Parse(cur)
		next := resolve(u, resp.Header.Get("Location"))
		if next == "" {
			return cur, nil
		}
		nu, err := url.Parse(next)
		if err != nil {
			return cur, nil
		}
		// Only follow to the same site (with or without www).
		if !c.internal(nu) {
			return cur, nil
		}
		cur = next
	}
	return cur, nil
}

func (c *crawl) get(ctx context.Context, raw string, limit int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	return b, resp.StatusCode, err
}

func (c *crawl) loadRobots(ctx context.Context) {
	base := c.start.Scheme + "://" + c.start.Host
	body, status, err := c.get(ctx, base+"/robots.txt", 512<<10)
	if err != nil || status != http.StatusOK {
		c.noRobots = true
		return
	}
	c.robots = parseRobots(bytes.NewReader(body))
}

type sitemapDoc struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
	Sitemaps []struct {
		Loc string `xml:"loc"`
	} `xml:"sitemap"`
}

// sitemapURLs reads the site's sitemaps (robots.txt Sitemap lines, else
// /sitemap.xml), following one level of sitemap index.
func (c *crawl) sitemapURLs(ctx context.Context) []string {
	sources := append([]string{}, c.robots.sitemaps...)
	if len(sources) == 0 {
		sources = []string{c.start.Scheme + "://" + c.start.Host + "/sitemap.xml"}
	}
	var out []string
	c.sitemap = -1
	visited := 0
	for len(sources) > 0 && visited < 10 && len(out) < c.opt.MaxPages {
		src := sources[0]
		sources = sources[1:]
		visited++
		body, status, err := c.get(ctx, src, 20<<20)
		if err != nil || status != http.StatusOK {
			continue
		}
		var doc sitemapDoc
		if xml.Unmarshal(body, &doc) != nil {
			continue
		}
		if c.sitemap < 0 {
			c.sitemap = 0
		}
		for _, s := range doc.Sitemaps {
			sources = append(sources, strings.TrimSpace(s.Loc))
		}
		for _, u := range doc.URLs {
			c.sitemap++
			if loc := strings.TrimSpace(u.Loc); loc != "" && len(out) < c.opt.MaxPages {
				out = append(out, loc)
			}
		}
	}
	return out
}

// newClient never follows redirects (each hop is a page in the report) and,
// unless allowed, refuses private and loopback addresses.
func newClient(opt Options) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConnsPerHost:   opt.Concurrency,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: opt.Timeout,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if !opt.AllowPrivate {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
				if err != nil {
					return nil, err
				}
				for _, ip := range ips {
					if privateIP(ip) {
						return nil, errPrivate
					}
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
	return &http.Client{
		Transport:     transport,
		Timeout:       opt.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

var errPrivate = errors.New("refused: the address is on a private network (set DFSEO_CRAWL_ALLOW_PRIVATE=true to allow)")

func privateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() ||
		ip.IsInterfaceLocalMulticast() || ip.Equal(net.ParseIP("169.254.169.254"))
}

// ---- turning pages into issues ----

func (c *crawl) result() Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	var res Result
	add := func(page, code, sev, detail string) {
		res.Issues = append(res.Issues, store.AuditIssue{PageURL: page, Code: code, Severity: sev, Detail: detail})
	}
	titles := map[string][]string{}
	descs := map[string][]string{}
	for _, key := range c.order {
		f, ok := c.results[key]
		if !ok {
			continue // queued but never fetched (stopped early)
		}
		f.page.Inlinks = c.inlinks[key]
		res.Pages = append(res.Pages, f.page)
		p := f.page
		switch {
		case f.err != nil:
			add(key, "fetch_failed", "error", "Could not be read: "+shortErr(f.err))
			continue
		case p.StatusCode >= 400:
			add(key, "http_error", "error", fmt.Sprintf("Answers HTTP %d", p.StatusCode))
			for _, from := range c.linkedBy[key] {
				add(from, "broken_link", "error", fmt.Sprintf("Links to %s, which answers HTTP %d", key, p.StatusCode))
			}
			continue
		case p.StatusCode >= 300:
			if p.RedirectTo != "" {
				if next, ok := c.results[mustNorm(p.RedirectTo)]; ok && next.page.StatusCode >= 300 && next.page.StatusCode < 400 {
					add(key, "redirect_chain", "warning", "Redirects to "+p.RedirectTo+", which redirects again")
				}
				if len(c.linkedBy[key]) > 0 {
					add(key, "linked_redirect", "notice", fmt.Sprintf("%d internal link(s) point at this redirect instead of its target %s", c.inlinks[key], p.RedirectTo))
				}
			}
			continue
		}
		if !f.isHTML {
			continue
		}
		info := f.info
		if c.start.Scheme == "https" && strings.HasPrefix(key, "http://") {
			add(key, "not_https", "warning", "Page is served over plain http")
		}
		if p.ResponseMS > 2000 {
			add(key, "slow_page", "warning", fmt.Sprintf("Took %.1f s to answer", float64(p.ResponseMS)/1000))
		}
		if p.Bytes > 3<<20 {
			add(key, "large_page", "notice", fmt.Sprintf("HTML is %.1f MB", float64(p.Bytes)/(1<<20)))
		}
		if info.noindex {
			add(key, "noindex", "notice", "Asks search engines not to index it (noindex)")
		}
		if p.Canonical != "" && mustNorm(p.Canonical) != key {
			add(key, "canonical_elsewhere", "notice", "Canonical points to "+p.Canonical)
		}
		indexable := !info.noindex && (p.Canonical == "" || mustNorm(p.Canonical) == key)
		switch {
		case info.title == "":
			add(key, "missing_title", "error", "No <title>")
		case len([]rune(info.title)) > 60:
			add(key, "long_title", "warning", fmt.Sprintf("Title is %d characters; Google shows about 60", len([]rune(info.title))))
		case len([]rune(info.title)) < 10:
			add(key, "short_title", "warning", fmt.Sprintf("Title is only %d characters", len([]rune(info.title))))
		}
		if info.titles > 1 {
			add(key, "multiple_titles", "warning", fmt.Sprintf("%d <title> tags", info.titles))
		}
		switch d := len([]rune(info.description)); {
		case d == 0:
			add(key, "missing_description", "warning", "No meta description")
		case d > 160:
			add(key, "long_description", "notice", fmt.Sprintf("Meta description is %d characters; Google shows about 160", d))
		case d < 50:
			add(key, "short_description", "notice", fmt.Sprintf("Meta description is only %d characters", d))
		}
		switch {
		case info.h1s == 0:
			add(key, "missing_h1", "warning", "No <h1> heading")
		case info.h1s > 1:
			add(key, "multiple_h1", "notice", fmt.Sprintf("%d <h1> headings", info.h1s))
		}
		if info.words < 200 && indexable {
			add(key, "thin_content", "notice", fmt.Sprintf("Only %d words of text", info.words))
		}
		if info.lang == "" {
			add(key, "missing_lang", "notice", "<html> has no lang attribute")
		}
		if !info.viewport {
			add(key, "missing_viewport", "warning", "No viewport meta tag: the page may not fit phones")
		}
		missingAlt := 0
		for _, img := range info.images {
			if !img.hasAlt {
				missingAlt++
			}
		}
		if missingAlt > 0 {
			add(key, "image_alt", "warning", fmt.Sprintf("%d image(s) without alt text", missingAlt))
		}
		if len(info.insecureAsset) > 0 {
			add(key, "mixed_content", "warning", fmt.Sprintf("Loads %d resource(s) over plain http, e.g. %s", len(info.insecureAsset), info.insecureAsset[0]))
		}
		if p.Depth > 4 {
			add(key, "deep_page", "notice", fmt.Sprintf("%d clicks from the home page", p.Depth))
		}
		if indexable && info.title != "" {
			titles[info.title] = append(titles[info.title], key)
		}
		if indexable && info.description != "" {
			descs[info.description] = append(descs[info.description], key)
		}
	}
	for _, group := range []struct {
		m    map[string][]string
		code string
		what string
	}{{titles, "duplicate_title", "title"}, {descs, "duplicate_description", "meta description"}} {
		for _, pages := range group.m {
			if len(pages) < 2 {
				continue
			}
			for _, p := range pages {
				add(p, group.code, "warning", fmt.Sprintf("Same %s as %d other page(s)", group.what, len(pages)-1))
			}
		}
	}
	site := c.start.Scheme + "://" + c.start.Host + "/"
	if c.noRobots {
		add(site, "missing_robots", "notice", "No robots.txt found")
	}
	if c.sitemap < 0 {
		add(site, "missing_sitemap", "notice", "No XML sitemap found (robots.txt Sitemap line or /sitemap.xml)")
	}
	sort.SliceStable(res.Issues, func(i, j int) bool { return sevRank(res.Issues[i].Severity) < sevRank(res.Issues[j].Severity) })
	return res
}

func sevRank(s string) int {
	switch s {
	case "error":
		return 0
	case "warning":
		return 1
	}
	return 2
}

func mustNorm(s string) string {
	k, _, ok := normalize(s)
	if !ok {
		return s
	}
	return k
}

func shortErr(err error) string {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timed out"
	}
	if errors.Is(err, errPrivate) {
		return errPrivate.Error()
	}
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && len(s)-i < 120 {
		return s[i+2:]
	}
	return truncate(s, 200)
}
