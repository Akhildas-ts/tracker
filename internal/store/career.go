package store

import (
	"database/sql"
	"errors"
	"strings"
)

var (
	ErrDuplicate = errors.New("a record with that name already exists")
	ErrInUse     = errors.New("record is still referenced by other records")
)

// GoRelevance is stored in the go_relevance column for historical reasons; it
// records how central the user's main skill (Settings → main skill) is to a role.

// Option is one allowed value of an enumerated field, with its display label.
type Option struct {
	Value string
	Label string
}

var (
	GoRelevanceOptions = []Option{{"core", "Main part of the role"}, {"partial", "Part of the stack"}, {"none", "Not used"}, {"unknown", "Unknown"}}
	RemoteOptions      = []Option{{"yes", "Remote"}, {"hybrid", "Hybrid"}, {"no", "On-site"}, {"unknown", "Unknown"}}
	YesNoOptions       = []Option{{"yes", "Yes"}, {"no", "No"}, {"unknown", "Unknown"}}
	MatchOptions       = []Option{{"strong", "Strong Match"}, {"possible", "Possible Match"}, {"needs_improvement", "Needs Improvement"}, {"not_match", "Not a Match"}, {"unset", "Not assessed"}}
	StatusOptions      = []Option{{"saved", "Saved"}, {"researching", "Researching"}, {"ready", "Ready to Apply"}, {"applied", "Applied"}, {"follow_up", "Follow-up"}, {"response", "Response"}, {"interview", "Interview"}, {"offer", "Offer"}, {"rejected", "Rejected"}, {"closed", "Closed"}}
	OutreachTypes      = []Option{{"cold_email", "Cold Email"}, {"recruiter_email", "Recruiter Email"}, {"linkedin", "LinkedIn Message"}, {"referral", "Referral Request"}, {"follow_up", "Follow-up"}, {"other", "Other"}}
	OutreachStatuses   = []Option{{"sent", "Sent"}, {"replied", "Replied"}, {"no_response", "No response"}, {"closed", "Closed"}}
)

func Valid(opts []Option, v string) bool {
	for _, o := range opts {
		if o.Value == v {
			return true
		}
	}
	return false
}

func Label(opts []Option, v string) string {
	for _, o := range opts {
		if o.Value == v {
			return o.Label
		}
	}
	return v
}

// submittedStatuses imply the application was sent; preSubmitStatuses mean it
// wasn't sent yet. Rejected and Closed can be either.
var (
	submittedStatuses = map[string]bool{"applied": true, "follow_up": true, "response": true, "interview": true, "offer": true}
	preSubmitStatuses = map[string]bool{"saved": true, "researching": true, "ready": true}
)

// PendingStatuses are submitted applications still waiting for a final outcome.
const pendingSQL = `('applied', 'follow_up', 'response', 'interview')`

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// likeArg turns a search string into a LIKE pattern ("" matches everything).
func likeArg(q string) string { return "%" + strings.TrimSpace(q) + "%" }

// ---- Companies ----------------------------------------------------------------

type Company struct {
	ID      int64
	Name    string
	Country string
	Website string
	Notes   string
	AddedOn string

	Opportunities int
	Applications  int
	Outreach      int
	Contacts      int
}

const companyCols = `c.id, c.name, COALESCE(c.country, ''), COALESCE(c.website, ''), COALESCE(c.notes, ''), c.added_on,
	(SELECT COUNT(*) FROM opportunities p WHERE p.company_id = c.id),
	(SELECT COUNT(*) FROM applications a JOIN opportunities p ON p.id = a.opportunity_id WHERE p.company_id = c.id),
	(SELECT COUNT(*) FROM outreach o WHERE o.company_id = c.id),
	(SELECT COUNT(*) FROM contacts k WHERE k.company_id = c.id)`

func scanCompany(row interface{ Scan(...any) error }) (Company, error) {
	var c Company
	err := row.Scan(&c.ID, &c.Name, &c.Country, &c.Website, &c.Notes, &c.AddedOn,
		&c.Opportunities, &c.Applications, &c.Outreach, &c.Contacts)
	return c, err
}

// Companies lists companies matching a search string and country ("" = any).
func (s *Store) Companies(search, country string) ([]Company, error) {
	rows, err := s.db.Query(`SELECT `+companyCols+` FROM companies c
		WHERE (c.name LIKE ?1 OR c.notes LIKE ?1 OR c.website LIKE ?1)
		  AND (?2 = '' OR c.country = ?2)
		ORDER BY c.name`, likeArg(search), country)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Company
	for rows.Next() {
		c, err := scanCompany(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Company(id int64) (Company, error) {
	return scanCompany(s.db.QueryRow(`SELECT `+companyCols+` FROM companies c WHERE c.id = ?`, id))
}

// SaveCompany inserts (ID == 0) or updates a company.
func (s *Store) SaveCompany(c *Company) error {
	var err error
	if c.ID == 0 {
		var res sql.Result
		res, err = s.db.Exec(`INSERT INTO companies (name, country, website, notes, added_on, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			c.Name, nullIfEmpty(c.Country), nullIfEmpty(c.Website), nullIfEmpty(c.Notes), c.AddedOn, timestamp())
		if err == nil {
			c.ID, err = res.LastInsertId()
		}
	} else {
		_, err = s.db.Exec(`UPDATE companies SET name = ?, country = ?, website = ?, notes = ?, added_on = ? WHERE id = ?`,
			c.Name, nullIfEmpty(c.Country), nullIfEmpty(c.Website), nullIfEmpty(c.Notes), c.AddedOn, c.ID)
	}
	if isUnique(err) {
		return ErrDuplicate
	}
	return err
}

// DeleteCompany removes a company and its contacts. It refuses while
// opportunities or outreach still point at it.
func (s *Store) DeleteCompany(id int64) error {
	c, err := s.Company(id)
	if err != nil {
		return err
	}
	if c.Opportunities > 0 || c.Outreach > 0 {
		return ErrInUse
	}
	_, err = s.db.Exec(`DELETE FROM companies WHERE id = ?`, id)
	return err
}

// findOrCreateCompany returns the id of the company with this name
// (case-insensitive), creating it if needed. An empty name returns 0.
func findOrCreateCompany(q querier, name, country, date string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, nil
	}
	var id int64
	err := q.QueryRow(`SELECT id FROM companies WHERE name = ?`, name).Scan(&id)
	if err == nil {
		// Fill in a missing country from the first record that knows it.
		_, err = q.Exec(`UPDATE companies SET country = ? WHERE id = ? AND COALESCE(country, '') = '' AND ? <> ''`, country, id, country)
		return id, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	res, err := q.Exec(`INSERT INTO companies (name, country, added_on, created_at) VALUES (?, ?, ?, ?)`,
		name, nullIfEmpty(country), date, timestamp())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Countries returns every country used on a company or opportunity, sorted.
func (s *Store) Countries() ([]string, error) {
	return queryStrings(s.db, `SELECT country FROM companies WHERE COALESCE(country, '') <> ''
		UNION SELECT country FROM opportunities WHERE COALESCE(country, '') <> '' ORDER BY 1`)
}

func (s *Store) CompanyNames() ([]string, error) {
	return queryStrings(s.db, `SELECT name FROM companies ORDER BY name`)
}

// ---- Contacts -----------------------------------------------------------------

type Contact struct {
	ID         int64
	CompanyID  int64
	Company    string
	Name       string
	Role       string
	Email      string
	ProfileURL string
	Notes      string
}

// Contacts lists contacts for a company, or all contacts when companyID is 0.
func (s *Store) Contacts(companyID int64) ([]Contact, error) {
	rows, err := s.db.Query(`SELECT k.id, k.company_id, c.name, k.name, COALESCE(k.role, ''), COALESCE(k.email, ''),
			COALESCE(k.profile_url, ''), COALESCE(k.notes, '')
		FROM contacts k JOIN companies c ON c.id = k.company_id
		WHERE ?1 = 0 OR k.company_id = ?1 ORDER BY c.name, k.name`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Contact
	for rows.Next() {
		var k Contact
		if err := rows.Scan(&k.ID, &k.CompanyID, &k.Company, &k.Name, &k.Role, &k.Email, &k.ProfileURL, &k.Notes); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) CreateContact(k *Contact) error {
	res, err := s.db.Exec(`INSERT INTO contacts (company_id, name, role, email, profile_url, notes, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		k.CompanyID, k.Name, nullIfEmpty(k.Role), nullIfEmpty(k.Email), nullIfEmpty(k.ProfileURL), nullIfEmpty(k.Notes), timestamp())
	if isUnique(err) {
		return ErrDuplicate
	}
	if err != nil {
		return err
	}
	k.ID, err = res.LastInsertId()
	return err
}

func (s *Store) DeleteContact(id int64) error {
	_, err := s.db.Exec(`DELETE FROM contacts WHERE id = ?`, id)
	return err
}

func findOrCreateContact(q querier, companyID int64, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" || companyID == 0 {
		return 0, nil
	}
	var id int64
	err := q.QueryRow(`SELECT id FROM contacts WHERE company_id = ? AND name = ?`, companyID, name).Scan(&id)
	if err == nil || !errors.Is(err, sql.ErrNoRows) {
		return id, err
	}
	res, err := q.Exec(`INSERT INTO contacts (company_id, name, created_at) VALUES (?, ?, ?)`, companyID, name, timestamp())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
