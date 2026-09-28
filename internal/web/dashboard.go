package web

import (
	"net/http"

	"tracker/internal/stats"
	"tracker/internal/store"
)

type dashHabit struct {
	H         store.Habit
	Progress  float64
	Done      bool
	Scheduled bool
	Value     string
	Streak    int
}

type dashView struct {
	page
	Greeting     string
	DateLabel    string
	Score        score
	Habits       []dashHabit
	Todo         []string
	OpenTasks    []store.Task
	WeekProgress float64
	WeekLabel    string
	Career       store.CareerMetrics
	Outreach     store.OutreachDay
	Target       float64
	OutreachPct  int
	Pending      []store.Application
	Activity     []store.Activity
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	if s.needsSetup(w, r) {
		return
	}
	// Phase 1 linked to /?date=… for the Today page.
	if d := r.URL.Query().Get("date"); d != "" {
		http.Redirect(w, r, "/today?date="+d, http.StatusMovedPermanently)
		return
	}
	today := s.todayDate()
	key := stats.Key(today)
	hs, err := s.load()
	if err != nil {
		s.fail(w, err)
		return
	}
	from, to := s.weekRange()
	v := dashView{
		page:      page{Nav: "dashboard", Title: "Dashboard"},
		Greeting:  greeting(s.now()),
		DateLabel: today.Format("Monday, January 2, 2006"),
		Score:     dayScore(hs, today),
		WeekLabel: shortDate(from) + " – " + shortDate(to),
		Target:    s.outreachTarget(),
	}
	for _, h := range hs {
		e := h.raw[key]
		p, done := h.ent.On(today)
		dh := dashHabit{H: h.Habit, Progress: p, Done: done, Scheduled: h.sched.ScheduledOn(today),
			Streak: stats.CurrentStreak(h.sched, h.ent, today)}
		switch h.Kind {
		case store.KindBinary:
			if done {
				dh.Value = "Done"
			} else {
				dh.Value = "Not yet"
			}
		case store.KindDuration:
			dh.Value = duration(e.Value) + " / " + duration(h.Target)
		default:
			dh.Value = num(e.Value) + " / " + num(h.Target)
		}
		if dh.Scheduled && !done {
			v.Todo = append(v.Todo, h.Emoji+" "+h.Name)
		}
		v.Habits = append(v.Habits, dh)
	}

	_, tot, err := s.report(hs, stats.Monday(today), stats.Monday(today).AddDate(0, 0, 6), today)
	if err != nil {
		s.fail(w, err)
		return
	}
	v.WeekProgress = tot.Overall

	tasks, err := s.store.TasksFor(key, true)
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, t := range tasks {
		if !t.Done {
			v.OpenTasks = append(v.OpenTasks, t)
		}
	}
	if v.Career, err = s.store.CareerMetrics(from, to, ""); err != nil {
		s.fail(w, err)
		return
	}
	if v.Outreach, err = s.store.OutreachOn(key); err != nil {
		s.fail(w, err)
		return
	}
	v.OutreachPct = barHeight(float64(v.Outreach.Total()), v.Target)
	if v.Pending, err = s.store.Applications(store.ApplicationFilter{Pending: true}); err != nil {
		s.fail(w, err)
		return
	}
	if len(v.Pending) > 6 {
		v.Pending = v.Pending[:6]
	}
	if v.Activity, err = s.store.RecentActivity(8); err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, http.StatusOK, "dashboard", v)
}
