// Package store persists habits, daily entries and tasks in SQLite.
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct {
	db   *sql.DB
	path string
}

// Open opens (creating if needed) the database at path and applies pending migrations.
func Open(path string) (*Store, error) {
	// Personal data: new folders are readable by the current user only.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close checkpoints the write-ahead log into the main file and closes the database.
func (s *Store) Close() error {
	s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return s.db.Close()
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := migrationFS.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`, name, timestamp()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Backup writes a consistent copy of the database to dir/tracker-<stamp>.db
// (skipped if that file already exists) and keeps only the newest keep copies.
// The daily startup backup uses the date as stamp; manual backups add a time.
func (s *Store) Backup(dir, stamp string, keep int) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "tracker-"+stamp+".db")
	if _, err := os.Stat(dst); err == nil {
		return "", nil
	}
	if _, err := s.db.Exec(`VACUUM INTO ?`, dst); err != nil {
		return "", err
	}
	old, _ := filepath.Glob(filepath.Join(dir, "tracker-*.db"))
	sort.Strings(old)
	for len(old) > keep {
		os.Remove(old[0])
		old = old[1:]
	}
	return dst, nil
}

func timestamp() string { return time.Now().Format(time.RFC3339) }

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// Export returns every row of every table, for the JSON download.
func (s *Store) Export() (map[string][]map[string]any, error) {
	tables := []string{"habits", "habit_entries", "tasks", "companies", "contacts", "opportunities",
		"applications", "application_status_log", "outreach", "settings"}
	out := map[string][]map[string]any{}
	for _, t := range tables {
		rows, err := s.db.Query(`SELECT * FROM ` + t + ` ORDER BY rowid`)
		if err != nil {
			return nil, err
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			return nil, err
		}
		list := []map[string]any{}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				return nil, err
			}
			m := map[string]any{}
			for i, c := range cols {
				if b, ok := vals[i].([]byte); ok {
					vals[i] = string(b)
				}
				m[c] = vals[i]
			}
			list = append(list, m)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		out[t] = list
	}
	return out, nil
}
