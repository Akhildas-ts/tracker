package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"tracker/internal/stats"
	"tracker/internal/store"
)

type habitListView struct {
	page
	Active   []store.Habit
	Archived []store.Habit
}

func (s *Server) habitList(w http.ResponseWriter, r *http.Request) {
	hs, err := s.store.Habits(true)
	if err != nil {
		s.fail(w, err)
		return
	}
	v := habitListView{page: page{Nav: "habits", Title: "Habits"}}
	for _, h := range hs {
		if h.Archived() {
			v.Archived = append(v.Archived, h)
		} else {
			v.Active = append(v.Active, h)
		}
	}
	s.render(w, http.StatusOK, "habits", v)
}

type dayOption struct {
	Bit  int
	Name string
	On   bool
}

type habitFormView struct {
	page
	H      store.Habit
	IsNew  bool
	Errors []string
	Days   []dayOption
}

func (s *Server) renderForm(w http.ResponseWriter, status int, h store.Habit, errs []string) {
	v := habitFormView{page: page{Nav: "habits", Title: "Edit habit"}, H: h, IsNew: h.ID == 0, Errors: errs}
	if v.IsNew {
		v.Title = "New habit"
	}
	for i, n := range weekdayNames {
		v.Days = append(v.Days, dayOption{Bit: i, Name: n, On: h.ScheduleDays&(1<<i) != 0})
	}
	s.render(w, status, "habit_form", v)
}

func (s *Server) newHabit(w http.ResponseWriter, r *http.Request) {
	s.renderForm(w, http.StatusOK, store.Habit{Kind: store.KindBinary, Target: 1, Step: 1, ScheduleDays: store.EveryDay}, nil)
}

func (s *Server) createHabit(w http.ResponseWriter, r *http.Request) {
	h := store.Habit{StartDate: stats.Key(s.todayDate())}
	if errs := parseHabitForm(r, &h); len(errs) > 0 {
		s.renderForm(w, http.StatusBadRequest, h, errs)
		return
	}
	if err := s.store.CreateHabit(&h); err != nil {
		s.fail(w, err)
		return
	}
	if h.Linked() {
		if err := s.store.ResyncOutreach(); err != nil {
			s.fail(w, err)
			return
		}
	}
	http.Redirect(w, r, "/habits", http.StatusSeeOther)
}

// habitFromPath loads the habit named in the URL, writing an error response if it can't.
func (s *Server) habitFromPath(w http.ResponseWriter, r *http.Request) (store.Habit, bool) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return store.Habit{}, false
	}
	h, err := s.store.Habit(id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return h, false
	} else if err != nil {
		s.fail(w, err)
		return h, false
	}
	return h, true
}

func (s *Server) editHabit(w http.ResponseWriter, r *http.Request) {
	if h, ok := s.habitFromPath(w, r); ok {
		s.renderForm(w, http.StatusOK, h, nil)
	}
}

func (s *Server) updateHabit(w http.ResponseWriter, r *http.Request) {
	h, ok := s.habitFromPath(w, r)
	if !ok {
		return
	}
	if errs := parseHabitForm(r, &h); len(errs) > 0 {
		s.renderForm(w, http.StatusBadRequest, h, errs)
		return
	}
	if err := s.store.UpdateHabit(h, stats.Key(s.todayDate())); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/habits", http.StatusSeeOther)
}

func (s *Server) archiveHabit(w http.ResponseWriter, r *http.Request) { s.setArchived(w, r, true) }
func (s *Server) restoreHabit(w http.ResponseWriter, r *http.Request) { s.setArchived(w, r, false) }

func (s *Server) setArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	h, ok := s.habitFromPath(w, r)
	if !ok {
		return
	}
	if err := s.store.SetArchived(h.ID, archived); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/habits", http.StatusSeeOther)
}

func (s *Server) deleteHabit(w http.ResponseWriter, r *http.Request) {
	h, ok := s.habitFromPath(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteHabit(h.ID); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/habits", http.StatusSeeOther)
}

func (s *Server) moveHabit(w http.ResponseWriter, r *http.Request) {
	h, ok := s.habitFromPath(w, r)
	if !ok {
		return
	}
	delta := 1
	if r.FormValue("dir") == "up" {
		delta = -1
	}
	if err := s.store.MoveHabit(h.ID, delta); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/habits", http.StatusSeeOther)
}

// parseHabitForm copies the submitted form into h and returns validation errors.
func parseHabitForm(r *http.Request, h *store.Habit) []string {
	var errs []string
	if err := r.ParseForm(); err != nil {
		return []string{"Could not read the form."}
	}
	h.Name = strings.TrimSpace(r.FormValue("name"))
	h.Emoji = strings.TrimSpace(r.FormValue("emoji"))
	h.Kind = r.FormValue("kind")
	h.Unit = strings.TrimSpace(r.FormValue("unit"))
	h.ReminderTime = r.FormValue("reminder_time")
	h.Description = strings.TrimSpace(r.FormValue("description"))
	h.Source = store.SourceManual
	if r.FormValue("source") == store.SourceOutreach && h.Kind == store.KindCount {
		h.Source = store.SourceOutreach
	}

	h.ScheduleDays = 0
	for _, v := range r.Form["days"] {
		if i, err := strconv.Atoi(v); err == nil && i >= 0 && i < 7 {
			h.ScheduleDays |= 1 << i
		}
	}

	var keys []string
	for _, k := range strings.Split(r.FormValue("breakdown"), ",") {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			keys = append(keys, k)
		}
	}
	h.Breakdown = strings.Join(keys, ",")

	target, errT := strconv.ParseFloat(r.FormValue("target"), 64)
	step, errS := strconv.ParseFloat(r.FormValue("step"), 64)
	h.Target, h.Step = target, step

	switch h.Kind {
	case store.KindBinary:
		h.Target, h.Step, h.Unit, h.Breakdown = 1, 1, "", ""
	case store.KindDuration:
		h.Unit, h.Breakdown = "min", ""
		fallthrough
	case store.KindCount:
		if errT != nil || target <= 0 {
			errs = append(errs, "Daily target must be a number greater than 0.")
		}
		if errS != nil || step <= 0 {
			errs = append(errs, "Step must be a number greater than 0.")
		}
	default:
		errs = append(errs, "Choose a habit type.")
	}
	if h.Name == "" {
		errs = append(errs, "Name is required.")
	}
	if h.ScheduleDays == 0 {
		errs = append(errs, "Pick at least one day.")
	}
	return errs
}
