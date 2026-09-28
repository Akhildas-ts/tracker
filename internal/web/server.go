// Package web serves the tracker's HTML pages and JSON endpoints.
package web

import (
	"bytes"
	"embed"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"tracker/internal/stats"
	"tracker/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

type Server struct {
	store *store.Store
	opts  Options
	pages map[string]*template.Template
	now   func() time.Time
}

type Options struct {
	DBPath    string // shown on the Settings page
	BackupDir string
	// LoopbackOnly rejects requests whose Host isn't localhost/127.0.0.1/::1,
	// which blocks DNS-rebinding attacks from web pages open in the browser.
	LoopbackOnly bool
}

var pageNames = []string{
	"welcome", "dashboard", "today", "analytics", "habits", "habit_form", "settings",
	"career", "companies", "company", "opportunities", "opportunity_form",
	"applications", "application_form", "outreach", "outreach_form",
}

func New(st *store.Store, opts Options) (*Server, error) {
	s := &Server{store: st, opts: opts, pages: map[string]*template.Template{}, now: time.Now}
	fm := template.FuncMap{
		"profile":      s.profileName,
		"careerOn":     s.careerOn,
		"skill":        s.skillName,
		"profileSkill": func() string { v, _ := s.store.Setting(store.KeySkill); return v },
	}
	for k, v := range funcs {
		fm[k] = v
	}
	for _, p := range pageNames {
		t, err := template.New("").Funcs(fm).ParseFS(templateFS,
			"templates/layout.html", "templates/partials.html", "templates/"+p+".html")
		if err != nil {
			return nil, err
		}
		s.pages[p] = t
	}
	return s, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.FileServerFS(staticFS))

	mux.HandleFunc("GET /welcome", s.welcome)
	mux.HandleFunc("POST /welcome", s.finishWelcome)
	mux.HandleFunc("GET /{$}", s.dashboard)
	mux.HandleFunc("GET /today", s.today)
	mux.HandleFunc("POST /api/entries", s.saveEntry)
	mux.HandleFunc("POST /api/outreach", s.quickOutreach)
	mux.HandleFunc("POST /api/tasks", s.createTask)
	mux.HandleFunc("POST /api/tasks/{id}", s.updateTask)
	mux.HandleFunc("DELETE /api/tasks/{id}", s.deleteTask)

	mux.HandleFunc("GET /analytics", s.analytics)
	// Phase 1 URLs.
	mux.HandleFunc("GET /week", s.redirectTo("/analytics?period=week", "start"))
	mux.HandleFunc("GET /month", s.redirectTo("/analytics?period=month", "m"))

	mux.HandleFunc("GET /career", s.careerOverview)
	mux.HandleFunc("GET /career/companies", s.companyList)
	mux.HandleFunc("GET /career/companies/new", s.newCompany)
	mux.HandleFunc("POST /career/companies", s.saveCompany)
	mux.HandleFunc("GET /career/companies/{id}", s.showCompany)
	mux.HandleFunc("POST /career/companies/{id}", s.saveCompany)
	mux.HandleFunc("POST /career/companies/{id}/delete", s.deleteCompany)
	mux.HandleFunc("POST /career/companies/{id}/contacts", s.addContact)
	mux.HandleFunc("POST /career/contacts/{id}/delete", s.deleteContact)

	mux.HandleFunc("GET /career/opportunities", s.opportunityList)
	mux.HandleFunc("GET /career/opportunities/new", s.newOpportunity)
	mux.HandleFunc("POST /career/opportunities", s.saveOpportunity)
	mux.HandleFunc("GET /career/opportunities/{id}", s.editOpportunity)
	mux.HandleFunc("POST /career/opportunities/{id}", s.saveOpportunity)
	mux.HandleFunc("POST /career/opportunities/{id}/delete", s.deleteOpportunity)

	mux.HandleFunc("GET /career/applications", s.applicationList)
	mux.HandleFunc("GET /career/applications/new", s.newApplication)
	mux.HandleFunc("POST /career/applications", s.saveApplication)
	mux.HandleFunc("GET /career/applications/{id}", s.editApplication)
	mux.HandleFunc("POST /career/applications/{id}", s.saveApplication)
	mux.HandleFunc("POST /career/applications/{id}/status", s.setApplicationStatus)
	mux.HandleFunc("POST /career/applications/{id}/delete", s.deleteApplication)

	mux.HandleFunc("GET /career/outreach", s.outreachList)
	mux.HandleFunc("GET /career/outreach/new", s.newOutreach)
	mux.HandleFunc("POST /career/outreach", s.saveOutreach)
	mux.HandleFunc("GET /career/outreach/{id}", s.editOutreach)
	mux.HandleFunc("POST /career/outreach/{id}", s.saveOutreach)
	mux.HandleFunc("POST /career/outreach/{id}/delete", s.deleteOutreach)

	mux.HandleFunc("GET /habits", s.habitList)
	mux.HandleFunc("GET /habits/new", s.newHabit)
	mux.HandleFunc("POST /habits", s.createHabit)
	mux.HandleFunc("GET /habits/{id}/edit", s.editHabit)
	mux.HandleFunc("POST /habits/{id}", s.updateHabit)
	mux.HandleFunc("POST /habits/{id}/archive", s.archiveHabit)
	mux.HandleFunc("POST /habits/{id}/restore", s.restoreHabit)
	mux.HandleFunc("POST /habits/{id}/move", s.moveHabit)
	mux.HandleFunc("POST /habits/{id}/delete", s.deleteHabit)

	mux.HandleFunc("GET /settings", s.settings)
	mux.HandleFunc("POST /settings", s.saveSettings)
	mux.HandleFunc("POST /settings/backup", s.backupNow)
	mux.HandleFunc("GET /settings/export", s.export)
	return s.guard(mux)
}

// guard protects the local app from other websites open in the same browser.
// There is no login, so any page could otherwise send a form POST to
// 127.0.0.1 (cross-site request forgery) or read pages through a hostname
// that resolves to 127.0.0.1 (DNS rebinding).
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.opts.LoopbackOnly && !isLoopbackHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r) {
			http.Error(w, "cross-site request blocked", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// sameOrigin reports whether a state-changing request came from this app's own pages.
func sameOrigin(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
		// Older browsers and tools: fall back to the Origin header when present.
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			return err == nil && u.Host == r.Host
		}
		return true
	}
	return false
}

// redirectTo sends an old URL to its new home, carrying one query parameter over.
func (s *Server) redirectTo(target, param string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		to := target
		if v := r.URL.Query().Get(param); v != "" {
			to += "&" + param + "=" + url.QueryEscape(v)
		}
		http.Redirect(w, r, to, http.StatusMovedPermanently)
	}
}

// page holds the fields every page template uses.
type page struct {
	Nav   string // top-level section
	Sub   string // career sub-section
	Title string
}

func (s *Server) profileName() string {
	name, _ := s.store.Setting(store.KeyDisplayName)
	return name
}

// careerOn reports whether job-search tracking is shown (it is unless turned off).
func (s *Server) careerOn() bool {
	v, _ := s.store.Setting(store.KeyCareer)
	return v != "0"
}

// skillName is the user's main skill, used to label how relevant a role is to it.
func (s *Server) skillName() string {
	if v, _ := s.store.Setting(store.KeySkill); v != "" {
		return v
	}
	return "Main skill"
}

// needsSetup sends a brand-new install to the welcome page; it reports
// whether it did.
func (s *Server) needsSetup(w http.ResponseWriter, r *http.Request) bool {
	if done, _ := s.store.Setting(store.KeySetupDone); done == "1" {
		return false
	}
	http.Redirect(w, r, "/welcome", http.StatusSeeOther)
	return true
}

func (s *Server) render(w http.ResponseWriter, status int, name string, data any) {
	var buf bytes.Buffer
	if err := s.pages[name].ExecuteTemplate(&buf, "layout", data); err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	log.Printf("error: %v", err)
	http.Error(w, "Something went wrong. Check the server log.", http.StatusInternalServerError)
}

func (s *Server) todayDate() time.Time { return stats.Today(s.now()) }

// dateParam parses a YYYY-MM-DD query parameter, defaulting to today and
// never going past today.
func (s *Server) dateParam(r *http.Request, name string) time.Time {
	today := s.todayDate()
	d, err := stats.ParseDay(r.URL.Query().Get(name))
	if err != nil || d.After(today) {
		return today
	}
	return d
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil
}

// habitData is an active habit together with all of its entries.
type habitData struct {
	store.Habit
	sched stats.Habit
	raw   map[string]store.Entry
	ent   stats.Entries
}

func (s *Server) load() ([]*habitData, error) {
	hs, err := s.store.Habits(false)
	if err != nil {
		return nil, err
	}
	es, err := s.store.Entries()
	if err != nil {
		return nil, err
	}
	out := make([]*habitData, 0, len(hs))
	byID := map[int64]*habitData{}
	for _, h := range hs {
		start, err := stats.ParseDay(h.StartDate)
		if err != nil {
			return nil, err
		}
		hd := &habitData{
			Habit: h,
			sched: stats.Habit{ScheduleDays: h.ScheduleDays, Start: start},
			raw:   map[string]store.Entry{},
			ent:   stats.Entries{},
		}
		out = append(out, hd)
		byID[h.ID] = hd
	}
	for _, e := range es {
		if hd, ok := byID[e.HabitID]; ok {
			hd.raw[e.Date] = e
			hd.ent[e.Date] = stats.Entry{Value: e.Value, Target: e.Target}
		}
	}
	return out, nil
}

type score struct {
	Completed int
	Total     int
	Progress  float64
}

// dayScore scores the habits scheduled on d, giving partial credit for partial progress.
func dayScore(hs []*habitData, d time.Time) score {
	var sc score
	var sum float64
	for _, h := range hs {
		if !h.sched.ScheduledOn(d) {
			continue
		}
		p, done := h.ent.On(d)
		sc.Total++
		sum += p
		if done {
			sc.Completed++
		}
	}
	if sc.Total > 0 {
		sc.Progress = sum / float64(sc.Total)
	}
	return sc
}

// Option lists available to every template.
func (page) MatchOpts() []store.Option  { return store.MatchOptions }
func (page) GoOpts() []store.Option     { return store.GoRelevanceOptions }
func (page) RemoteOpts() []store.Option { return store.RemoteOptions }
func (page) YesNo() []store.Option      { return store.YesNoOptions }
func (page) StatusOpts() []store.Option { return store.StatusOptions }
func (page) TypeOpts() []store.Option   { return store.OutreachTypes }
func (page) ReplyOpts() []store.Option  { return store.OutreachStatuses }
