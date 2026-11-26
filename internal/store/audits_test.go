package store

import "testing"

func TestHealthScore(t *testing.T) {
	cases := []struct {
		pages    int
		c        Counts
		min, max int
	}{
		{40, Counts{}, 100, 100},
		{40, Counts{Notices: 1}, 100, 100},
		{1, Counts{Warnings: 2, Notices: 3}, 60, 75},  // a one-page site with a few gaps
		{10, Counts{Errors: 10}, 50, 60},              // one error on every page
		{10, Counts{Errors: 50, Warnings: 50}, 0, 10}, // badly broken
		{0, Counts{Errors: 1}, 0, 100},
	}
	for _, c := range cases {
		if got := HealthScore(c.pages, c.c); got < c.min || got > c.max {
			t.Errorf("HealthScore(%d, %+v) = %d, want %d-%d", c.pages, c.c, got, c.min, c.max)
		}
	}
}
