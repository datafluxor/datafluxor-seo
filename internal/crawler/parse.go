package crawler

import (
	"io"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// pageInfo is what one HTML page says about itself.
type pageInfo struct {
	title         string
	titles        int
	description   string
	descriptions  int
	h1            string
	h1s           int
	canonical     string
	noindex       bool
	nofollow      bool
	lang          string
	viewport      bool
	words         int
	links         []link
	images        []image
	insecureAsset []string // http:// scripts, styles and images on an https page
}

type link struct {
	href     string
	nofollow bool
}

type image struct {
	src    string
	hasAlt bool
}

// parseHTML reads the parts of a page an audit checks.
func parseHTML(r io.Reader, base *url.URL) pageInfo {
	var p pageInfo
	z := html.NewTokenizer(r)
	var inTitle, inH1, skip int
	var title, h1 strings.Builder
	inBody := false
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			p.title = clean(title.String())
			if p.h1 == "" {
				p.h1 = clean(h1.String())
			}
			return p
		case html.TextToken:
			text := string(z.Text())
			if inTitle > 0 {
				title.WriteString(text)
			}
			if inH1 > 0 && p.h1s == 1 {
				h1.WriteString(text)
			}
			if inBody && skip == 0 {
				p.words += countWords(text)
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			attrs := map[string]string{}
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				key := strings.ToLower(string(k))
				if _, seen := attrs[key]; !seen {
					attrs[key] = string(v)
				}
			}
			tag := string(name)
			switch tag {
			case "html":
				p.lang = strings.TrimSpace(attrs["lang"])
			case "body":
				inBody = true
			case "title":
				if tt == html.StartTagToken && !inBody {
					p.titles++
					if p.titles == 1 {
						inTitle++
					}
				}
			case "h1":
				p.h1s++
				if tt == html.StartTagToken {
					inH1++
				}
			case "script", "style", "noscript", "template":
				if tt == html.StartTagToken {
					skip++
				}
				if tag == "script" {
					p.noteAsset(base, attrs["src"])
				}
			case "meta":
				nameAttr := strings.ToLower(attrs["name"])
				content := attrs["content"]
				switch nameAttr {
				case "description":
					p.descriptions++
					if p.descriptions == 1 {
						p.description = clean(content)
					}
				case "robots", "googlebot":
					c := strings.ToLower(content)
					p.noindex = p.noindex || strings.Contains(c, "noindex") || strings.Contains(c, "none")
					p.nofollow = p.nofollow || strings.Contains(c, "nofollow") || strings.Contains(c, "none")
				case "viewport":
					p.viewport = true
				}
			case "link":
				rel := strings.ToLower(attrs["rel"])
				if hasWord(rel, "canonical") && p.canonical == "" {
					p.canonical = resolve(base, attrs["href"])
				}
				if hasWord(rel, "stylesheet") {
					p.noteAsset(base, attrs["href"])
				}
			case "a":
				if href := resolve(base, attrs["href"]); href != "" {
					p.links = append(p.links, link{href: href, nofollow: hasWord(strings.ToLower(attrs["rel"]), "nofollow")})
				}
			case "img":
				_, alt := attrs["alt"]
				src := attrs["src"]
				p.images = append(p.images, image{src: resolve(base, src), hasAlt: alt})
				p.noteAsset(base, src)
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "title":
				if inTitle > 0 {
					inTitle--
				}
			case "h1":
				if inH1 > 0 {
					inH1--
				}
			case "script", "style", "noscript", "template":
				if skip > 0 {
					skip--
				}
			}
		}
	}
}

func (p *pageInfo) noteAsset(base *url.URL, ref string) {
	if base == nil || base.Scheme != "https" || ref == "" {
		return
	}
	if u := resolve(base, ref); strings.HasPrefix(u, "http://") {
		p.insecureAsset = append(p.insecureAsset, u)
	}
}

// resolve makes an absolute http(s) URL without its fragment, or "".
func resolve(base *url.URL, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "#") {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

func hasWord(s, word string) bool {
	for _, f := range strings.Fields(s) {
		if f == word {
			return true
		}
	}
	return false
}

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

func countWords(s string) int {
	n, in := 0, false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if !in {
				n++
				in = true
			}
		} else {
			in = false
		}
	}
	return n
}
