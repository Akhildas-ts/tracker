package store

import (
	"database/sql"
	"sort"
)

// querier is satisfied by both *sql.DB and *sql.Tx.
type querier interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// The Job Outreach habit is never logged by hand. Its daily value is
// "outreach messages sent + applications submitted" on that day, written into
// habit_entries whenever career records change (and re-checked on startup),
// so every existing statistic and streak works on it unchanged. Days before
// keyOutreachSince keep whatever was logged manually before the link existed.

func outreachCount(q querier, date string) (int, error) {
	var n int
	err := q.QueryRow(`SELECT (SELECT COUNT(*) FROM outreach WHERE date = ?) +
		(SELECT COUNT(*) FROM applications WHERE applied_on = ?)`, date, date).Scan(&n)
	return n, err
}

func queryStrings(q querier, query string, args ...any) ([]string, error) {
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// syncOutreachDates recomputes linked habits' entries for the given dates.
func syncOutreachDates(q querier, dates ...string) error {
	since, err := setting(q, keyOutreachSince)
	if err != nil {
		return err
	}
	ids, err := queryStrings(q, `SELECT id FROM habits WHERE source = ?`, SourceOutreach)
	if err != nil || len(ids) == 0 {
		return err
	}
	seen := map[string]bool{}
	for _, d := range dates {
		if d == "" || d < since || seen[d] {
			continue
		}
		seen[d] = true
		n, err := outreachCount(q, d)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := q.Exec(`INSERT INTO habit_entries (habit_id, date, value, target, updated_at)
				VALUES (?, ?, ?, (SELECT target FROM habits WHERE id = ?), ?)
				ON CONFLICT (habit_id, date) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
				WHERE value <> excluded.value`, id, d, n, id, timestamp()); err != nil {
				return err
			}
			if _, err := q.Exec(`UPDATE habits SET start_date = ? WHERE id = ? AND start_date > ?`, d, id, d); err != nil {
				return err
			}
		}
	}
	return nil
}

// syncOutreachAll recomputes every date that has career activity or an existing linked entry.
func syncOutreachAll(q querier) error {
	dates, err := queryStrings(q, `SELECT date FROM outreach
		UNION SELECT applied_on FROM applications WHERE applied_on IS NOT NULL
		UNION SELECT date FROM habit_entries WHERE habit_id IN (SELECT id FROM habits WHERE source = ?)`, SourceOutreach)
	if err != nil {
		return err
	}
	sort.Strings(dates)
	return syncOutreachDates(q, dates...)
}

// ResyncOutreach rebuilds the linked habit values from career records. It is
// cheap and idempotent, and runs on startup as a consistency check.
func (s *Store) ResyncOutreach() error {
	return s.inTxQ(syncOutreachAll)
}

// OutreachDay is the breakdown of one day's Job Outreach count.
type OutreachDay struct {
	Emails, LinkedIn, Other, Applications int
}

func (d OutreachDay) Total() int { return d.Emails + d.LinkedIn + d.Other + d.Applications }

func (s *Store) OutreachOn(date string) (OutreachDay, error) {
	var d OutreachDay
	err := s.db.QueryRow(`SELECT
			COUNT(CASE WHEN type IN ('cold_email', 'recruiter_email') THEN 1 END),
			COUNT(CASE WHEN type = 'linkedin' THEN 1 END),
			COUNT(CASE WHEN type NOT IN ('cold_email', 'recruiter_email', 'linkedin') THEN 1 END),
			(SELECT COUNT(*) FROM applications WHERE applied_on = ?)
		FROM outreach WHERE date = ?`, date, date).Scan(&d.Emails, &d.LinkedIn, &d.Other, &d.Applications)
	return d, err
}

func (s *Store) inTxQ(fn func(q querier) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
