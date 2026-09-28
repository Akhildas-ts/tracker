package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tracker/internal/stats"
	"tracker/internal/store"
)

// Shared helpers for the career pages.

// field returns a trimmed form value, capped at 2000 characters.
func field(r *http.Request, name string) string {
	return truncate(strings.TrimSpace(r.FormValue(name)), 2000)
}

// dateField returns a valid YYYY-MM-DD form value, or def if blank.
func dateField(r *http.Request, name, def string, errs *[]string, label string) string {
	v := field(r, name)
	if v == "" {
		return def
	}
	if _, err := stats.ParseDay(v); err != nil {
		*errs = append(*errs, label+" must be a valid date.")
		return def
	}
	return v
}

func enumField(r *http.Request, name string, opts []store.Option, def string) string {
	if v := r.FormValue(name); store.Valid(opts, v) {
		return v
	}
	return def
}

func idField(r *http.Request, name string) int64 {
	id, _ := strconv.ParseInt(r.FormValue(name), 10, 64)
	return id
}

// loadByID runs get for the {id} in the URL, answering 404 when missing.
func loadByID[T any](s *Server, w http.ResponseWriter, r *http.Request, get func(int64) (T, error)) (T, bool) {
	var zero T
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return zero, false
	}
	v, err := get(id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return zero, false
	}
	if err != nil {
		s.fail(w, err)
		return zero, false
	}
	return v, true
}

func (s *Server) todayKey() string { return stats.Key(s.todayDate()) }

// weekRange returns Monday..Sunday of the current week.
func (s *Server) weekRange() (string, string) {
	mon := stats.Monday(s.todayDate())
	return stats.Key(mon), stats.Key(mon.AddDate(0, 0, 6))
}

// lookups are the datalist/select sources shared by the career forms.
type lookups struct {
	CompanyNames  []string
	Countries     []string
	Contacts      []store.Contact
	Opportunities []store.Opportunity
}

func (s *Server) lookups() (lookups, error) {
	var l lookups
	var err error
	if l.CompanyNames, err = s.store.CompanyNames(); err != nil {
		return l, err
	}
	if l.Countries, err = s.store.Countries(); err != nil {
		return l, err
	}
	if l.Contacts, err = s.store.Contacts(0); err != nil {
		return l, err
	}
	l.Opportunities, err = s.store.Opportunities(store.OpportunityFilter{})
	return l, err
}

// ---- Overview ---------------------------------------------------------------

type statusCount struct {
	Value, Label string
	Count        int
}

type careerView struct {
	page
	WeekLabel string
	Week      store.CareerMetrics
	Month     store.CareerMetrics
	Pipeline  []statusCount
	Pending   []store.Application
	Activity  []store.Activity
	Outreach  store.OutreachDay
	Target    float64
}

func (s *Server) careerOverview(w http.ResponseWriter, r *http.Request) {
	from, to := s.weekRange()
	today := s.todayDate()
	monthFrom := stats.Key(time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC))
	v := careerView{page: page{Nav: "career", Sub: "overview", Title: "Career"}, WeekLabel: shortDate(from) + " – " + shortDate(to)}
	var err error
	if v.Week, err = s.store.CareerMetrics(from, to, ""); err != nil {
		s.fail(w, err)
		return
	}
	if v.Month, err = s.store.CareerMetrics(monthFrom, s.todayKey(), ""); err != nil {
		s.fail(w, err)
		return
	}
	counts, err := s.store.StatusCounts("")
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, o := range store.StatusOptions {
		v.Pipeline = append(v.Pipeline, statusCount{Value: o.Value, Label: o.Label, Count: counts[o.Value]})
	}
	if v.Pending, err = s.store.Applications(store.ApplicationFilter{Pending: true}); err != nil {
		s.fail(w, err)
		return
	}
	if v.Activity, err = s.store.RecentActivity(12); err != nil {
		s.fail(w, err)
		return
	}
	if v.Outreach, err = s.store.OutreachOn(s.todayKey()); err != nil {
		s.fail(w, err)
		return
	}
	v.Target = s.outreachTarget()
	s.render(w, http.StatusOK, "career", v)
}

// outreachTarget is the daily target of the career-linked habit (0 if none).
func (s *Server) outreachTarget() float64 {
	hs, err := s.store.Habits(false)
	if err != nil {
		return 0
	}
	for _, h := range hs {
		if h.Linked() {
			return h.Target
		}
	}
	return 0
}

// ---- Companies ----------------------------------------------------------------

type companyListView struct {
	page
	Search, Country string
	Countries       []string
	Companies       []store.Company
}

func (s *Server) companyList(w http.ResponseWriter, r *http.Request) {
	v := companyListView{page: page{Nav: "career", Sub: "companies", Title: "Companies"},
		Search: r.URL.Query().Get("q"), Country: r.URL.Query().Get("country")}
	var err error
	if v.Companies, err = s.store.Companies(v.Search, v.Country); err != nil {
		s.fail(w, err)
		return
	}
	if v.Countries, err = s.store.Countries(); err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, http.StatusOK, "companies", v)
}

type companyView struct {
	page
	C             store.Company
	IsNew         bool
	Errors        []string
	Countries     []string
	Contacts      []store.Contact
	Opportunities []store.Opportunity
	Applications  []store.Application
	Outreach      []store.Outreach
}

func (s *Server) renderCompany(w http.ResponseWriter, status int, c store.Company, errs []string) {
	v := companyView{page: page{Nav: "career", Sub: "companies", Title: c.Name}, C: c, IsNew: c.ID == 0, Errors: errs}
	if v.IsNew {
		v.Title = "New company"
	}
	var err error
	if v.Countries, err = s.store.Countries(); err != nil {
		s.fail(w, err)
		return
	}
	if !v.IsNew {
		if v.Contacts, err = s.store.Contacts(c.ID); err == nil {
			if v.Opportunities, err = s.store.Opportunities(store.OpportunityFilter{CompanyID: c.ID}); err == nil {
				if v.Applications, err = s.store.Applications(store.ApplicationFilter{CompanyID: c.ID}); err == nil {
					v.Outreach, err = s.store.OutreachList(store.OutreachFilter{CompanyID: c.ID})
				}
			}
		}
		if err != nil {
			s.fail(w, err)
			return
		}
	}
	s.render(w, status, "company", v)
}

func (s *Server) newCompany(w http.ResponseWriter, r *http.Request) {
	s.renderCompany(w, http.StatusOK, store.Company{AddedOn: s.todayKey()}, nil)
}

func (s *Server) showCompany(w http.ResponseWriter, r *http.Request) {
	if c, ok := loadByID(s, w, r, s.store.Company); ok {
		s.renderCompany(w, http.StatusOK, c, nil)
	}
}

func (s *Server) saveCompany(w http.ResponseWriter, r *http.Request) {
	c := store.Company{AddedOn: s.todayKey()}
	if r.PathValue("id") != "" {
		var ok bool
		if c, ok = loadByID(s, w, r, s.store.Company); !ok {
			return
		}
	}
	var errs []string
	c.Name, c.Country, c.Website, c.Notes = field(r, "name"), field(r, "country"), field(r, "website"), field(r, "notes")
	c.AddedOn = dateField(r, "added_on", c.AddedOn, &errs, "Research date")
	if c.Name == "" {
		errs = append(errs, "Company name is required.")
	}
	if len(errs) == 0 {
		if err := s.store.SaveCompany(&c); errors.Is(err, store.ErrDuplicate) {
			errs = append(errs, "A company called “"+c.Name+"” already exists.")
		} else if err != nil {
			s.fail(w, err)
			return
		}
	}
	if len(errs) > 0 {
		s.renderCompany(w, http.StatusBadRequest, c, errs)
		return
	}
	http.Redirect(w, r, "/career/companies/"+strconv.FormatInt(c.ID, 10), http.StatusSeeOther)
}

func (s *Server) deleteCompany(w http.ResponseWriter, r *http.Request) {
	c, ok := loadByID(s, w, r, s.store.Company)
	if !ok {
		return
	}
	if err := s.store.DeleteCompany(c.ID); errors.Is(err, store.ErrInUse) {
		s.renderCompany(w, http.StatusConflict, c, []string{"This company still has opportunities or outreach. Delete those first."})
		return
	} else if err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/career/companies", http.StatusSeeOther)
}

func (s *Server) addContact(w http.ResponseWriter, r *http.Request) {
	c, ok := loadByID(s, w, r, s.store.Company)
	if !ok {
		return
	}
	k := store.Contact{CompanyID: c.ID, Name: field(r, "name"), Role: field(r, "role"), Email: field(r, "email"),
		ProfileURL: field(r, "profile_url"), Notes: field(r, "notes")}
	if k.Name == "" {
		s.renderCompany(w, http.StatusBadRequest, c, []string{"Contact name is required."})
		return
	}
	if err := s.store.CreateContact(&k); errors.Is(err, store.ErrDuplicate) {
		s.renderCompany(w, http.StatusBadRequest, c, []string{k.Name + " is already a contact at " + c.Name + "."})
		return
	} else if err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/career/companies/"+strconv.FormatInt(c.ID, 10)+"#contacts", http.StatusSeeOther)
}

func (s *Server) deleteContact(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteContact(id); err != nil {
		s.fail(w, err)
		return
	}
	back := "/career/companies"
	if cid := idField(r, "company_id"); cid > 0 {
		back += "/" + strconv.FormatInt(cid, 10) + "#contacts"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// ---- Opportunities --------------------------------------------------------------

type opportunityListView struct {
	page
	F             store.OpportunityFilter
	Countries     []string
	Opportunities []store.Opportunity
	Filtered      bool
}

func (s *Server) opportunityList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.OpportunityFilter{Search: q.Get("q"), Country: q.Get("country"), Match: q.Get("match"),
		Go: q.Get("go"), Remote: q.Get("remote"), Visa: q.Get("visa"), Relocation: q.Get("relocation")}
	v := opportunityListView{page: page{Nav: "career", Sub: "opportunities", Title: "Opportunities"}, F: f,
		Filtered: f != (store.OpportunityFilter{})}
	var err error
	if v.Opportunities, err = s.store.Opportunities(f); err != nil {
		s.fail(w, err)
		return
	}
	if v.Countries, err = s.store.Countries(); err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, http.StatusOK, "opportunities", v)
}

type opportunityFormView struct {
	page
	O           store.Opportunity
	CompanyName string
	IsNew       bool
	Errors      []string
	lookups
}

func (s *Server) renderOpportunity(w http.ResponseWriter, status int, o store.Opportunity, company string, errs []string) {
	v := opportunityFormView{page: page{Nav: "career", Sub: "opportunities", Title: "Edit opportunity"},
		O: o, CompanyName: company, IsNew: o.ID == 0, Errors: errs}
	if v.IsNew {
		v.Title = "New opportunity"
	}
	var err error
	if v.lookups, err = s.lookups(); err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, status, "opportunity_form", v)
}

func (s *Server) newOpportunity(w http.ResponseWriter, r *http.Request) {
	o := store.Opportunity{GoRelevance: "unknown", Remote: "unknown", Relocation: "unknown", Visa: "unknown",
		Match: "unset", ResearchedOn: s.todayKey()}
	s.renderOpportunity(w, http.StatusOK, o, r.URL.Query().Get("company"), nil)
}

func (s *Server) editOpportunity(w http.ResponseWriter, r *http.Request) {
	if o, ok := loadByID(s, w, r, s.store.Opportunity); ok {
		s.renderOpportunity(w, http.StatusOK, o, o.Company, nil)
	}
}

func (s *Server) saveOpportunity(w http.ResponseWriter, r *http.Request) {
	var o store.Opportunity
	if r.PathValue("id") != "" {
		var ok bool
		if o, ok = loadByID(s, w, r, s.store.Opportunity); !ok {
			return
		}
	}
	var errs []string
	company := field(r, "company")
	o.Title, o.Country, o.Location, o.URL = field(r, "title"), field(r, "country"), field(r, "location"), field(r, "url")
	o.Experience, o.Skills, o.Notes = field(r, "experience"), field(r, "skills"), field(r, "notes")
	o.GoRelevance = enumField(r, "go", store.GoRelevanceOptions, "unknown")
	o.Remote = enumField(r, "remote", store.RemoteOptions, "unknown")
	o.Relocation = enumField(r, "relocation", store.YesNoOptions, "unknown")
	o.Visa = enumField(r, "visa", store.YesNoOptions, "unknown")
	o.Match = enumField(r, "match", store.MatchOptions, "unset")
	o.ResearchedOn = dateField(r, "researched_on", s.todayKey(), &errs, "Research date")
	if company == "" {
		errs = append(errs, "Company is required.")
	}
	if o.Title == "" {
		errs = append(errs, "Job title is required.")
	}
	if len(errs) > 0 {
		s.renderOpportunity(w, http.StatusBadRequest, o, company, errs)
		return
	}
	if err := s.store.SaveOpportunity(&o, company); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/career/opportunities", http.StatusSeeOther)
}

func (s *Server) deleteOpportunity(w http.ResponseWriter, r *http.Request) {
	o, ok := loadByID(s, w, r, s.store.Opportunity)
	if !ok {
		return
	}
	if err := s.store.DeleteOpportunity(o.ID); errors.Is(err, store.ErrInUse) {
		s.renderOpportunity(w, http.StatusConflict, o, o.Company, []string{"This opportunity has an application. Delete the application first."})
		return
	} else if err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/career/opportunities", http.StatusSeeOther)
}

// ---- Applications ---------------------------------------------------------------

type applicationListView struct {
	page
	F            store.ApplicationFilter
	Countries    []string
	Pipeline     []statusCount
	Applications []store.Application
	Filtered     bool
}

func (s *Server) applicationList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.ApplicationFilter{Search: q.Get("q"), Status: q.Get("status"), Country: q.Get("country"), Pending: q.Get("pending") == "1"}
	v := applicationListView{page: page{Nav: "career", Sub: "applications", Title: "Applications"}, F: f,
		Filtered: f != (store.ApplicationFilter{})}
	var err error
	if v.Applications, err = s.store.Applications(f); err != nil {
		s.fail(w, err)
		return
	}
	if v.Countries, err = s.store.Countries(); err != nil {
		s.fail(w, err)
		return
	}
	counts, err := s.store.StatusCounts("")
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, o := range store.StatusOptions {
		if counts[o.Value] > 0 {
			v.Pipeline = append(v.Pipeline, statusCount{Value: o.Value, Label: o.Label, Count: counts[o.Value]})
		}
	}
	s.render(w, http.StatusOK, "applications", v)
}

type applicationFormView struct {
	page
	A          store.Application
	IsNew      bool
	Errors     []string
	Today      string
	NewCompany string
	NewTitle   string
	NewCountry string
	lookups
}

func (s *Server) renderApplication(w http.ResponseWriter, status int, a store.Application, errs []string) {
	s.renderApplicationWith(w, status, a, errs, "", "", "")
}

func (s *Server) renderApplicationWith(w http.ResponseWriter, status int, a store.Application, errs []string, company, title, country string) {
	v := applicationFormView{page: page{Nav: "career", Sub: "applications", Title: "Edit application"}, A: a, IsNew: a.ID == 0,
		Errors: errs, Today: s.todayKey(), NewCompany: company, NewTitle: title, NewCountry: country}
	if v.IsNew {
		v.Title = "New application"
	}
	var err error
	if v.lookups, err = s.lookups(); err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, status, "application_form", v)
}

func (s *Server) newApplication(w http.ResponseWriter, r *http.Request) {
	a := store.Application{Status: "applied", AppliedOn: s.todayKey(), OpportunityID: idField(r, "opportunity")}
	s.renderApplication(w, http.StatusOK, a, nil)
}

func (s *Server) editApplication(w http.ResponseWriter, r *http.Request) {
	if a, ok := loadByID(s, w, r, s.store.Application); ok {
		s.renderApplication(w, http.StatusOK, a, nil)
	}
}

func (s *Server) saveApplication(w http.ResponseWriter, r *http.Request) {
	var a store.Application
	if r.PathValue("id") != "" {
		var ok bool
		if a, ok = loadByID(s, w, r, s.store.Application); !ok {
			return
		}
	}
	var errs []string
	a.OpportunityID = idField(r, "opportunity_id")
	a.Status = enumField(r, "status", store.StatusOptions, "")
	a.Source, a.Notes = field(r, "source"), field(r, "notes")
	a.AppliedOn = dateField(r, "applied_on", "", &errs, "Application date")
	statusDate := dateField(r, "status_date", s.todayKey(), &errs, "Status date")
	if a.AppliedOn > s.todayKey() || statusDate > s.todayKey() {
		errs = append(errs, "Dates can't be in the future.")
	}
	// A role can be picked from Opportunities or typed in here, which also saves it as an opportunity.
	newCompany, newTitle, newCountry := field(r, "new_company"), field(r, "new_title"), field(r, "new_country")
	var newOpp *store.Opportunity
	switch {
	case a.OpportunityID > 0:
		if _, err := s.store.Opportunity(a.OpportunityID); err != nil {
			errs = append(errs, "That opportunity no longer exists.")
		}
	case newCompany != "" && newTitle != "":
		newOpp = &store.Opportunity{Title: newTitle, Country: newCountry, GoRelevance: "unknown", Remote: "unknown",
			Relocation: "unknown", Visa: "unknown", Match: "unset", ResearchedOn: statusDate}
	case newCompany != "" || newTitle != "":
		errs = append(errs, "Enter both the company and the job title for the new role.")
	default:
		errs = append(errs, "Pick a role you've researched, or enter the company and job title.")
	}
	if a.Status == "" {
		errs = append(errs, "Choose a status.")
	}
	if len(errs) > 0 {
		s.renderApplicationWith(w, http.StatusBadRequest, a, errs, newCompany, newTitle, newCountry)
		return
	}
	if newOpp != nil {
		if err := s.store.SaveOpportunity(newOpp, newCompany); err != nil {
			s.fail(w, err)
			return
		}
		a.OpportunityID = newOpp.ID
	}
	if err := s.store.SaveApplication(&a, statusDate); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/career/applications", http.StatusSeeOther)
}

// setApplicationStatus handles the inline status menus in lists.
func (s *Server) setApplicationStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	status := r.FormValue("status")
	if !ok || !store.Valid(store.StatusOptions, status) {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}
	if err := s.store.SetApplicationStatus(id, status, s.todayKey()); errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.fail(w, err)
		return
	}
	back := r.FormValue("back")
	if !strings.HasPrefix(back, "/") || strings.HasPrefix(back, "//") {
		back = "/career/applications"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (s *Server) deleteApplication(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteApplication(id); err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/career/applications", http.StatusSeeOther)
}

// ---- Outreach ---------------------------------------------------------------------

type outreachListView struct {
	page
	F         store.OutreachFilter
	Countries []string
	Items     []store.Outreach
	Filtered  bool
}

func (s *Server) outreachList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.OutreachFilter{Search: q.Get("q"), Type: q.Get("type"), Status: q.Get("status"), Country: q.Get("country")}
	v := outreachListView{page: page{Nav: "career", Sub: "outreach", Title: "Outreach"}, F: f,
		Filtered: f != (store.OutreachFilter{})}
	var err error
	if v.Items, err = s.store.OutreachList(f); err != nil {
		s.fail(w, err)
		return
	}
	if v.Countries, err = s.store.Countries(); err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, http.StatusOK, "outreach", v)
}

type outreachFormView struct {
	page
	O           store.Outreach
	CompanyName string
	ContactName string
	IsNew       bool
	Errors      []string
	lookups
}

func (s *Server) renderOutreach(w http.ResponseWriter, status int, o store.Outreach, company, contact string, errs []string) {
	v := outreachFormView{page: page{Nav: "career", Sub: "outreach", Title: "Edit outreach"},
		O: o, CompanyName: company, ContactName: contact, IsNew: o.ID == 0, Errors: errs}
	if v.IsNew {
		v.Title = "Log outreach"
	}
	var err error
	if v.lookups, err = s.lookups(); err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, status, "outreach_form", v)
}

func (s *Server) newOutreach(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	o := store.Outreach{Type: enumField(r, "type", store.OutreachTypes, "cold_email"), Date: s.todayKey(), Status: "sent",
		OpportunityID: idField(r, "opportunity")}
	company := q.Get("company")
	if o.OpportunityID > 0 && company == "" {
		if opp, err := s.store.Opportunity(o.OpportunityID); err == nil {
			company = opp.Company
		}
	}
	s.renderOutreach(w, http.StatusOK, o, company, "", nil)
}

func (s *Server) editOutreach(w http.ResponseWriter, r *http.Request) {
	if o, ok := loadByID(s, w, r, s.store.OutreachItem); ok {
		s.renderOutreach(w, http.StatusOK, o, o.Company, o.Contact, nil)
	}
}

func (s *Server) saveOutreach(w http.ResponseWriter, r *http.Request) {
	var o store.Outreach
	if r.PathValue("id") != "" {
		var ok bool
		if o, ok = loadByID(s, w, r, s.store.OutreachItem); !ok {
			return
		}
	}
	var errs []string
	company, contact := field(r, "company"), field(r, "contact")
	o.Type = enumField(r, "type", store.OutreachTypes, "")
	o.Status = enumField(r, "status", store.OutreachStatuses, "sent")
	o.Date = dateField(r, "date", s.todayKey(), &errs, "Date")
	o.ResponseOn = dateField(r, "response_on", "", &errs, "Response date")
	o.OpportunityID = idField(r, "opportunity_id")
	o.Response, o.Notes = field(r, "response"), field(r, "notes")
	if o.Type == "" {
		errs = append(errs, "Choose the outreach type.")
	}
	if o.Date > s.todayKey() {
		errs = append(errs, "Date can't be in the future.")
	}
	if o.OpportunityID > 0 {
		if opp, err := s.store.Opportunity(o.OpportunityID); err != nil {
			errs = append(errs, "That opportunity no longer exists.")
		} else if company == "" {
			company = opp.Company
		} else if !strings.EqualFold(company, opp.Company) {
			errs = append(errs, "The selected opportunity is at "+opp.Company+", not "+company+".")
		}
	}
	if contact != "" && company == "" {
		errs = append(errs, "Add the company for this contact.")
	}
	if o.Status != "replied" && o.Response == "" {
		o.ResponseOn = ""
	}
	if len(errs) > 0 {
		s.renderOutreach(w, http.StatusBadRequest, o, company, contact, errs)
		return
	}
	if err := s.store.SaveOutreach(&o, company, contact, s.todayKey()); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/career/outreach", http.StatusSeeOther)
}

func (s *Server) deleteOutreach(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteOutreach(id); err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/career/outreach", http.StatusSeeOther)
}
