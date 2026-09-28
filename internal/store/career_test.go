package store

import (
	"errors"
	"path/filepath"
	"testing"
)

const day1, day2 = "2026-09-21", "2026-09-22"

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.SeedDefaults(day1); err != nil {
		t.Fatal(err)
	}
	// Career linking normally starts on the migration day; move it back so the test dates count.
	if err := s.SetSetting(keyOutreachSince, "2000-01-01"); err != nil {
		t.Fatal(err)
	}
	return s
}

func outreachHabit(t *testing.T, s *Store) Habit {
	t.Helper()
	hs, err := s.Habits(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hs {
		if h.Linked() {
			return h
		}
	}
	t.Fatal("no linked habit seeded")
	return Habit{}
}

// outreachValue returns the Job Outreach entry value on date (-1 if none).
func outreachValue(t *testing.T, s *Store, date string) float64 {
	t.Helper()
	h := outreachHabit(t, s)
	es, err := s.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		if e.HabitID == h.ID && e.Date == date {
			return e.Value
		}
	}
	return -1
}

func TestCareerFlow(t *testing.T) {
	s := openTest(t)

	opp := Opportunity{Title: "Backend Engineer (Go)", Country: "Germany", GoRelevance: "core", Remote: "hybrid",
		Relocation: "yes", Visa: "yes", Match: "strong", ResearchedOn: day1}
	if err := s.SaveOpportunity(&opp, "Acme"); err != nil {
		t.Fatal(err)
	}
	c, err := s.Company(opp.CompanyID)
	if err != nil || c.Name != "Acme" || c.Country != "Germany" {
		t.Fatalf("company auto-created wrong: %+v %v", c, err)
	}

	// Researching an opportunity is not an application and not outreach.
	if v := outreachValue(t, s, day1); v > 0 {
		t.Fatalf("outreach after research = %v, want 0", v)
	}

	app := Application{OpportunityID: opp.ID, Status: "applied", Source: "Company website"}
	if err := s.SaveApplication(&app, day1); err != nil {
		t.Fatal(err)
	}
	if app.AppliedOn != day1 {
		t.Fatalf("applied_on = %q, want %q", app.AppliedOn, day1)
	}

	li := Outreach{Type: "linkedin", Date: day1, Status: "sent"}
	if err := s.SaveOutreach(&li, "acme", "Jane Recruiter", day1); err != nil { // lower-case name reuses Acme
		t.Fatal(err)
	}
	if li.CompanyID != opp.CompanyID || li.ContactID == 0 {
		t.Fatalf("outreach not linked to existing company/new contact: %+v", li)
	}
	mail := Outreach{Type: "cold_email", Date: day1, Status: "sent"}
	if err := s.SaveOutreach(&mail, "", "", day1); err != nil {
		t.Fatal(err)
	}

	if v := outreachValue(t, s, day1); v != 3 {
		t.Fatalf("Job Outreach = %v, want 3 (1 application + 2 messages)", v)
	}

	if err := s.SetApplicationStatus(app.ID, "interview", day2); err != nil {
		t.Fatal(err)
	}
	li.Status = "replied"
	if err := s.SaveOutreach(&li, "Acme", "Jane Recruiter", day2); err != nil {
		t.Fatal(err)
	}

	m, err := s.CareerMetrics(day1, "2026-09-27", "")
	if err != nil {
		t.Fatal(err)
	}
	want := CareerMetrics{Applications: 1, Emails: 1, LinkedIn: 1, Companies: 1, Opportunities: 1, Responses: 1, Interviews: 1, Pending: 1}
	if m != want {
		t.Fatalf("metrics\n got %+v\nwant %+v", m, want)
	}
	if m.Outreach() != 3 {
		t.Fatalf("Outreach() = %d", m.Outreach())
	}

	// Country filter: the unlinked email has no country and drops out.
	m, _ = s.CareerMetrics(day1, "2026-09-27", "Germany")
	if m.Emails != 0 || m.LinkedIn != 1 || m.Applications != 1 {
		t.Fatalf("Germany metrics wrong: %+v", m)
	}
	m, _ = s.CareerMetrics(day1, "2026-09-27", "Poland")
	if m != (CareerMetrics{}) {
		t.Fatalf("Poland metrics should be empty: %+v", m)
	}

	// Moving and deleting outreach keeps Job Outreach in step.
	mail.Date = day2
	if err := s.SaveOutreach(&mail, "", "", day2); err != nil {
		t.Fatal(err)
	}
	if a, b := outreachValue(t, s, day1), outreachValue(t, s, day2); a != 2 || b != 1 {
		t.Fatalf("after move: day1=%v day2=%v, want 2 and 1", a, b)
	}
	if err := s.DeleteOutreach(li.ID); err != nil {
		t.Fatal(err)
	}
	if v := outreachValue(t, s, day1); v != 1 {
		t.Fatalf("after delete = %v, want 1", v)
	}

	// A manual save on the linked habit only keeps the note.
	h := outreachHabit(t, s)
	if err := s.SaveEntry(Entry{HabitID: h.ID, Date: day1, Value: 99, Note: "good day"}); err != nil {
		t.Fatal(err)
	}
	if v := outreachValue(t, s, day1); v != 1 {
		t.Fatalf("manual value leaked into linked habit: %v", v)
	}

	// Referential guards.
	if err := s.DeleteCompany(opp.CompanyID); !errors.Is(err, ErrInUse) {
		t.Fatalf("DeleteCompany err = %v, want ErrInUse", err)
	}
	if err := s.DeleteOpportunity(opp.ID); !errors.Is(err, ErrInUse) {
		t.Fatalf("DeleteOpportunity err = %v, want ErrInUse", err)
	}
	dup := Company{Name: "ACME", AddedOn: day1}
	if err := s.SaveCompany(&dup); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate company err = %v", err)
	}

	// Deleting the application removes it from Job Outreach.
	if err := s.DeleteApplication(app.ID); err != nil {
		t.Fatal(err)
	}
	if v := outreachValue(t, s, day1); v != 0 {
		t.Fatalf("after deleting application = %v, want 0", v)
	}
}

func TestLegacyEntriesBeforeLinkAreKept(t *testing.T) {
	s := openTest(t)
	h := outreachHabit(t, s)
	// Pretend the link started on day2: a manual day1 value must survive a resync.
	if err := s.SetSetting(keyOutreachSince, day2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO habit_entries (habit_id, date, value, target, updated_at) VALUES (?, ?, 7, 10, '')`, h.ID, day1); err != nil {
		t.Fatal(err)
	}
	if err := s.ResyncOutreach(); err != nil {
		t.Fatal(err)
	}
	if v := outreachValue(t, s, day1); v != 7 {
		t.Fatalf("legacy entry = %v, want 7", v)
	}
}

func TestPendingAndStatusDates(t *testing.T) {
	s := openTest(t)
	opp := Opportunity{Title: "Go Developer", GoRelevance: "core", Remote: "yes", Relocation: "unknown", Visa: "unknown", Match: "possible", ResearchedOn: day1}
	if err := s.SaveOpportunity(&opp, "Beta"); err != nil {
		t.Fatal(err)
	}
	// Saved for later: not submitted, not pending, not outreach.
	app := Application{OpportunityID: opp.ID, Status: "saved"}
	if err := s.SaveApplication(&app, day1); err != nil {
		t.Fatal(err)
	}
	m, _ := s.CareerMetrics(day1, day2, "")
	if m.Applications != 0 || m.Pending != 0 {
		t.Fatalf("saved application counted: %+v", m)
	}
	if err := s.SetApplicationStatus(app.ID, "applied", day2); err != nil {
		t.Fatal(err)
	}
	if err := s.SetApplicationStatus(app.ID, "rejected", day2); err != nil {
		t.Fatal(err)
	}
	m, _ = s.CareerMetrics(day2, day2, "")
	if m.Applications != 1 || m.Rejections != 1 || m.Pending != 0 {
		t.Fatalf("got %+v", m)
	}
	a, _ := s.Application(app.ID)
	if a.AppliedOn != day2 || len(a.History) != 3 {
		t.Fatalf("application = %+v", a)
	}
}

// Regression: an application that hasn't been sent must never count as outreach,
// even if the form still carried a date.
func TestUnsentApplicationDoesNotCount(t *testing.T) {
	s := openTest(t)
	opp := Opportunity{Title: "Go Engineer", GoRelevance: "core", Remote: "no", Relocation: "unknown", Visa: "unknown", Match: "unset", ResearchedOn: day1}
	if err := s.SaveOpportunity(&opp, "Gamma"); err != nil {
		t.Fatal(err)
	}
	app := Application{OpportunityID: opp.ID, Status: "saved", AppliedOn: day1}
	if err := s.SaveApplication(&app, day1); err != nil {
		t.Fatal(err)
	}
	if v := outreachValue(t, s, day1); v > 0 {
		t.Fatalf("saved application counted as outreach: %v", v)
	}
	// Once it's actually sent it counts, and it keeps counting after a rejection.
	if err := s.SetApplicationStatus(app.ID, "applied", day2); err != nil {
		t.Fatal(err)
	}
	if err := s.SetApplicationStatus(app.ID, "rejected", day2); err != nil {
		t.Fatal(err)
	}
	if v := outreachValue(t, s, day2); v != 1 {
		t.Fatalf("sent-then-rejected application = %v, want 1", v)
	}
}

// Regression: a follow-up message plus moving the same application to
// Follow-up on the same day is one follow-up, not two. Same for responses.
func TestFollowUpAndResponseNotDoubleCounted(t *testing.T) {
	s := openTest(t)
	opp := Opportunity{Title: "Platform Engineer", GoRelevance: "partial", Remote: "yes", Relocation: "unknown", Visa: "unknown", Match: "possible", ResearchedOn: day1}
	if err := s.SaveOpportunity(&opp, "Delta"); err != nil {
		t.Fatal(err)
	}
	app := Application{OpportunityID: opp.ID, Status: "applied"}
	if err := s.SaveApplication(&app, day1); err != nil {
		t.Fatal(err)
	}
	fu := Outreach{Type: "follow_up", Date: day2, Status: "replied", OpportunityID: opp.ID}
	if err := s.SaveOutreach(&fu, "Delta", "", day2); err != nil {
		t.Fatal(err)
	}
	if err := s.SetApplicationStatus(app.ID, "follow_up", day2); err != nil {
		t.Fatal(err)
	}
	if err := s.SetApplicationStatus(app.ID, "response", day2); err != nil {
		t.Fatal(err)
	}
	m, err := s.CareerMetrics(day1, day2, "")
	if err != nil {
		t.Fatal(err)
	}
	if m.FollowUps != 1 || m.Responses != 1 {
		t.Fatalf("follow-ups=%d responses=%d, want 1 and 1", m.FollowUps, m.Responses)
	}
	// Job Outreach still counts both real actions: the application and the follow-up message.
	if a, b := outreachValue(t, s, day1), outreachValue(t, s, day2); a != 1 || b != 1 {
		t.Fatalf("outreach day1=%v day2=%v", a, b)
	}
}

// Regression: a reply date only counts while the outreach is marked replied/closed.
func TestResponseDateClearedWhenNotReplied(t *testing.T) {
	s := openTest(t)
	o := Outreach{Type: "cold_email", Date: day1, Status: "replied"}
	if err := s.SaveOutreach(&o, "Epsilon", "", day2); err != nil {
		t.Fatal(err)
	}
	o.Status = "no_response"
	if err := s.SaveOutreach(&o, "Epsilon", "", day2); err != nil {
		t.Fatal(err)
	}
	m, _ := s.CareerMetrics(day1, day2, "")
	if m.Responses != 0 {
		t.Fatalf("responses = %d after marking no response", m.Responses)
	}
}

// Regression: on a fresh database, outreach recorded for earlier days counts
// toward Job Outreach on those days.
func TestBackfilledOutreachCountsOnFreshDatabase(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SeedDefaults(day2); err != nil {
		t.Fatal(err)
	}
	o := Outreach{Type: "cold_email", Date: "2026-08-15", Status: "sent"}
	if err := s.SaveOutreach(&o, "Zeta", "", day2); err != nil {
		t.Fatal(err)
	}
	if v := outreachValue(t, s, "2026-08-15"); v != 1 {
		t.Fatalf("backfilled outreach = %v, want 1", v)
	}
}
