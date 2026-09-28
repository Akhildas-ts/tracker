package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"tracker/internal/stats"
	"tracker/internal/store"
)

type client struct {
	t *testing.T
	h http.Handler
}

func (c client) do(method, path, contentType, body string) *httptest.ResponseRecorder {
	c.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	return rec
}

func (c client) get(path string) string {
	c.t.Helper()
	rec := c.do("GET", path, "", "")
	if rec.Code != http.StatusOK {
		c.t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// form posts a form and expects a redirect (a successful save).
func (c client) form(path string, vals url.Values) string {
	c.t.Helper()
	rec := c.do("POST", path, "application/x-www-form-urlencoded", vals.Encode())
	if rec.Code != http.StatusSeeOther {
		c.t.Fatalf("POST %s = %d: %s", path, rec.Code, rec.Body.String())
	}
	return rec.Header().Get("Location")
}

func (c client) json(path string, body any) map[string]any {
	c.t.Helper()
	b, _ := json.Marshal(body)
	rec := c.do("POST", path, "application/json", string(b))
	if rec.Code != http.StatusOK {
		c.t.Fatalf("POST %s = %d: %s", path, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		c.t.Fatal(err)
	}
	return out
}

// metric reads a career tile's value (the first one with this label) from a page.
func metric(t *testing.T, page, label string) int {
	t.Helper()
	m := regexp.MustCompile(`<span class="metric-value">(\d+)</span><span class="metric-label">` + regexp.QuoteMeta(label) + `</span>`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("metric %q not found", label)
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func TestCareerFlowEndToEnd(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "tracker.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	today := stats.Key(stats.Today(time.Now()))
	if err := st.SeedDefaults(today); err != nil {
		t.Fatal(err)
	}
	srv, err := New(st, Options{DBPath: filepath.Join(dir, "tracker.db"), BackupDir: filepath.Join(dir, "backups")})
	if err != nil {
		t.Fatal(err)
	}
	c := client{t: t, h: srv.Routes()}

	var outreachHabit int64
	hs, _ := st.Habits(false)
	for _, h := range hs {
		if h.Linked() {
			outreachHabit = h.ID
		}
	}
	if outreachHabit == 0 {
		t.Fatal("Job Outreach is not linked on a fresh database")
	}

	// 1. Company → opportunity (research only).
	loc := c.form("/career/companies", url.Values{"name": {"Acme GmbH"}, "country": {"Germany"}})
	if !strings.HasPrefix(loc, "/career/companies/") {
		t.Fatalf("company redirect = %q", loc)
	}
	c.form("/career/opportunities", url.Values{"company": {"Acme GmbH"}, "title": {"Go Backend Engineer"},
		"go": {"core"}, "remote": {"hybrid"}, "visa": {"yes"}, "relocation": {"yes"}, "match": {"strong"}})
	if page := c.get("/career/opportunities"); !strings.Contains(page, "Go Backend Engineer") || !strings.Contains(page, "Strong Match") {
		t.Fatal("opportunity not listed")
	}
	if !strings.Contains(c.get("/today"), `<b class="sum">0</b>`) {
		t.Fatal("researching an opportunity must not count as outreach")
	}

	// 2. Application + outreach from the Career pages and the Today quick buttons.
	opps, _ := st.Opportunities(store.OpportunityFilter{})
	c.form("/career/applications", url.Values{"opportunity_id": {strconv.FormatInt(opps[0].ID, 10)}, "status": {"applied"}, "source": {"Company website"}})
	c.form("/career/outreach", url.Values{"type": {"linkedin"}, "company": {"Acme GmbH"}, "contact": {"Jane Recruiter"}, "status": {"sent"}})
	res := c.json("/api/outreach", map[string]any{"habit_id": outreachHabit, "date": today, "type": "cold_email", "company": "Beta Sp. z o.o."})
	if res["value"].(float64) != 3 {
		t.Fatalf("Job Outreach after 1 application + 2 messages = %v, want 3", res["value"])
	}

	// A manual value cannot override the career-linked count.
	res = c.json("/api/entries", map[string]any{"habit_id": outreachHabit, "date": today, "value": 99, "note": "busy day"})
	if res["value"].(float64) != 3 {
		t.Fatalf("manual override leaked: %v", res["value"])
	}

	// 3. Status change is recorded.
	apps, _ := st.Applications(store.ApplicationFilter{})
	c.form("/career/applications/"+strconv.FormatInt(apps[0].ID, 10)+"/status", url.Values{"status": {"interview"}})

	// 4. Dashboard, weekly and monthly analytics all reflect it.
	for _, path := range []string{"/", "/analytics?period=week", "/analytics?period=month"} {
		page := c.get(path)
		for label, want := range map[string]int{"Applications": 1, "Emails": 1, "LinkedIn messages": 1,
			"Companies researched": 2, "Opportunities researched": 1, "Interviews": 1, "Pending now": 1} {
			if got := metric(t, page, label); got != want {
				t.Errorf("%s: %s = %d, want %d", path, label, got, want)
			}
		}
	}
	if !strings.Contains(c.get("/"), "<b>3</b> outreach today") {
		t.Error("dashboard doesn't show today's outreach")
	}

	// Country filter: only German activity (the Polish-named email has no country).
	page := c.get("/analytics?period=week&country=Germany")
	if metric(t, page, "Emails") != 0 || metric(t, page, "LinkedIn messages") != 1 || metric(t, page, "Applications") != 1 {
		t.Error("country filter not applied")
	}

	// 5. Every page renders with data present.
	for _, path := range []string{"/today", "/career", "/career/applications", "/career/opportunities", "/career/outreach",
		"/career/companies", loc, "/career/applications/" + strconv.FormatInt(apps[0].ID, 10), "/habits", "/settings"} {
		c.get(path)
	}

	// Validation errors re-render the form instead of saving.
	if rec := c.do("POST", "/career/opportunities", "application/x-www-form-urlencoded", "title=&company="); rec.Code != http.StatusBadRequest {
		t.Errorf("empty opportunity accepted: %d", rec.Code)
	}
}

func newTestServer(t *testing.T, opts Options) (client, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "tracker.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.SeedDefaults(stats.Key(stats.Today(time.Now()))); err != nil {
		t.Fatal(err)
	}
	srv, err := New(st, opts)
	if err != nil {
		t.Fatal(err)
	}
	return client{t: t, h: srv.Routes()}, st
}

func TestRequestGuard(t *testing.T) {
	c, _ := newTestServer(t, Options{LoopbackOnly: true})
	send := func(method, host, site, origin string) int {
		req := httptest.NewRequest(method, "/settings", strings.NewReader("display_name=x"))
		req.Host = host
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if site != "" {
			req.Header.Set("Sec-Fetch-Site", site)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		c.h.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, tc := range []struct {
		name                       string
		method, host, site, origin string
		want                       int
	}{
		{"own page", "POST", "127.0.0.1:8080", "same-origin", "http://127.0.0.1:8080", http.StatusSeeOther},
		{"localhost", "GET", "localhost:8080", "", "", http.StatusOK},
		{"other website posting", "POST", "127.0.0.1:8080", "cross-site", "https://evil.example", http.StatusForbidden},
		{"old browser, foreign origin", "POST", "127.0.0.1:8080", "", "https://evil.example", http.StatusForbidden},
		{"DNS rebinding", "GET", "evil.example:8080", "", "", http.StatusForbidden},
	} {
		if got := send(tc.method, tc.host, tc.site, tc.origin); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestQuickApplicationCreatesRole(t *testing.T) {
	c, st := newTestServer(t, Options{})
	c.form("/career/applications", url.Values{"opportunity_id": {"0"}, "new_company": {"Kestrel BV"},
		"new_title": {"Go Developer"}, "new_country": {"Netherlands"}, "status": {"applied"}})
	apps, _ := st.Applications(store.ApplicationFilter{})
	if len(apps) != 1 || apps[0].Company != "Kestrel BV" || apps[0].Country != "Netherlands" || apps[0].AppliedOn == "" {
		t.Fatalf("applications = %+v", apps)
	}
	// Half-filled new role is an error, not a silent save.
	rec := c.do("POST", "/career/applications", "application/x-www-form-urlencoded", "opportunity_id=0&new_company=X&status=applied")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "both the company and the job title") {
		t.Fatalf("partial role: %d", rec.Code)
	}
	// Outreach can't point at another company's role.
	rec = c.do("POST", "/career/outreach", "application/x-www-form-urlencoded",
		url.Values{"type": {"cold_email"}, "company": {"Other Co"}, "opportunity_id": {strconv.FormatInt(apps[0].OpportunityID, 10)}}.Encode())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mismatched outreach accepted: %d", rec.Code)
	}
}

func TestFirstRunSetup(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "tracker.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv, err := New(st, Options{})
	if err != nil {
		t.Fatal(err)
	}
	c := client{t: t, h: srv.Routes()}

	// A brand-new install has no habits and starts on the welcome page.
	for _, p := range []string{"/", "/today"} {
		if rec := c.do("GET", p, "", ""); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/welcome" {
			t.Fatalf("GET %s = %d → %q, want redirect to /welcome", p, rec.Code, rec.Header().Get("Location"))
		}
	}
	if page := c.get("/welcome"); !strings.Contains(page, "Pick some starter habits") || strings.Contains(page, `href="/career"`) {
		t.Fatal("welcome page should list templates and hide navigation")
	}

	// Someone who isn't job hunting: no Career, and the career-linked template is skipped.
	c.form("/welcome", url.Values{"display_name": {"Sam"}, "habit": {"reading", "water", "job-outreach"}})
	hs, _ := st.Habits(false)
	var names []string
	for _, h := range hs {
		names = append(names, h.Name)
	}
	if strings.Join(names, ",") != "Drink water,Reading" {
		t.Fatalf("habits = %v", names)
	}
	page := c.get("/")
	if strings.Contains(page, `href="/career"`) || strings.Contains(page, "Job search this week") || !strings.Contains(page, "Good") {
		t.Fatal("career should be hidden on the dashboard")
	}
	if strings.Contains(c.get("/habits/new"), "Counted from Career") {
		t.Fatal("habit form offers the Career source while Career is off")
	}

	// Re-submitting setup never duplicates habits.
	c.form("/welcome", url.Values{"habit": {"reading"}})
	if hs2, _ := st.Habits(true); len(hs2) != 2 {
		t.Fatalf("setup re-run created duplicates: %d habits", len(hs2))
	}

	// Turn Career on with a skill: nav and relevance labels follow.
	c.form("/settings", url.Values{"display_name": {"Sam"}, "career": {"1"}, "skill": {"React"}})
	if page := c.get("/career/opportunities"); !strings.Contains(page, `href="/career"`) || !strings.Contains(page, "Any React relevance") {
		t.Fatal("career nav or skill label missing after enabling")
	}
}

func TestDeleteHabitRemovesHistory(t *testing.T) {
	c, st := newTestServer(t, Options{})
	hs, _ := st.Habits(false)
	h := hs[0]
	c.json("/api/entries", map[string]any{"habit_id": h.ID, "date": stats.Key(stats.Today(time.Now())), "value": 1})
	c.form("/habits/"+strconv.FormatInt(h.ID, 10)+"/delete", nil)
	if _, err := st.Habit(h.ID); err == nil {
		t.Fatal("habit still exists")
	}
	es, _ := st.Entries()
	for _, e := range es {
		if e.HabitID == h.ID {
			t.Fatal("entries of a deleted habit remain")
		}
	}
	if strings.Contains(c.get("/today"), h.Name) {
		t.Fatal("deleted habit still shown on Today")
	}
}
