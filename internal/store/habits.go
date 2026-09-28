package store

import "strings"

const (
	KindBinary   = "binary"
	KindCount    = "count"
	KindDuration = "duration"

	EveryDay = 127

	SourceManual   = "manual"
	SourceOutreach = "career_outreach" // computed from outreach messages + submitted applications
)

type Habit struct {
	ID           int64
	Name         string
	Emoji        string
	Kind         string
	Target       float64
	Unit         string
	Step         float64
	ScheduleDays int
	Breakdown    string
	ReminderTime string
	Description  string
	SortOrder    int
	StartDate    string
	ArchivedAt   string
	Source       string
}

// Linked reports whether the habit's value is computed from career records
// instead of being logged by hand.
func (h Habit) Linked() bool { return h.Source == SourceOutreach }

// BreakdownKeys returns the habit's sub-counters, e.g. [easy medium hard].
func (h Habit) BreakdownKeys() []string {
	if h.Breakdown == "" {
		return nil
	}
	return strings.Split(h.Breakdown, ",")
}

func (h Habit) Archived() bool { return h.ArchivedAt != "" }

const habitCols = `id, name, COALESCE(emoji, ''), kind, target, COALESCE(unit, ''), step, schedule_days,
	COALESCE(breakdown, ''), COALESCE(reminder_time, ''), COALESCE(description, ''), sort_order, start_date,
	COALESCE(archived_at, ''), source`

func scanHabit(row interface{ Scan(...any) error }) (Habit, error) {
	var h Habit
	err := row.Scan(&h.ID, &h.Name, &h.Emoji, &h.Kind, &h.Target, &h.Unit, &h.Step, &h.ScheduleDays,
		&h.Breakdown, &h.ReminderTime, &h.Description, &h.SortOrder, &h.StartDate, &h.ArchivedAt, &h.Source)
	return h, err
}

// Habits returns habits in display order; archived ones last, and only if requested.
func (s *Store) Habits(includeArchived bool) ([]Habit, error) {
	q := `SELECT ` + habitCols + ` FROM habits`
	if !includeArchived {
		q += ` WHERE archived_at IS NULL`
	}
	q += ` ORDER BY archived_at IS NOT NULL, sort_order, id`
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hs []Habit
	for rows.Next() {
		h, err := scanHabit(rows)
		if err != nil {
			return nil, err
		}
		hs = append(hs, h)
	}
	return hs, rows.Err()
}

// Habit returns one habit, or sql.ErrNoRows.
func (s *Store) Habit(id int64) (Habit, error) {
	return scanHabit(s.db.QueryRow(`SELECT `+habitCols+` FROM habits WHERE id = ?`, id))
}

func (s *Store) CreateHabit(h *Habit) error {
	if h.Source == "" {
		h.Source = SourceManual
	}
	res, err := s.db.Exec(`INSERT INTO habits (name, emoji, kind, target, unit, step, schedule_days, breakdown,
			reminder_time, description, sort_order, start_date, created_at, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(sort_order), 0) + 10 FROM habits), ?, ?, ?)`,
		h.Name, nullIfEmpty(h.Emoji), h.Kind, h.Target, nullIfEmpty(h.Unit), h.Step, h.ScheduleDays,
		nullIfEmpty(h.Breakdown), nullIfEmpty(h.ReminderTime), nullIfEmpty(h.Description), h.StartDate, timestamp(), h.Source)
	if err != nil {
		return err
	}
	h.ID, err = res.LastInsertId()
	return err
}

// UpdateHabit saves a habit's settings. A changed target applies from today
// onwards; past entries keep the target they were logged against.
func (s *Store) UpdateHabit(h Habit, today string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`UPDATE habits SET name = ?, emoji = ?, kind = ?, target = ?, unit = ?, step = ?,
			schedule_days = ?, breakdown = ?, reminder_time = ?, description = ?, source = ?
		WHERE id = ?`,
		h.Name, nullIfEmpty(h.Emoji), h.Kind, h.Target, nullIfEmpty(h.Unit), h.Step, h.ScheduleDays,
		nullIfEmpty(h.Breakdown), nullIfEmpty(h.ReminderTime), nullIfEmpty(h.Description), h.Source, h.ID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE habit_entries SET target = ? WHERE habit_id = ? AND date >= ?`, h.Target, h.ID, today); err != nil {
		return err
	}
	if h.Linked() {
		if err := syncOutreachAll(tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SetArchived(id int64, archived bool) error {
	var at any
	if archived {
		at = timestamp()
	}
	_, err := s.db.Exec(`UPDATE habits SET archived_at = ? WHERE id = ?`, at, id)
	return err
}

// MoveHabit swaps an active habit with its neighbour; delta is -1 (up) or +1 (down).
func (s *Store) MoveHabit(id int64, delta int) error {
	hs, err := s.Habits(false)
	if err != nil {
		return err
	}
	i := -1
	for k, h := range hs {
		if h.ID == id {
			i = k
		}
	}
	j := i + delta
	if i < 0 || j < 0 || j >= len(hs) {
		return nil
	}
	hs[i], hs[j] = hs[j], hs[i]
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, h := range hs {
		if _, err := tx.Exec(`UPDATE habits SET sort_order = ? WHERE id = ?`, (k+1)*10, h.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteHabit permanently removes a habit and every day logged for it.
func (s *Store) DeleteHabit(id int64) error {
	return s.inTxQ(func(q querier) error {
		if _, err := q.Exec(`DELETE FROM habit_entries WHERE habit_id = ?`, id); err != nil {
			return err
		}
		_, err := q.Exec(`DELETE FROM habits WHERE id = ?`, id)
		return err
	})
}
