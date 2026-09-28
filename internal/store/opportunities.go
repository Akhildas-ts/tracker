package store

type Opportunity struct {
	ID           int64
	CompanyID    int64
	Company      string
	Title        string
	Country      string // the opportunity's own country, or the company's if not set
	Location     string
	URL          string
	GoRelevance  string
	Remote       string
	Relocation   string
	Visa         string
	Experience   string
	Skills       string
	Match        string
	ResearchedOn string
	Notes        string

	// Latest application for this opportunity, if any.
	ApplicationID     int64
	ApplicationStatus string
}

type OpportunityFilter struct {
	Search, Country, Match, Go, Remote, Visa, Relocation string
	CompanyID                                            int64
}

// countryExpr is the effective country of a row joined as p (opportunity) and c (company).
const countryExpr = `COALESCE(NULLIF(p.country, ''), c.country, '')`

const opportunityCols = `p.id, p.company_id, c.name, p.title, ` + countryExpr + `, COALESCE(p.location, ''),
	COALESCE(p.url, ''), p.go_relevance, p.remote, p.relocation, p.visa, COALESCE(p.experience, ''),
	COALESCE(p.skills, ''), p.profile_match, p.researched_on, COALESCE(p.notes, ''),
	COALESCE((SELECT a.id FROM applications a WHERE a.opportunity_id = p.id ORDER BY a.id DESC LIMIT 1), 0),
	COALESCE((SELECT a.status FROM applications a WHERE a.opportunity_id = p.id ORDER BY a.id DESC LIMIT 1), '')`

func scanOpportunity(row interface{ Scan(...any) error }) (Opportunity, error) {
	var o Opportunity
	err := row.Scan(&o.ID, &o.CompanyID, &o.Company, &o.Title, &o.Country, &o.Location, &o.URL, &o.GoRelevance,
		&o.Remote, &o.Relocation, &o.Visa, &o.Experience, &o.Skills, &o.Match, &o.ResearchedOn, &o.Notes,
		&o.ApplicationID, &o.ApplicationStatus)
	return o, err
}

func (s *Store) Opportunities(f OpportunityFilter) ([]Opportunity, error) {
	rows, err := s.db.Query(`SELECT `+opportunityCols+` FROM opportunities p JOIN companies c ON c.id = p.company_id
		WHERE (p.title LIKE ?1 OR c.name LIKE ?1 OR p.skills LIKE ?1 OR p.location LIKE ?1 OR p.notes LIKE ?1)
		  AND (?2 = '' OR `+countryExpr+` = ?2)
		  AND (?3 = '' OR p.profile_match = ?3)
		  AND (?4 = '' OR p.go_relevance = ?4)
		  AND (?5 = '' OR p.remote = ?5)
		  AND (?6 = '' OR p.visa = ?6)
		  AND (?7 = '' OR p.relocation = ?7)
		  AND (?8 = 0 OR p.company_id = ?8)
		ORDER BY p.researched_on DESC, p.id DESC`,
		likeArg(f.Search), f.Country, f.Match, f.Go, f.Remote, f.Visa, f.Relocation, f.CompanyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Opportunity
	for rows.Next() {
		o, err := scanOpportunity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) Opportunity(id int64) (Opportunity, error) {
	return scanOpportunity(s.db.QueryRow(`SELECT `+opportunityCols+`
		FROM opportunities p JOIN companies c ON c.id = p.company_id WHERE p.id = ?`, id))
}

// SaveOpportunity inserts (ID == 0) or updates an opportunity. The company is
// looked up by name and created if it doesn't exist yet.
func (s *Store) SaveOpportunity(o *Opportunity, companyName string) error {
	return s.inTxQ(func(q querier) error {
		cid, err := findOrCreateCompany(q, companyName, o.Country, o.ResearchedOn)
		if err != nil {
			return err
		}
		o.CompanyID = cid
		args := []any{cid, o.Title, nullIfEmpty(o.Country), nullIfEmpty(o.Location), nullIfEmpty(o.URL), o.GoRelevance,
			o.Remote, o.Relocation, o.Visa, nullIfEmpty(o.Experience), nullIfEmpty(o.Skills), o.Match, o.ResearchedOn,
			nullIfEmpty(o.Notes)}
		if o.ID == 0 {
			res, err := q.Exec(`INSERT INTO opportunities (company_id, title, country, location, url, go_relevance, remote,
					relocation, visa, experience, skills, profile_match, researched_on, notes, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, append(args, timestamp())...)
			if err != nil {
				return err
			}
			o.ID, err = res.LastInsertId()
			return err
		}
		_, err = q.Exec(`UPDATE opportunities SET company_id = ?, title = ?, country = ?, location = ?, url = ?,
				go_relevance = ?, remote = ?, relocation = ?, visa = ?, experience = ?, skills = ?, profile_match = ?,
				researched_on = ?, notes = ?
			WHERE id = ?`, append(args, o.ID)...)
		return err
	})
}

// DeleteOpportunity refuses while applications exist for it.
func (s *Store) DeleteOpportunity(id int64) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM applications WHERE opportunity_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrInUse
	}
	_, err := s.db.Exec(`DELETE FROM opportunities WHERE id = ?`, id)
	return err
}
