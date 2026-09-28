package store

type Application struct {
	ID            int64
	OpportunityID int64
	Title         string
	CompanyID     int64
	Company       string
	Country       string
	AppliedOn     string
	Source        string
	Status        string
	Notes         string
	LastChange    string // date of the most recent status change

	History []StatusChange // filled by Application only
}

type StatusChange struct {
	Status string
	Date   string
}

type ApplicationFilter struct {
	Search, Status, Country string
	Pending                 bool
	CompanyID               int64
}

const applicationCols = `a.id, a.opportunity_id, p.title, c.id, c.name, ` + countryExpr + `, COALESCE(a.applied_on, ''),
	COALESCE(a.source, ''), a.status, COALESCE(a.notes, ''),
	COALESCE((SELECT MAX(l.date) FROM application_status_log l WHERE l.application_id = a.id), '')`

const applicationFrom = ` FROM applications a
	JOIN opportunities p ON p.id = a.opportunity_id
	JOIN companies c ON c.id = p.company_id`

func scanApplication(row interface{ Scan(...any) error }) (Application, error) {
	var a Application
	err := row.Scan(&a.ID, &a.OpportunityID, &a.Title, &a.CompanyID, &a.Company, &a.Country, &a.AppliedOn,
		&a.Source, &a.Status, &a.Notes, &a.LastChange)
	return a, err
}

func (s *Store) Applications(f ApplicationFilter) ([]Application, error) {
	rows, err := s.db.Query(`SELECT `+applicationCols+applicationFrom+`
		WHERE (p.title LIKE ?1 OR c.name LIKE ?1 OR a.notes LIKE ?1 OR a.source LIKE ?1)
		  AND (?2 = '' OR a.status = ?2)
		  AND (?3 = '' OR `+countryExpr+` = ?3)
		  AND (?4 = 0 OR a.status IN `+pendingSQL+`)
		  AND (?5 = 0 OR c.id = ?5)
		ORDER BY COALESCE(a.applied_on, '9999') DESC, a.id DESC`,
		likeArg(f.Search), f.Status, f.Country, f.Pending, f.CompanyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Application
	for rows.Next() {
		a, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Application(id int64) (Application, error) {
	a, err := scanApplication(s.db.QueryRow(`SELECT `+applicationCols+applicationFrom+` WHERE a.id = ?`, id))
	if err != nil {
		return a, err
	}
	rows, err := s.db.Query(`SELECT status, date FROM application_status_log WHERE application_id = ? ORDER BY date, id`, id)
	if err != nil {
		return a, err
	}
	defer rows.Close()
	for rows.Next() {
		var c StatusChange
		if err := rows.Scan(&c.Status, &c.Date); err != nil {
			return a, err
		}
		a.History = append(a.History, c)
	}
	return a, rows.Err()
}

// SaveApplication inserts (ID == 0) or updates an application. A status
// change is logged on date (normally today). Moving to a submitted status
// without an application date sets it to date. Job Outreach is re-synced for
// the affected days.
func (s *Store) SaveApplication(a *Application, date string) error {
	return s.inTxQ(func(q querier) error {
		// Only a sent application has an application date; that date is what
		// counts toward Job Outreach.
		switch {
		case preSubmitStatuses[a.Status]:
			a.AppliedOn = ""
		case submittedStatuses[a.Status] && a.AppliedOn == "":
			a.AppliedOn = date
		}
		var oldStatus, oldApplied string
		if a.ID == 0 {
			res, err := q.Exec(`INSERT INTO applications (opportunity_id, applied_on, source, status, notes, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				a.OpportunityID, nullIfEmpty(a.AppliedOn), nullIfEmpty(a.Source), a.Status, nullIfEmpty(a.Notes), timestamp(), timestamp())
			if err != nil {
				return err
			}
			if a.ID, err = res.LastInsertId(); err != nil {
				return err
			}
		} else {
			if err := q.QueryRow(`SELECT status, COALESCE(applied_on, '') FROM applications WHERE id = ?`, a.ID).Scan(&oldStatus, &oldApplied); err != nil {
				return err
			}
			if _, err := q.Exec(`UPDATE applications SET opportunity_id = ?, applied_on = ?, source = ?, status = ?, notes = ?, updated_at = ?
				WHERE id = ?`,
				a.OpportunityID, nullIfEmpty(a.AppliedOn), nullIfEmpty(a.Source), a.Status, nullIfEmpty(a.Notes), timestamp(), a.ID); err != nil {
				return err
			}
		}
		if a.Status != oldStatus {
			// An application created as already submitted is logged on its application date.
			logDate := date
			if oldStatus == "" && submittedStatuses[a.Status] {
				logDate = a.AppliedOn
			}
			if _, err := q.Exec(`INSERT INTO application_status_log (application_id, status, date, created_at) VALUES (?, ?, ?, ?)`,
				a.ID, a.Status, logDate, timestamp()); err != nil {
				return err
			}
		}
		return syncOutreachDates(q, oldApplied, a.AppliedOn)
	})
}

// SetApplicationStatus changes only the status (used by the inline status menus).
func (s *Store) SetApplicationStatus(id int64, status, date string) error {
	a, err := s.Application(id)
	if err != nil {
		return err
	}
	a.Status = status
	return s.SaveApplication(&a, date)
}

func (s *Store) DeleteApplication(id int64) error {
	return s.inTxQ(func(q querier) error {
		var applied string
		if err := q.QueryRow(`SELECT COALESCE(applied_on, '') FROM applications WHERE id = ?`, id).Scan(&applied); err != nil {
			return err
		}
		if _, err := q.Exec(`DELETE FROM applications WHERE id = ?`, id); err != nil {
			return err
		}
		return syncOutreachDates(q, applied)
	})
}
