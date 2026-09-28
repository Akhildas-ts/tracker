package store

import "encoding/json"

type Entry struct {
	HabitID   int64
	Date      string
	Value     float64
	Target    float64
	Breakdown map[string]float64
	Note      string
}

// Entries returns every habit entry, oldest first.
func (s *Store) Entries() ([]Entry, error) {
	rows, err := s.db.Query(`SELECT habit_id, date, value, target, COALESCE(breakdown, ''), COALESCE(note, '')
		FROM habit_entries ORDER BY date`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var es []Entry
	for rows.Next() {
		var e Entry
		var bd string
		if err := rows.Scan(&e.HabitID, &e.Date, &e.Value, &e.Target, &bd, &e.Note); err != nil {
			return nil, err
		}
		if bd != "" {
			if err := json.Unmarshal([]byte(bd), &e.Breakdown); err != nil {
				return nil, err
			}
		}
		es = append(es, e)
	}
	return es, rows.Err()
}

// SaveEntry creates or replaces the entry for (HabitID, Date). A new entry
// snapshots the habit's current target. Logging a day before the habit's
// start date moves the start date back so the entry counts in statistics.
//
// For a career-linked habit only the note is taken from e: the value is
// always recomputed from career records (see sync.go).
func (s *Store) SaveEntry(e Entry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var target float64
	var start, source string
	if err := tx.QueryRow(`SELECT target, start_date, source FROM habits WHERE id = ?`, e.HabitID).Scan(&target, &start, &source); err != nil {
		return err
	}
	if source == SourceOutreach {
		since, err := setting(tx, keyOutreachSince)
		if err != nil {
			return err
		}
		if e.Date >= since {
			n, err := outreachCount(tx, e.Date)
			if err != nil {
				return err
			}
			e.Value, e.Breakdown = float64(n), nil
		}
	}
	var bd any
	if len(e.Breakdown) > 0 {
		b, err := json.Marshal(e.Breakdown)
		if err != nil {
			return err
		}
		bd = string(b)
	}
	_, err = tx.Exec(`INSERT INTO habit_entries (habit_id, date, value, target, breakdown, note, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (habit_id, date) DO UPDATE SET
			value = excluded.value, breakdown = excluded.breakdown, note = excluded.note, updated_at = excluded.updated_at`,
		e.HabitID, e.Date, e.Value, target, bd, nullIfEmpty(e.Note), timestamp())
	if err != nil {
		return err
	}
	if e.Date < start {
		if _, err := tx.Exec(`UPDATE habits SET start_date = ? WHERE id = ?`, e.Date, e.HabitID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
