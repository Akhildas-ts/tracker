package store

type Task struct {
	ID    int64
	Date  string
	Title string
	Done  bool
}

// TasksFor returns the tasks planned for date, plus (when carryOver is set)
// unfinished tasks from earlier days.
func (s *Store) TasksFor(date string, carryOver bool) ([]Task, error) {
	q := `SELECT id, date, title, done_at IS NOT NULL FROM tasks WHERE date = ?`
	args := []any{date}
	if carryOver {
		q += ` OR (date < ? AND done_at IS NULL)`
		args = append(args, date)
	}
	rows, err := s.db.Query(q+` ORDER BY date, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ts []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.Date, &t.Title, &t.Done); err != nil {
			return nil, err
		}
		ts = append(ts, t)
	}
	return ts, rows.Err()
}

// TaskCounts returns how many tasks were planned and completed per date in [from, to].
func (s *Store) TaskCounts(from, to string) (planned, done map[string]int, err error) {
	rows, err := s.db.Query(`SELECT date, COUNT(*), COUNT(done_at) FROM tasks WHERE date BETWEEN ? AND ? GROUP BY date`, from, to)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	planned, done = map[string]int{}, map[string]int{}
	for rows.Next() {
		var d string
		var p, c int
		if err := rows.Scan(&d, &p, &c); err != nil {
			return nil, nil, err
		}
		planned[d], done[d] = p, c
	}
	return planned, done, rows.Err()
}

func (s *Store) CreateTask(date, title string) (Task, error) {
	res, err := s.db.Exec(`INSERT INTO tasks (date, title, created_at) VALUES (?, ?, ?)`, date, title, timestamp())
	if err != nil {
		return Task{}, err
	}
	id, err := res.LastInsertId()
	return Task{ID: id, Date: date, Title: title}, err
}

// SetTaskDone marks a task done or not done. A carried-over task completed
// on a later day moves to that day.
func (s *Store) SetTaskDone(id int64, done bool, on string) error {
	if !done {
		_, err := s.db.Exec(`UPDATE tasks SET done_at = NULL WHERE id = ?`, id)
		return err
	}
	_, err := s.db.Exec(`UPDATE tasks SET done_at = ?, date = MAX(date, ?) WHERE id = ?`, timestamp(), on, id)
	return err
}

func (s *Store) DeleteTask(id int64) error {
	_, err := s.db.Exec(`DELETE FROM tasks WHERE id = ?`, id)
	return err
}
