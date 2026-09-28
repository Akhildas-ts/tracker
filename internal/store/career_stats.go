package store

import (
	"sort"
	"strconv"
)

// CareerMetrics counts job-search activity in a date range. Activity counts
// use the day it happened; Pending is a current snapshot.
type CareerMetrics struct {
	Applications  int // submitted (applied_on in range)
	Emails        int // cold + recruiter emails
	LinkedIn      int
	OtherMessages int // referral requests, follow-up messages, other
	FollowUps     int // follow-up messages + applications moved to Follow-up
	Companies     int // companies added/researched
	Opportunities int // opportunities researched
	Responses     int // outreach replies + applications moved to Response
	Interviews    int
	Rejections    int
	Offers        int
	Pending       int // submitted applications without a final outcome
}

// Messages is every outreach message sent.
func (m CareerMetrics) Messages() int { return m.Emails + m.LinkedIn + m.OtherMessages }

// Outreach is the Job Outreach total: messages plus submitted applications.
func (m CareerMetrics) Outreach() int { return m.Messages() + m.Applications }

// CareerMetrics computes metrics for [from, to]; country "" means all countries.
func (s *Store) CareerMetrics(from, to, country string) (CareerMetrics, error) {
	var m CareerMetrics
	scan := func(dst *int, query string, args ...any) error {
		if len(args) == 0 {
			args = []any{from, to, country}
		}
		return s.db.QueryRow(query, args...).Scan(dst)
	}
	err := s.db.QueryRow(`SELECT
			COUNT(CASE WHEN o.type IN ('cold_email', 'recruiter_email') THEN 1 END),
			COUNT(CASE WHEN o.type = 'linkedin' THEN 1 END),
			COUNT(CASE WHEN o.type IN ('referral', 'follow_up', 'other') THEN 1 END)
		`+outreachFrom+` WHERE o.date BETWEEN ?1 AND ?2 AND (?3 = '' OR `+countryExpr+` = ?3)`,
		from, to, country).Scan(&m.Emails, &m.LinkedIn, &m.OtherMessages)
	for _, q := range []struct {
		dst   *int
		query string
		args  []any
	}{
		{&m.Applications, `SELECT COUNT(*) ` + applicationFrom + `
			WHERE a.applied_on BETWEEN ?1 AND ?2 AND (?3 = '' OR ` + countryExpr + ` = ?3)`, nil},
		{&m.Pending, `SELECT COUNT(*) ` + applicationFrom + `
			WHERE a.status IN ` + pendingSQL + ` AND (?1 = '' OR ` + countryExpr + ` = ?1)`, []any{country}},
		{&m.Companies, `SELECT COUNT(*) FROM companies c WHERE c.added_on BETWEEN ?1 AND ?2 AND (?3 = '' OR c.country = ?3)`, nil},
		{&m.Opportunities, `SELECT COUNT(*) FROM opportunities p JOIN companies c ON c.id = p.company_id
			WHERE p.researched_on BETWEEN ?1 AND ?2 AND (?3 = '' OR ` + countryExpr + ` = ?3)`, nil},
		// A follow-up message and moving the same application to Follow-up on the
		// same day are one follow-up: UNION collapses identical (role, day) pairs.
		// Responses work the same way.
		{&m.FollowUps, eventCountSQL("o.type = 'follow_up'", "o.date", "follow_up"), nil},
		{&m.Responses, eventCountSQL("o.response_on IS NOT NULL", "o.response_on", "response"), nil},
		{&m.Interviews, statusCountSQL("interview"), nil},
		{&m.Rejections, statusCountSQL("rejected"), nil},
		{&m.Offers, statusCountSQL("offer"), nil},
	} {
		if err != nil {
			break
		}
		err = scan(q.dst, q.query, q.args...)
	}
	return m, err
}

// statusCountSQL counts applications moved to status in [?1, ?2] for country ?3.
func statusCountSQL(status string) string {
	return `SELECT COUNT(DISTINCT l.application_id) FROM application_status_log l
		JOIN applications a ON a.id = l.application_id
		JOIN opportunities p ON p.id = a.opportunity_id
		JOIN companies c ON c.id = p.company_id
		WHERE l.status = '` + status + `' AND l.date BETWEEN ?1 AND ?2 AND (?3 = '' OR ` + countryExpr + ` = ?3)`
}

// eventCountSQL counts distinct (role, day) pairs from matching outreach and
// from applications moved to status. Outreach not tied to a role counts on its own.
func eventCountSQL(outreachCond, outreachDate, status string) string {
	return `SELECT COUNT(*) FROM (
		SELECT COALESCE('p' || o.opportunity_id, 'o' || o.id) AS k, ` + outreachDate + ` AS d ` + outreachFrom + `
		WHERE ` + outreachCond + ` AND ` + outreachDate + ` BETWEEN ?1 AND ?2 AND (?3 = '' OR ` + countryExpr + ` = ?3)
		UNION
		SELECT 'p' || a.opportunity_id, l.date FROM application_status_log l
		JOIN applications a ON a.id = l.application_id
		JOIN opportunities p ON p.id = a.opportunity_id
		JOIN companies c ON c.id = p.company_id
		WHERE l.status = '` + status + `' AND l.date BETWEEN ?1 AND ?2 AND (?3 = '' OR ` + countryExpr + ` = ?3))`
}

// StatusCounts returns how many applications currently have each status.
func (s *Store) StatusCounts(country string) (map[string]int, error) {
	rows, err := s.db.Query(`SELECT a.status, COUNT(*) `+applicationFrom+`
		WHERE ?1 = '' OR `+countryExpr+` = ?1 GROUP BY a.status`, country)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}

// DailyOutreach returns Job Outreach totals (messages + applications) per date in [from, to].
func (s *Store) DailyOutreach(from, to, country string) (map[string]int, error) {
	rows, err := s.db.Query(`SELECT d, COUNT(*) FROM (
			SELECT o.date AS d `+outreachFrom+` WHERE o.date BETWEEN ?1 AND ?2 AND (?3 = '' OR `+countryExpr+` = ?3)
			UNION ALL
			SELECT a.applied_on `+applicationFrom+` WHERE a.applied_on BETWEEN ?1 AND ?2 AND (?3 = '' OR `+countryExpr+` = ?3)
		) GROUP BY d`, from, to, country)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var d string
		var n int
		if err := rows.Scan(&d, &n); err != nil {
			return nil, err
		}
		out[d] = n
	}
	return out, rows.Err()
}

// Activity is one entry in the recent-activity feed.
type Activity struct {
	Date    string
	Kind    string // "outreach", "status", "opportunity"
	Label   string // e.g. "LinkedIn Message", "Interview", "Researched"
	Raw     string // underlying type or status value, e.g. "linkedin", "interview"
	Title   string
	Company string
	Link    string
	order   string
}

// RecentActivity returns the newest career events across outreach,
// application status changes and researched opportunities.
func (s *Store) RecentActivity(limit int) ([]Activity, error) {
	var out []Activity
	add := func(query, kind string, label func(string) string, link string) error {
		rows, err := s.db.Query(query, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a Activity
			var id int64
			var raw string
			if err := rows.Scan(&id, &a.Date, &raw, &a.Title, &a.Company, &a.order); err != nil {
				return err
			}
			a.Kind, a.Label, a.Raw, a.Link = kind, label(raw), raw, link+strconv.FormatInt(id, 10)
			out = append(out, a)
		}
		return rows.Err()
	}
	err := add(`SELECT o.id, o.date, o.type, COALESCE(k.name, COALESCE(p.title, '')), COALESCE(c.name, ''), o.created_at`+
		outreachFrom+` ORDER BY o.date DESC, o.id DESC LIMIT ?`,
		"outreach", func(v string) string { return Label(OutreachTypes, v) }, "/career/outreach/")
	if err == nil {
		err = add(`SELECT a.id, l.date, l.status, p.title, c.name, l.created_at FROM application_status_log l
			JOIN applications a ON a.id = l.application_id JOIN opportunities p ON p.id = a.opportunity_id
			JOIN companies c ON c.id = p.company_id ORDER BY l.date DESC, l.id DESC LIMIT ?`,
			"status", func(v string) string { return Label(StatusOptions, v) }, "/career/applications/")
	}
	if err == nil {
		err = add(`SELECT p.id, p.researched_on, 'researched', p.title, c.name, p.created_at FROM opportunities p
			JOIN companies c ON c.id = p.company_id ORDER BY p.researched_on DESC, p.id DESC LIMIT ?`,
			"opportunity", func(string) string { return "Researched" }, "/career/opportunities/")
	}
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date > out[j].Date
		}
		return out[i].order > out[j].order
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
