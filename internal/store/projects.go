package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Project is one website the user works on.
type Project struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Domain      string    `json:"domain"`
	GSCProperty string    `json:"gsc_property"`
	CreatedAt   time.Time `json:"created_at"`
}

// Projects lists every project, newest first.
func (s *Store) Projects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,domain,gsc_property,created_at FROM projects ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Domain, &p.GSCProperty, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Project reads one project.
func (s *Store) Project(ctx context.Context, id int64) (Project, error) {
	var p Project
	err := s.db.QueryRowContext(ctx, `SELECT id,name,domain,gsc_property,created_at FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.Domain, &p.GSCProperty, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// CreateProject adds a project.
func (s *Store) CreateProject(ctx context.Context, name, domain string) (Project, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `INSERT INTO projects(name,domain,created_at) VALUES(?,?,?)`, name, domain, now)
	if err != nil {
		return Project{}, err
	}
	id, _ := res.LastInsertId()
	return Project{ID: id, Name: name, Domain: domain, CreatedAt: now}, nil
}

// UpdateProject changes a project's name, domain and Search Console property.
func (s *Store) UpdateProject(ctx context.Context, p Project) error {
	res, err := s.db.ExecContext(ctx, `UPDATE projects SET name=?, domain=?, gsc_property=? WHERE id=?`, p.Name, p.Domain, p.GSCProperty, p.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteProject removes a project and everything in it.
func (s *Store) DeleteProject(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- keywords and rank checks ----

// Keyword is one tracked search.
type Keyword struct {
	ID        int64      `json:"id"`
	ProjectID int64      `json:"project_id"`
	Keyword   string     `json:"keyword"`
	Country   string     `json:"country"`
	Language  string     `json:"language"`
	Device    string     `json:"device"`
	Tags      string     `json:"tags"`
	CreatedAt time.Time  `json:"created_at"`
	Latest    *RankCheck `json:"latest,omitempty"`
	Previous  *RankCheck `json:"previous,omitempty"`
}

// RankResult is one result on a checked Google page.
type RankResult struct {
	Position int    `json:"position"`
	URL      string `json:"url"`
	Domain   string `json:"domain"`
	Title    string `json:"title"`
}

// RankCheck is one reading of a keyword's position.
type RankCheck struct {
	ID          int64        `json:"id"`
	KeywordID   int64        `json:"keyword_id"`
	CheckedAt   time.Time    `json:"checked_at"`
	Position    *int         `json:"position"`
	URL         string       `json:"url"`
	Depth       int          `json:"depth"`
	CreditsUsed int          `json:"credits_used"`
	CacheHit    bool         `json:"cache_hit"`
	Results     []RankResult `json:"results,omitempty"`
	Features    []string     `json:"features"`
	Error       string       `json:"error,omitempty"`
}

// AddKeyword tracks a keyword; an existing identical one is returned as is.
func (s *Store) AddKeyword(ctx context.Context, k Keyword) (Keyword, bool, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `INSERT INTO keywords(project_id,keyword,country,language,device,tags,created_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(project_id,keyword,country,language,device) DO NOTHING`, k.ProjectID, k.Keyword, k.Country, k.Language, k.Device, k.Tags, now)
	if err != nil {
		return k, false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		err := s.db.QueryRowContext(ctx, `SELECT id,created_at FROM keywords WHERE project_id=? AND keyword=? AND country=? AND language=? AND device=?`,
			k.ProjectID, k.Keyword, k.Country, k.Language, k.Device).Scan(&k.ID, &k.CreatedAt)
		return k, false, err
	}
	k.ID, _ = res.LastInsertId()
	k.CreatedAt = now
	return k, true, nil
}

// DeleteKeyword stops tracking a keyword in a project.
func (s *Store) DeleteKeyword(ctx context.Context, projectID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM keywords WHERE id=? AND project_id=?`, id, projectID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// KeywordByID reads one keyword.
func (s *Store) KeywordByID(ctx context.Context, id int64) (Keyword, error) {
	var k Keyword
	err := s.db.QueryRowContext(ctx, `SELECT id,project_id,keyword,country,language,device,tags,created_at FROM keywords WHERE id=?`, id).
		Scan(&k.ID, &k.ProjectID, &k.Keyword, &k.Country, &k.Language, &k.Device, &k.Tags, &k.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return k, ErrNotFound
	}
	return k, err
}

// Keywords lists a project's keywords with their two latest readings.
func (s *Store) Keywords(ctx context.Context, projectID int64) ([]Keyword, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,keyword,country,language,device,tags,created_at FROM keywords WHERE project_id=? ORDER BY keyword`, projectID)
	if err != nil {
		return nil, err
	}
	out := []Keyword{}
	for rows.Next() {
		var k Keyword
		if err := rows.Scan(&k.ID, &k.ProjectID, &k.Keyword, &k.Country, &k.Language, &k.Device, &k.Tags, &k.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		checks, err := s.RankChecks(ctx, out[i].ID, 2, false)
		if err != nil {
			return nil, err
		}
		if len(checks) > 0 {
			out[i].Latest = &checks[0]
		}
		if len(checks) > 1 {
			out[i].Previous = &checks[1]
		}
	}
	return out, nil
}

// AllKeywords lists every tracked keyword (for the daily schedule).
func (s *Store) AllKeywords(ctx context.Context) ([]Keyword, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,keyword,country,language,device,tags,created_at FROM keywords ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Keyword{}
	for rows.Next() {
		var k Keyword
		if err := rows.Scan(&k.ID, &k.ProjectID, &k.Keyword, &k.Country, &k.Language, &k.Device, &k.Tags, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// AddRankCheck stores a reading.
func (s *Store) AddRankCheck(ctx context.Context, c RankCheck) (RankCheck, error) {
	results, _ := json.Marshal(nonNilResults(c.Results))
	if c.Features == nil {
		c.Features = []string{}
	}
	features, _ := json.Marshal(c.Features)
	res, err := s.db.ExecContext(ctx, `INSERT INTO rank_checks(keyword_id,checked_at,position,url,depth,credits_used,cache_hit,results,features,error)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, c.KeywordID, c.CheckedAt.UTC(), c.Position, c.URL, c.Depth, c.CreditsUsed, c.CacheHit, string(results), string(features), c.Error)
	if err != nil {
		return c, err
	}
	c.ID, _ = res.LastInsertId()
	return c, nil
}

func nonNilResults(r []RankResult) []RankResult {
	if r == nil {
		return []RankResult{}
	}
	return r
}

// RankChecks lists a keyword's readings, newest first. withResults loads the
// stored page as well. Failed checks are left out.
func (s *Store) RankChecks(ctx context.Context, keywordID int64, limit int, withResults bool) ([]RankCheck, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,keyword_id,checked_at,position,url,depth,credits_used,cache_hit,results,features,error
		FROM rank_checks WHERE keyword_id=? AND error='' ORDER BY checked_at DESC, id DESC LIMIT ?`, keywordID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RankCheck{}
	for rows.Next() {
		var c RankCheck
		var pos sql.NullInt64
		var results, features string
		if err := rows.Scan(&c.ID, &c.KeywordID, &c.CheckedAt, &pos, &c.URL, &c.Depth, &c.CreditsUsed, &c.CacheHit, &results, &features, &c.Error); err != nil {
			return nil, err
		}
		if pos.Valid {
			p := int(pos.Int64)
			c.Position = &p
		}
		if withResults {
			_ = json.Unmarshal([]byte(results), &c.Results)
		}
		_ = json.Unmarshal([]byte(features), &c.Features)
		out = append(out, c)
	}
	return out, rows.Err()
}

// LastCheck is when a keyword was last checked successfully (failed=false)
// or last failed (failed=true); zero when never.
func (s *Store) LastCheck(ctx context.Context, keywordID int64, failed bool) (time.Time, error) {
	// Select the column itself (not MAX) so the driver reads it as a time.
	cond := "error=''"
	if failed {
		cond = "error<>''"
	}
	var t time.Time
	err := s.db.QueryRowContext(ctx, `SELECT checked_at FROM rank_checks WHERE keyword_id=? AND `+cond+` ORDER BY checked_at DESC LIMIT 1`, keywordID).Scan(&t)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	return t, err
}
