// Package history persists projected household activity and automation pause
// state. It never stores raw Matter frames or request bodies.
package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("history path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create history directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open history: %w", err)
	}
	db.SetMaxOpenConns(1)
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("read history schema version: %w", err)
	}
	if version > 1 {
		_ = db.Close()
		return nil, fmt.Errorf("history schema version %d is newer than this binary", version)
	}
	for _, statement := range []string{
		"PRAGMA busy_timeout=5000",
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=FULL",
		`CREATE TABLE IF NOT EXISTS history (
			id INTEGER PRIMARY KEY,
			at_ns INTEGER NOT NULL,
			kind TEXT NOT NULL,
			summary TEXT NOT NULL,
			node_id TEXT NOT NULL DEFAULT '',
			device_label TEXT NOT NULL DEFAULT '',
			endpoint INTEGER,
			file TEXT NOT NULL DEFAULT '',
			revision TEXT NOT NULL DEFAULT '',
			rule INTEGER NOT NULL DEFAULT 0,
			run_id TEXT NOT NULL DEFAULT '',
			outcome TEXT NOT NULL DEFAULT '',
			details BLOB
		)`,
		`CREATE TABLE IF NOT EXISTS history_devices (
			entry_id INTEGER NOT NULL REFERENCES history(id),
			node_id TEXT NOT NULL,
			PRIMARY KEY(entry_id, node_id)
		)`,
		"CREATE INDEX IF NOT EXISTS history_at ON history(at_ns, id)",
		"CREATE INDEX IF NOT EXISTS history_file ON history(file, id)",
		"CREATE INDEX IF NOT EXISTS history_run ON history(run_id, id)",
		"CREATE INDEX IF NOT EXISTS history_devices_node ON history_devices(node_id, entry_id)",
		`CREATE TABLE IF NOT EXISTS automation_pauses (
			file TEXT PRIMARY KEY,
			paused INTEGER NOT NULL,
			updated_ns INTEGER NOT NULL
		)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("initialize history: %w", err)
		}
	}
	if version == 0 {
		if _, err := db.Exec("PRAGMA user_version=1"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("set history schema version: %w", err)
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Append(ctx context.Context, entry api.HistoryEntry) (api.HistoryEntry, error) {
	if entry.ObservedAt.IsZero() {
		entry.ObservedAt = time.Now().UTC()
	}
	if entry.Kind == "" || entry.Summary == "" {
		return entry, errors.New("history kind and summary are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return entry, err
	}
	defer func() { _ = tx.Rollback() }()
	var endpoint any
	if entry.Endpoint != nil {
		endpoint = int64(*entry.Endpoint)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO history
		(at_ns,kind,summary,node_id,device_label,endpoint,file,revision,rule,run_id,outcome,details)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		entry.ObservedAt.UnixNano(), entry.Kind, entry.Summary, entry.NodeID,
		entry.DeviceLabel, endpoint, entry.File, entry.Revision, entry.Rule,
		entry.RunID, entry.Outcome, []byte(entry.Details))
	if err != nil {
		return entry, err
	}
	entry.ID, err = result.LastInsertId()
	if err != nil {
		return entry, err
	}
	if entry.NodeID != "" && !slices.Contains(entry.RelatedNodes, entry.NodeID) {
		entry.RelatedNodes = append(entry.RelatedNodes, entry.NodeID)
	}
	slices.Sort(entry.RelatedNodes)
	entry.RelatedNodes = slices.Compact(entry.RelatedNodes)
	for _, id := range entry.RelatedNodes {
		if _, err := tx.ExecContext(ctx, "INSERT INTO history_devices(entry_id,node_id) VALUES (?,?)", entry.ID, id); err != nil {
			return entry, err
		}
	}
	if err := tx.Commit(); err != nil {
		return entry, err
	}
	return entry, nil
}

func (s *Store) Query(ctx context.Context, query api.HistoryQuery) (api.HistoryPage, error) {
	if query.Limit <= 0 || query.Limit > 100 {
		query.Limit = 50
	}
	var sqlText strings.Builder
	sqlText.WriteString(`SELECT id,at_ns,kind,summary,node_id,device_label,endpoint,file,revision,rule,run_id,outcome,details
		FROM history WHERE 1=1`)
	args := make([]any, 0, 8)
	add := func(condition string, value any) {
		sqlText.WriteString(" AND " + condition)
		args = append(args, value)
	}
	if !query.Since.IsZero() {
		add("at_ns>=?", query.Since.UnixNano())
	}
	if !query.Until.IsZero() {
		add("at_ns<=?", query.Until.UnixNano())
	}
	if query.Device != "" {
		add("EXISTS (SELECT 1 FROM history_devices d WHERE d.entry_id=history.id AND d.node_id=?)", string(query.Device))
	}
	if query.File != "" {
		add("file=?", query.File)
	}
	if query.Kind != "" {
		add("kind=?", query.Kind)
	}
	if query.Outcome != "" {
		add("outcome=?", query.Outcome)
	}
	if query.RunID != "" {
		add("run_id=?", query.RunID)
	}
	if query.Before > 0 {
		add("id<?", query.Before)
	}
	if query.After > 0 {
		add("id>?", query.After)
		sqlText.WriteString(" ORDER BY id ASC LIMIT ?")
	} else {
		sqlText.WriteString(" ORDER BY id DESC LIMIT ?")
	}
	args = append(args, query.Limit+1)
	rows, err := s.db.QueryContext(ctx, sqlText.String(), args...)
	if err != nil {
		return api.HistoryPage{}, err
	}
	defer func() { _ = rows.Close() }()
	page := api.HistoryPage{Entries: []api.HistoryEntry{}}
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return page, err
		}
		page.Entries = append(page.Entries, entry)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if err := rows.Close(); err != nil {
		return page, err
	}
	if len(page.Entries) > query.Limit {
		page.Entries = page.Entries[:query.Limit]
		page.Next = page.Entries[len(page.Entries)-1].ID
	}
	for i := range page.Entries {
		page.Entries[i].RelatedNodes, err = s.related(ctx, page.Entries[i].ID)
		if err != nil {
			return page, err
		}
	}
	return page, nil
}

func (s *Store) Get(ctx context.Context, id int64) (api.HistoryEntry, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,at_ns,kind,summary,node_id,device_label,endpoint,file,revision,rule,run_id,outcome,details
		FROM history WHERE id=?`, id)
	entry, err := scanEntry(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return entry, api.ErrNotFound
		}
		return entry, err
	}
	entry.RelatedNodes, err = s.related(ctx, id)
	return entry, err
}

type scanner interface{ Scan(...any) error }

func scanEntry(row scanner) (api.HistoryEntry, error) {
	var entry api.HistoryEntry
	var at int64
	var endpoint sql.NullInt64
	var details []byte
	err := row.Scan(&entry.ID, &at, &entry.Kind, &entry.Summary, &entry.NodeID,
		&entry.DeviceLabel, &endpoint, &entry.File, &entry.Revision, &entry.Rule,
		&entry.RunID, &entry.Outcome, &details)
	if err != nil {
		return entry, err
	}
	entry.ObservedAt = time.Unix(0, at).UTC()
	if endpoint.Valid {
		if endpoint.Int64 < 0 || endpoint.Int64 > 65535 {
			return entry, fmt.Errorf("invalid stored endpoint %d", endpoint.Int64)
		}
		value := uint16(endpoint.Int64)
		entry.Endpoint = &value
	}
	entry.Details = details
	return entry, nil
}

func (s *Store) related(ctx context.Context, id int64) ([]api.NodeID, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT node_id FROM history_devices WHERE entry_id=? ORDER BY node_id", id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []api.NodeID
	for rows.Next() {
		var node api.NodeID
		if err := rows.Scan(&node); err != nil {
			return nil, err
		}
		ids = append(ids, node)
	}
	return ids, rows.Err()
}

func validFile(name string) bool {
	return name != "" && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\\x00") && strings.HasSuffix(name, ".star")
}

func (s *Store) SetPaused(ctx context.Context, file string, paused bool) error {
	if !validFile(file) {
		return api.ErrInvalidRequest
	}
	value := 0
	if paused {
		value = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO automation_pauses(file,paused,updated_ns)
		VALUES (?,?,?) ON CONFLICT(file) DO UPDATE SET paused=excluded.paused,updated_ns=excluded.updated_ns`,
		file, value, time.Now().UTC().UnixNano())
	return err
}

func (s *Store) PausedFiles(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT file,paused FROM automation_pauses")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := map[string]bool{}
	for rows.Next() {
		var file string
		var paused bool
		if err := rows.Scan(&file, &paused); err != nil {
			return nil, err
		}
		result[file] = paused
	}
	return result, rows.Err()
}
