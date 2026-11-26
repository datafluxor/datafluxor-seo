package crawler

import (
	"bufio"
	"io"
	"strings"
)

// robots is the part of a robots.txt that applies to this crawler: the group
// for our user agent if there is one, else the "*" group.
type robots struct {
	rules    []rule
	sitemaps []string
}

type rule struct {
	allow bool
	path  string
}

const robotsAgent = "datafluxor-seo"

func parseRobots(r io.Reader) robots {
	type group struct {
		agents []string
		rules  []rule
	}
	var groups []*group
	var cur *group
	lastWasAgent := false
	var out robots
	sc := bufio.NewScanner(io.LimitReader(r, 512<<10))
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		switch key {
		case "user-agent":
			if cur == nil || !lastWasAgent {
				cur = &group{}
				groups = append(groups, cur)
			}
			cur.agents = append(cur.agents, strings.ToLower(val))
			lastWasAgent = true
		case "allow", "disallow":
			lastWasAgent = false
			if cur == nil {
				continue
			}
			if key == "disallow" && val == "" {
				continue // "Disallow:" with nothing allows everything
			}
			cur.rules = append(cur.rules, rule{allow: key == "allow", path: val})
		case "sitemap":
			lastWasAgent = false
			out.sitemaps = append(out.sitemaps, val)
		default:
			lastWasAgent = false
		}
	}
	var star *group
	for _, g := range groups {
		for _, a := range g.agents {
			if strings.Contains(a, robotsAgent) {
				out.rules = g.rules
				return out
			}
			if a == "*" && star == nil {
				star = g
			}
		}
	}
	if star != nil {
		out.rules = star.rules
	}
	return out
}

// allowed applies the longest matching rule; Allow wins a tie.
func (r robots) allowed(path string) bool {
	best, allow := -1, true
	for _, ru := range r.rules {
		if n := matchLen(ru.path, path); n > best || (n == best && ru.allow) {
			if n >= 0 {
				best, allow = n, ru.allow
			}
		}
	}
	return allow
}

// matchLen returns the length of pattern when it matches the start of path
// (with * wildcards and a $ end anchor), else -1.
func matchLen(pattern, path string) int {
	anchored := strings.HasSuffix(pattern, "$")
	p := strings.TrimSuffix(pattern, "$")
	if !wildMatch(p, path, anchored) {
		return -1
	}
	return len(pattern)
}

func wildMatch(p, s string, anchored bool) bool {
	parts := strings.Split(p, "*")
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	pos := len(parts[0])
	for i, part := range parts[1:] {
		last := i == len(parts)-2
		if last && anchored {
			return strings.HasSuffix(s[pos:], part)
		}
		j := strings.Index(s[pos:], part)
		if j < 0 {
			return false
		}
		pos += j + len(part)
	}
	return !anchored || pos == len(s) || (len(parts) > 1 && parts[len(parts)-1] == "")
}
