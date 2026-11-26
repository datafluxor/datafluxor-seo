// Package store keeps everything the app saves in one SQLite file.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver: no C toolchain needed
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// Store wraps the database.
type Store struct {
	db *sql.DB
}

// Open opens (and creates or upgrades) the database in dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "datafluxor-seo.db")
	// The database holds the API key and sessions: readable by its owner
	// only. SQLite gives the -wal and -shm files the database file's mode, so
	// the file is created 0600 before SQLite opens it.
	if f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600); err == nil {
		f.Close()
	}
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		_ = os.Chmod(p, 0o600)
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite allows one writer; one connection keeps writes simple and safe.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("upgrade database: %w", err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle for tests.
func (s *Store) DB() *sql.DB { return s.db }

var migrations = []string{
	`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
	CREATE TABLE users (
		id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, created_at TIMESTAMP NOT NULL);
	CREATE TABLE sessions (
		token_hash TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at TIMESTAMP NOT NULL, expires_at TIMESTAMP NOT NULL);
	CREATE TABLE projects (
		id INTEGER PRIMARY KEY, name TEXT NOT NULL, domain TEXT NOT NULL, gsc_property TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL);
	CREATE TABLE audits (
		id INTEGER PRIMARY KEY, project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
		start_url TEXT NOT NULL, status TEXT NOT NULL, max_pages INTEGER NOT NULL, pages_crawled INTEGER NOT NULL DEFAULT 0,
		error TEXT NOT NULL DEFAULT '', started_at TIMESTAMP NOT NULL, finished_at TIMESTAMP);
	CREATE TABLE audit_pages (
		id INTEGER PRIMARY KEY, audit_id INTEGER NOT NULL REFERENCES audits(id) ON DELETE CASCADE,
		url TEXT NOT NULL, status_code INTEGER NOT NULL, content_type TEXT NOT NULL, title TEXT NOT NULL,
		meta_description TEXT NOT NULL, h1 TEXT NOT NULL, canonical TEXT NOT NULL, noindex INTEGER NOT NULL,
		words INTEGER NOT NULL, response_ms INTEGER NOT NULL, bytes INTEGER NOT NULL, depth INTEGER NOT NULL,
		redirect_to TEXT NOT NULL, inlinks INTEGER NOT NULL DEFAULT 0);
	CREATE INDEX audit_pages_audit ON audit_pages(audit_id);
	CREATE TABLE audit_issues (
		id INTEGER PRIMARY KEY, audit_id INTEGER NOT NULL REFERENCES audits(id) ON DELETE CASCADE,
		page_url TEXT NOT NULL, code TEXT NOT NULL, severity TEXT NOT NULL, detail TEXT NOT NULL);
	CREATE INDEX audit_issues_audit ON audit_issues(audit_id, code);
	CREATE TABLE keywords (
		id INTEGER PRIMARY KEY, project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
		keyword TEXT NOT NULL, country TEXT NOT NULL, language TEXT NOT NULL, device TEXT NOT NULL,
		tags TEXT NOT NULL DEFAULT '', created_at TIMESTAMP NOT NULL,
		UNIQUE(project_id, keyword, country, language, device));
	CREATE TABLE rank_checks (
		id INTEGER PRIMARY KEY, keyword_id INTEGER NOT NULL REFERENCES keywords(id) ON DELETE CASCADE,
		checked_at TIMESTAMP NOT NULL, position INTEGER, url TEXT NOT NULL DEFAULT '', depth INTEGER NOT NULL,
		credits_used INTEGER NOT NULL DEFAULT 0, cache_hit INTEGER NOT NULL DEFAULT 0, results TEXT NOT NULL DEFAULT '[]',
		features TEXT NOT NULL DEFAULT '[]', error TEXT NOT NULL DEFAULT '');
	CREATE INDEX rank_checks_keyword ON rank_checks(keyword_id, checked_at);
	CREATE TABLE spend (
		id INTEGER PRIMARY KEY, at TIMESTAMP NOT NULL, tool TEXT NOT NULL, credits INTEGER NOT NULL, note TEXT NOT NULL DEFAULT '');
	CREATE INDEX spend_at ON spend(at);`,
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var v int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_version`).Scan(&v); err != nil {
		return err
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version(version) VALUES (?)`, i+1); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// ---- settings ----

// Setting reads one setting; "" when unset.
func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetSetting writes one setting; an empty value deletes it.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	if value == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key=?`, key)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// ---- users and sessions ----

// User is the person who signs in.
type User struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	hash  string
}

// Hash is the stored password hash.
func (u User) Hash() string { return u.hash }

// UserCount says how many users exist (0 means first-run setup).
func (s *Store) UserCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CreateFirstUser creates the owner account, only while no user exists.
func (s *Store) CreateFirstUser(ctx context.Context, email, hash string) (User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return User{}, err
	}
	if n > 0 {
		return User{}, errors.New("setup is already complete")
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO users(email,password_hash,created_at) VALUES(?,?,?)`, email, hash, time.Now().UTC())
	if err != nil {
		return User{}, err
	}
	id, _ := res.LastInsertId()
	return User{ID: id, Email: email, hash: hash}, tx.Commit()
}

// UserByEmail finds a user.
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `SELECT id,email,password_hash FROM users WHERE email=?`, email).Scan(&u.ID, &u.Email, &u.hash)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// UserByID finds a user.
func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `SELECT id,email,password_hash FROM users WHERE id=?`, id).Scan(&u.ID, &u.Email, &u.hash)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// SetPassword replaces a user's password hash and signs out every session.
func (s *Store) SetPassword(ctx context.Context, id int64, hash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, hash, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateSession stores a session by the hash of its token.
func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, ttl time.Duration) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(token_hash,user_id,created_at,expires_at) VALUES(?,?,?,?)`, tokenHash, userID, now, now.Add(ttl))
	return err
}

// SessionUser returns the user of a live session.
func (s *Store) SessionUser(ctx context.Context, tokenHash string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.email,u.password_hash FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=? AND s.expires_at > ?`, tokenHash, time.Now().UTC()).Scan(&u.ID, &u.Email, &u.hash)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// DeleteSession signs one session out.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, tokenHash)
	return err
}

// PurgeSessions removes expired sessions.
func (s *Store) PurgeSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().UTC())
	return err
}

// ---- spend ----

// RecordSpend notes credits a call used.
func (s *Store) RecordSpend(ctx context.Context, tool string, credits int, note string) error {
	if credits <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO spend(at,tool,credits,note) VALUES(?,?,?,?)`, time.Now().UTC(), tool, credits, note)
	return err
}

// SpendSince sums credits used since t.
func (s *Store) SpendSince(ctx context.Context, t time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(credits),0) FROM spend WHERE at >= ?`, t.UTC()).Scan(&n)
	return n, err
}

// SpendRow is one recorded use of credits.
type SpendRow struct {
	At      time.Time `json:"at"`
	Tool    string    `json:"tool"`
	Credits int       `json:"credits"`
	Note    string    `json:"note"`
}

// RecentSpend lists the latest uses of credits.
func (s *Store) RecentSpend(ctx context.Context, limit int) ([]SpendRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT at,tool,credits,note FROM spend ORDER BY at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SpendRow{}
	for rows.Next() {
		var r SpendRow
		if err := rows.Scan(&r.At, &r.Tool, &r.Credits, &r.Note); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
