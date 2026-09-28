package store

type Outreach struct {
	ID            int64
	CompanyID     int64
	Company       string
	OpportunityID int64
	Opportunity   string
	ContactID     int64
	Contact       string
	Country       string
	Type          string
	Date          string
	Status        string
	Response      string
	ResponseOn    string
	Notes         string
}

type OutreachFilter struct {
	Search, Type, Status, Country string
	CompanyID                     int64
	Limit                         int
}

const outreachCols = `o.id, COALESCE(o.company_id, 0), COALESCE(c.name, ''), COALESCE(o.opportunity_id, 0),
	COALESCE(p.title, ''), COALESCE(o.contact_id, 0), COALESCE(k.name, ''), ` + countryExpr + `, o.type, o.date,
	o.status, COALESCE(o.response, ''), COALESCE(o.response_on, ''), COALESCE(o.notes, '')`

const outreachFrom = ` FROM outreach o
	LEFT JOIN companies c ON c.id = o.company_id
	LEFT JOIN opportunities p ON p.id = o.opportunity_id
	LEFT JOIN contacts k ON k.id = o.contact_id`

func scanOutreach(row interface{ Scan(...any) error }) (Outreach, error) {
	var o Outreach
	err := row.Scan(&o.ID, &o.CompanyID, &o.Company, &o.OpportunityID, &o.Opportunity, &o.ContactID, &o.Contact,
		&o.Country, &o.Type, &o.Date, &o.Status, &o.Response, &o.ResponseOn, &o.Notes)
	return o, err
}

func (s *Store) OutreachList(f OutreachFilter) ([]Outreach, error) {
	if f.Limit <= 0 {
		f.Limit = 500
	}
	rows, err := s.db.Query(`SELECT `+outreachCols+outreachFrom+`
		WHERE (COALESCE(c.name, '') LIKE ?1 OR COALESCE(k.name, '') LIKE ?1 OR COALESCE(p.title, '') LIKE ?1
		       OR COALESCE(o.notes, '') LIKE ?1 OR COALESCE(o.response, '') LIKE ?1)
		  AND (?2 = '' OR o.type = ?2)
		  AND (?3 = '' OR o.status = ?3)
		  AND (?4 = '' OR `+countryExpr+` = ?4)
		  AND (?5 = 0 OR o.company_id = ?5)
		ORDER BY o.date DESC, o.id DESC LIMIT ?6`,
		likeArg(f.Search), f.Type, f.Status, f.Country, f.CompanyID, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Outreach
	for rows.Next() {
		o, err := scanOutreach(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) OutreachItem(id int64) (Outreach, error) {
	return scanOutreach(s.db.QueryRow(`SELECT `+outreachCols+outreachFrom+` WHERE o.id = ?`, id))
}

// SaveOutreach inserts (ID == 0) or updates an outreach record. Company and
// contact are looked up by name and created if new. Marking it replied
// without a response date uses today. Job Outreach is re-synced.
func (s *Store) SaveOutreach(o *Outreach, companyName, contactName, today string) error {
	return s.inTxQ(func(q querier) error {
		cid, err := findOrCreateCompany(q, companyName, "", o.Date)
		if err != nil {
			return err
		}
		o.CompanyID = cid
		if o.ContactID, err = findOrCreateContact(q, cid, contactName); err != nil {
			return err
		}
		// A reply date only makes sense once they replied (or the thread was closed after).
		switch {
		case o.Status == "replied" && o.ResponseOn == "":
			o.ResponseOn = today
		case o.Status == "sent" || o.Status == "no_response":
			o.ResponseOn = ""
		}
		args := []any{nullID(o.CompanyID), nullID(o.OpportunityID), nullID(o.ContactID), o.Type, o.Date, o.Status,
			nullIfEmpty(o.Response), nullIfEmpty(o.ResponseOn), nullIfEmpty(o.Notes)}
		var oldDate string
		if o.ID == 0 {
			res, err := q.Exec(`INSERT INTO outreach (company_id, opportunity_id, contact_id, type, date, status, response,
					response_on, notes, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, append(args, timestamp())...)
			if err != nil {
				return err
			}
			if o.ID, err = res.LastInsertId(); err != nil {
				return err
			}
		} else {
			if err := q.QueryRow(`SELECT date FROM outreach WHERE id = ?`, o.ID).Scan(&oldDate); err != nil {
				return err
			}
			if _, err := q.Exec(`UPDATE outreach SET company_id = ?, opportunity_id = ?, contact_id = ?, type = ?, date = ?,
					status = ?, response = ?, response_on = ?, notes = ?
				WHERE id = ?`, append(args, o.ID)...); err != nil {
				return err
			}
		}
		return syncOutreachDates(q, oldDate, o.Date)
	})
}

func (s *Store) DeleteOutreach(id int64) error {
	return s.inTxQ(func(q querier) error {
		var date string
		if err := q.QueryRow(`SELECT date FROM outreach WHERE id = ?`, id).Scan(&date); err != nil {
			return err
		}
		if _, err := q.Exec(`DELETE FROM outreach WHERE id = ?`, id); err != nil {
			return err
		}
		return syncOutreachDates(q, date)
	})
}
