package crawler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const goodHead = `<html lang="en"><head><title>%s</title><meta name="description" content="A perfectly reasonable description of this page that is long enough."><meta name="viewport" content="width=device-width"></head><body>`

func testSite() *httptest.Server {
	mux := http.NewServeMux()
	words := strings.Repeat("word ", 250)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, goodHead+`<h1>Home</h1>%s<a href="/about">About</a> <a href="/missing">Gone</a> <a href="/old">Old</a>
			<a href="/dup1">d1</a> <a href="/dup2">d2</a> <a href="/private/x">p</a> <a href="https://elsewhere.example/">ext</a>
			<img src="/logo.png"></body></html>`, "Home page of the test site", words)
	})
	mux.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head></head><body><p>short</p></body></html>`)
	})
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/older", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/older", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/about", http.StatusMovedPermanently)
	})
	for _, p := range []string{"/dup1", "/dup2"} {
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, goodHead+`<h1>Dup</h1>%s</body></html>`, "The same duplicated title", words)
		})
	}
	mux.HandleFunc("/private/x", func(w http.ResponseWriter, r *http.Request) { panic("robots.txt disallows this") })
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "User-agent: *\nDisallow: /private/\n")
	})
	return httptest.NewServer(mux)
}

func issues(res Result) map[string][]string {
	out := map[string][]string{}
	for _, i := range res.Issues {
		out[i.Code] = append(out[i.Code], i.PageURL)
	}
	return out
}

func TestCrawlFindsTheUsualProblems(t *testing.T) {
	site := testSite()
	defer site.Close()
	res, err := Crawl(context.Background(), site.URL+"/", Options{MaxPages: 50, AllowPrivate: true})
	if err != nil {
		t.Fatal(err)
	}
	got := issues(res)
	for _, code := range []string{"http_error", "broken_link", "redirect_chain", "missing_title", "missing_h1", "missing_description",
		"duplicate_title", "image_alt", "thin_content", "missing_lang", "missing_viewport", "missing_sitemap", "linked_redirect"} {
		if len(got[code]) == 0 {
			t.Errorf("expected a %s issue; got %v", code, got)
		}
	}
	if len(got["duplicate_title"]) != 2 {
		t.Errorf("both duplicate pages should be reported: %v", got["duplicate_title"])
	}
	for _, p := range res.Pages {
		if strings.Contains(p.URL, "/private/") {
			t.Fatal("crawled a page robots.txt disallows")
		}
		if strings.Contains(p.URL, "elsewhere.example") {
			t.Fatal("crawled an external site")
		}
	}
	if !strings.HasSuffix(got["broken_link"][0], "/") {
		t.Errorf("the broken link should be reported on the page that has it: %v", got["broken_link"])
	}
}

func TestCrawlRespectsThePageBudget(t *testing.T) {
	site := testSite()
	defer site.Close()
	res, err := Crawl(context.Background(), site.URL+"/", Options{MaxPages: 2, AllowPrivate: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) > 2 {
		t.Fatalf("crawled %d pages with a budget of 2", len(res.Pages))
	}
}

func TestCrawlRefusesPrivateAddressesByDefault(t *testing.T) {
	site := testSite()
	defer site.Close()
	res, _ := Crawl(context.Background(), site.URL+"/", Options{MaxPages: 5})
	if len(res.Pages) != 1 || res.Pages[0].StatusCode != 0 {
		t.Fatalf("a loopback address must not be fetched: %+v", res.Pages)
	}
	if got := issues(res); len(got["fetch_failed"]) != 1 {
		t.Fatalf("the refusal should be reported: %v", got)
	}
}

func TestCrawlStopsWhenCancelled(t *testing.T) {
	site := testSite()
	defer site.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Crawl(ctx, site.URL+"/", Options{MaxPages: 50, AllowPrivate: true}); err == nil {
		t.Fatal("a cancelled crawl should say so")
	}
}

func TestRobots(t *testing.T) {
	r := parseRobots(strings.NewReader("User-agent: Googlebot\nDisallow: /\n\nUser-agent: *\nDisallow: /admin\nAllow: /admin/public\nDisallow: /*.pdf$\nSitemap: https://x.test/s.xml\n"))
	cases := map[string]bool{"/": true, "/admin": false, "/admin/x": false, "/admin/public/a": true, "/doc.pdf": false, "/doc.pdf?x": true}
	for path, want := range cases {
		if got := r.allowed(path); got != want {
			t.Errorf("allowed(%q) = %v, want %v", path, got, want)
		}
	}
	if len(r.sitemaps) != 1 {
		t.Errorf("sitemaps: %v", r.sitemaps)
	}
	own := parseRobots(strings.NewReader("User-agent: *\nDisallow: /\n\nUser-agent: datafluxor-seo\nDisallow: /x\n"))
	if !own.allowed("/") || own.allowed("/x") {
		t.Error("a group naming this crawler takes precedence over *")
	}
}
