// Package sqlite provides a SQLite-backed implementation of
// stem.Store. Envelopes are persisted in crtx v0.1 shape.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"hop.top/stem"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const timeLayout = time.RFC3339Nano

// Compile-time interface check.
var _ stem.Store = (*Store)(nil)

// Store implements stem.Store backed by SQLite via modernc.org/sqlite.
type Store struct {
	db *sql.DB
}

// New opens (or creates) a SQLite database at path and runs
// migrations. Use ":memory:" for an in-memory database.
func New(path string) (*Store, error) {
	db, err := openDB(path)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open: %w", err)
	}

	// For in-memory databases, restrict to a single connection to
	// prevent data loss across multiple connections.
	if path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
		db.SetMaxOpenConns(1)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// openDB sets up a modernc sqlite DSN with sane pragmas applied to
// every pooled connection.
func openDB(path string) (*sql.DB, error) {
	if path == "" {
		return nil, errors.New("sqlite: path required")
	}
	if path != ":memory:" && !strings.HasPrefix(path, "file::memory:") {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("sqlite: mkdir: %w", err)
		}
	}

	dsn, err := buildDSN(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func buildDSN(path string) (string, error) {
	pragmas := []string{
		"busy_timeout=5000",
		"foreign_keys=on",
		"journal_mode=WAL",
	}
	base, query := path, ""
	if i := strings.Index(path, "?"); i >= 0 {
		base, query = path[:i], path[i+1:]
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		return "", err
	}
	for _, p := range pragmas {
		values.Add("_pragma", p)
	}
	return base + "?" + values.Encode(), nil
}

func (s *Store) migrate() error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("sqlite: read migrations: %w", err)
	}
	for _, e := range entries {
		data, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return fmt.Errorf("sqlite: read %s: %w", e.Name(), err)
		}
		if _, err := s.db.Exec(string(data)); err != nil {
			return fmt.Errorf("sqlite: exec %s: %w", e.Name(), err)
		}
	}
	return nil
}

// Create persists a new session from metadata.
func (s *Store) Create(ctx context.Context, meta stem.SessionMeta) error {
	if meta.Source.Kind == "" {
		meta.Source = stem.DefaultSource()
	}
	metaJSON, err := json.Marshal(meta.Metadata)
	if err != nil {
		return fmt.Errorf("sqlite: marshal metadata: %w", err)
	}

	createdAt := meta.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := meta.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sessions
		 (id, crtx_version, source_kind, source_ver, source_inst,
		  parent_id, fork_point, metadata, created_at, updated_at, closed_at)
		 VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, NULL)`,
		meta.ID,
		stem.CrtxVersion,
		meta.Source.Kind,
		meta.Source.Version,
		meta.Source.Instance,
		meta.ParentID,
		string(metaJSON),
		createdAt.Format(timeLayout),
		updatedAt.Format(timeLayout),
	)
	if err != nil {
		return fmt.Errorf("sqlite: insert session: %w", err)
	}
	return nil
}

// Load retrieves a session and all its turns ordered by seq.
func (s *Store) Load(ctx context.Context, id string) (*stem.Session, error) {
	sess := &stem.Session{}
	var crtxVer, srcKind, srcVer, srcInst, metaStr string
	var createdStr, updatedStr string
	var closedStr sql.NullString
	var forkPoint sql.NullInt64

	err := s.db.QueryRowContext(ctx,
		`SELECT id, crtx_version, source_kind, source_ver, source_inst,
		        parent_id, fork_point, metadata,
		        created_at, updated_at, closed_at
		 FROM sessions WHERE id = ?`, id,
	).Scan(&sess.ID, &crtxVer, &srcKind, &srcVer, &srcInst,
		&sess.ParentID, &forkPoint, &metaStr,
		&createdStr, &updatedStr, &closedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, stem.ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: load session: %w", err)
	}

	sess.CrtxVersion = crtxVer
	sess.Source = stem.Source{Kind: srcKind, Version: srcVer, Instance: srcInst}
	if forkPoint.Valid {
		fp := int(forkPoint.Int64)
		sess.ForkPoint = &fp
	}

	if err := json.Unmarshal([]byte(metaStr), &sess.Metadata); err != nil {
		return nil, fmt.Errorf("sqlite: unmarshal metadata: %w", err)
	}
	sess.CreatedAt, err = time.Parse(timeLayout, createdStr)
	if err != nil {
		return nil, fmt.Errorf("sqlite: parse created_at: %w", err)
	}
	sess.UpdatedAt, err = time.Parse(timeLayout, updatedStr)
	if err != nil {
		return nil, fmt.Errorf("sqlite: parse updated_at: %w", err)
	}
	if closedStr.Valid {
		t, perr := time.Parse(timeLayout, closedStr.String)
		if perr != nil {
			return nil, fmt.Errorf("sqlite: parse closed_at: %w", perr)
		}
		sess.ClosedAt = &t
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, role, content, metadata, created_at
		 FROM turns WHERE session_id = ? ORDER BY seq ASC`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: query turns: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		t, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		sess.Turns = append(sess.Turns, t)
	}
	return sess, rows.Err()
}

// AppendTurn inserts a turn with auto-incremented seq.
func (s *Store) AppendTurn(ctx context.Context, sessionID string, turn stem.Turn) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Verify session exists and is not closed.
	var closedStr sql.NullString
	err = tx.QueryRow(
		`SELECT closed_at FROM sessions WHERE id = ?`, sessionID,
	).Scan(&closedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return stem.ErrSessionNotFound
	}
	if err != nil {
		return fmt.Errorf("sqlite: check session: %w", err)
	}
	if closedStr.Valid {
		return stem.ErrSessionClosed
	}

	if err := insertTurnAutoSeq(tx, sessionID, turn); err != nil {
		return err
	}

	_, err = tx.Exec(
		`UPDATE sessions SET updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(timeLayout), sessionID,
	)
	if err != nil {
		return fmt.Errorf("sqlite: update timestamp: %w", err)
	}

	return tx.Commit()
}

// List returns session metadata matching the filter.
func (s *Store) List(ctx context.Context, f stem.Filter) ([]stem.SessionMeta, error) {
	var where []string
	var args []any

	if f.ParentID != "" {
		where = append(where, "s.parent_id = ?")
		args = append(args, f.ParentID)
	}

	zero := time.Time{}
	if f.Before != zero {
		where = append(where, "s.created_at < ?")
		args = append(args, f.Before.Format(timeLayout))
	}
	if f.After != zero {
		where = append(where, "s.created_at > ?")
		args = append(args, f.After.Format(timeLayout))
	}

	query := `SELECT s.id, s.metadata, s.parent_id, s.created_at, s.updated_at,
	                 s.source_kind, s.source_ver, s.source_inst,
	                 COUNT(t.session_id) as turn_count
	          FROM sessions s
	          LEFT JOIN turns t ON t.session_id = s.id`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " GROUP BY s.id ORDER BY s.created_at DESC"

	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	if f.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", f.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var metas []stem.SessionMeta
	for rows.Next() {
		var m stem.SessionMeta
		var metaStr, createdStr, updatedStr string
		var srcKind, srcVer, srcInst string

		if err := rows.Scan(&m.ID, &metaStr, &m.ParentID,
			&createdStr, &updatedStr,
			&srcKind, &srcVer, &srcInst,
			&m.TurnCount); err != nil {
			return nil, fmt.Errorf("sqlite: scan meta: %w", err)
		}

		if err := json.Unmarshal([]byte(metaStr), &m.Metadata); err != nil {
			return nil, fmt.Errorf("sqlite: unmarshal meta: %w", err)
		}
		m.CreatedAt, err = time.Parse(timeLayout, createdStr)
		if err != nil {
			return nil, fmt.Errorf("sqlite: parse created_at: %w", err)
		}
		m.UpdatedAt, err = time.Parse(timeLayout, updatedStr)
		if err != nil {
			return nil, fmt.Errorf("sqlite: parse updated_at: %w", err)
		}
		m.Source = stem.Source{Kind: srcKind, Version: srcVer, Instance: srcInst}

		metas = append(metas, m)
	}
	return metas, rows.Err()
}

// Delete removes a session and its turns (cascaded by FK).
func (s *Store) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite: delete session: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return stem.ErrSessionNotFound
	}
	return nil
}

// ListTurns returns turns for the given session matching the filter.
func (s *Store) ListTurns(ctx context.Context, sessionID string, f stem.TurnFilter) ([]stem.Turn, error) {
	q := `SELECT id, role, content, metadata, created_at FROM turns WHERE session_id = ?`
	args := []any{sessionID}

	if f.Role != "" {
		q += ` AND role = ?`
		args = append(args, string(f.Role))
	}
	if !f.After.IsZero() {
		q += ` AND created_at > ?`
		args = append(args, f.After.Format(time.RFC3339Nano))
	}
	if !f.Before.IsZero() {
		q += ` AND created_at < ?`
		args = append(args, f.Before.Format(time.RFC3339Nano))
	}
	q += ` ORDER BY seq ASC`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	}
	if f.Offset > 0 {
		q += fmt.Sprintf(` OFFSET %d`, f.Offset)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list turns: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var turns []stem.Turn
	for rows.Next() {
		t, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		turns = append(turns, t)
	}
	return turns, rows.Err()
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func insertTurnAutoSeq(tx *sql.Tx, sessionID string, t stem.Turn) error {
	contentBytes, err := json.Marshal(t.Content)
	if err != nil {
		return fmt.Errorf("sqlite: marshal content: %w", err)
	}
	metaBytes, err := json.Marshal(t.Metadata)
	if err != nil {
		return fmt.Errorf("sqlite: marshal turn metadata: %w", err)
	}

	createdAt := t.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	_, err = tx.Exec(
		`INSERT INTO turns (id, session_id, role, content, metadata, created_at, seq)
		 VALUES (?, ?, ?, ?, ?, ?,
		         (SELECT COALESCE(MAX(seq), -1) + 1 FROM turns WHERE session_id = ?))`,
		t.ID, sessionID, string(t.Role),
		string(contentBytes), string(metaBytes),
		createdAt.Format(timeLayout), sessionID,
	)
	if err != nil {
		return fmt.Errorf("sqlite: insert turn: %w", err)
	}
	return nil
}

func scanTurn(rows *sql.Rows) (stem.Turn, error) {
	var t stem.Turn
	var roleStr string
	var contentStr, metaStr, createdStr string

	err := rows.Scan(&t.ID, &roleStr, &contentStr, &metaStr, &createdStr)
	if err != nil {
		return t, fmt.Errorf("sqlite: scan turn: %w", err)
	}

	t.Role = stem.Role(roleStr)
	t.CreatedAt, err = time.Parse(timeLayout, createdStr)
	if err != nil {
		return t, fmt.Errorf("sqlite: parse turn created_at: %w", err)
	}

	if err := json.Unmarshal([]byte(contentStr), &t.Content); err != nil {
		return t, fmt.Errorf("sqlite: unmarshal content: %w", err)
	}
	if err := json.Unmarshal([]byte(metaStr), &t.Metadata); err != nil {
		return t, fmt.Errorf("sqlite: unmarshal metadata: %w", err)
	}

	return t, nil
}
