package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

// Audit is one crawl of a project's site.
type Audit struct {
	ID           int64      `json:"id"`
	ProjectID    int64      `json:"project_id"`
	StartURL     string     `json:"start_url"`
	Status       string     `json:"status"` // running, done, failed, stopped
	MaxPages     int        `json:"max_pages"`
	PagesCrawled int        `json:"pages_crawled"`
	Error        string     `json:"error,omitempty"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	Score        *int       `json:"score,omitempty"`
	Counts       Counts     `json:"counts"`
}

// Counts are how many issues of each severity an audit found.
type Counts struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Notices  int `json:"notices"`
}

// AuditPage is one crawled address.
type AuditPage struct {
	URL             string `json:"url"`
	StatusCode      int    `json:"status_code"`
	ContentType     string `json:"content_type"`
	Title           string `json:"title"`
	MetaDescription string `json:"meta_description"`
	H1              string `json:"h1"`
	Canonical       string `json:"canonical"`
	Noindex         bool   `json:"noindex"`
	Words           int    `json:"words"`
	ResponseMS      int    `json:"response_ms"`
	Bytes           int    `json:"bytes"`
	Depth           int    `json:"depth"`
	RedirectTo      string `json:"redirect_to"`
	Inlinks         int    `json:"inlinks"`
}

// AuditIssue is one problem found on a page.
type AuditIssue struct {
	PageURL  string `json:"page_url"`
	Code     string `json:"code"`
	Severity string `json:"severity"` // error, warning, notice
	Detail   string `json:"detail"`
}

// StartAudit records a new running audit.
func (s *Store) StartAudit(ctx context.Context, projectID int64, startURL string, maxPages int) (Audit, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `INSERT INTO audits(project_id,start_url,status,max_pages,started_at) VALUES(?,?,?,?,?)`,
		projectID, startURL, "running", maxPages, now)
	if err != nil {
		return Audit{}, err
	}
	id, _ := res.LastInsertId()
	return Audit{ID: id, ProjectID: projectID, StartURL: startURL, Status: "running", MaxPages: maxPages, StartedAt: now}, nil
}

// AuditProgress updates the number of pages crawled so far.
func (s *Store) AuditProgress(ctx context.Context, id int64, pages int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE audits SET pages_crawled=? WHERE id=?`, pages, id)
	return err
}

// FinishAudit saves an audit's pages and issues and closes it.
func (s *Store) FinishAudit(ctx context.Context, id int64, status, errText string, pages []AuditPage, issues []AuditIssue) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	pageStmt, err := tx.PrepareContext(ctx, `INSERT INTO audit_pages(audit_id,url,status_code,content_type,title,meta_description,h1,canonical,noindex,words,response_ms,bytes,depth,redirect_to,inlinks)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer pageStmt.Close()
	for _, p := range pages {
		if _, err := pageStmt.ExecContext(ctx, id, p.URL, p.StatusCode, p.ContentType, p.Title, p.MetaDescription, p.H1, p.Canonical, p.Noindex,
			p.Words, p.ResponseMS, p.Bytes, p.Depth, p.RedirectTo, p.Inlinks); err != nil {
			return err
		}
	}
	issueStmt, err := tx.PrepareContext(ctx, `INSERT INTO audit_issues(audit_id,page_url,code,severity,detail) VALUES(?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer issueStmt.Close()
	for _, is := range issues {
		if _, err := issueStmt.ExecContext(ctx, id, is.PageURL, is.Code, is.Severity, is.Detail); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE audits SET status=?, error=?, pages_crawled=?, finished_at=? WHERE id=?`,
		status, errText, len(pages), time.Now().UTC(), id); err != nil {
		return err
	}
	return tx.Commit()
}

// FailRunningAudits closes audits left running by a restart.
func (s *Store) FailRunningAudits(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE audits SET status='failed', error='the server restarted during the crawl', finished_at=? WHERE status='running'`, time.Now().UTC())
	return err
}

const auditCols = `a.id,a.project_id,a.start_url,a.status,a.max_pages,a.pages_crawled,a.error,a.started_at,a.finished_at,
	(SELECT COUNT(*) FROM audit_issues i WHERE i.audit_id=a.id AND i.severity='error'),
	(SELECT COUNT(*) FROM audit_issues i WHERE i.audit_id=a.id AND i.severity='warning'),
	(SELECT COUNT(*) FROM audit_issues i WHERE i.audit_id=a.id AND i.severity='notice')`

func scanAudit(sc interface{ Scan(...any) error }) (Audit, error) {
	var a Audit
	var fin sql.NullTime
	if err := sc.Scan(&a.ID, &a.ProjectID, &a.StartURL, &a.Status, &a.MaxPages, &a.PagesCrawled, &a.Error, &a.StartedAt, &fin,
		&a.Counts.Errors, &a.Counts.Warnings, &a.Counts.Notices); err != nil {
		return a, err
	}
	if fin.Valid {
		a.FinishedAt = &fin.Time
	}
	if a.Status == "done" && a.PagesCrawled > 0 {
		score := HealthScore(a.PagesCrawled, a.Counts)
		a.Score = &score
	}
	return a, nil
}

// HealthScore turns issue counts into a 0-100 score. Issues are weighed
// (an error 1, a warning 0.25, a notice 0.05) and divided by the number of
// pages, so big sites are not punished for being big; the penalty then
// flattens out, so a small site with a few warnings is not scored near zero.
func HealthScore(pages int, c Counts) int {
	if pages < 1 {
		pages = 1
	}
	load := (float64(c.Errors) + float64(c.Warnings)*0.25 + float64(c.Notices)*0.05) / float64(pages)
	return int(math.Round(100 * math.Exp(-0.6*load)))
}

// Audits lists a project's audits, newest first.
func (s *Store) Audits(ctx context.Context, projectID int64, limit int) ([]Audit, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+auditCols+` FROM audits a WHERE a.project_id=? ORDER BY a.id DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Audit{}
	for rows.Next() {
		a, err := scanAudit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Audit reads one audit.
func (s *Store) Audit(ctx context.Context, id int64) (Audit, error) {
	a, err := scanAudit(s.db.QueryRowContext(ctx, `SELECT `+auditCols+` FROM audits a WHERE a.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// AuditIssues lists an audit's issues.
func (s *Store) AuditIssues(ctx context.Context, auditID int64) ([]AuditIssue, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT page_url,code,severity,detail FROM audit_issues WHERE audit_id=?
		ORDER BY CASE severity WHEN 'error' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END, code, page_url`, auditID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditIssue{}
	for rows.Next() {
		var is AuditIssue
		if err := rows.Scan(&is.PageURL, &is.Code, &is.Severity, &is.Detail); err != nil {
			return nil, err
		}
		out = append(out, is)
	}
	return out, rows.Err()
}

// AuditPages lists an audit's pages.
func (s *Store) AuditPages(ctx context.Context, auditID int64) ([]AuditPage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT url,status_code,content_type,title,meta_description,h1,canonical,noindex,words,response_ms,bytes,depth,redirect_to,inlinks
		FROM audit_pages WHERE audit_id=? ORDER BY depth, url`, auditID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditPage{}
	for rows.Next() {
		var p AuditPage
		if err := rows.Scan(&p.URL, &p.StatusCode, &p.ContentType, &p.Title, &p.MetaDescription, &p.H1, &p.Canonical, &p.Noindex,
			&p.Words, &p.ResponseMS, &p.Bytes, &p.Depth, &p.RedirectTo, &p.Inlinks); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
