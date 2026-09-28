package web

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"tracker/internal/stats"
	"tracker/internal/store"
)

type breakdownItem struct {
	Key   string
	Value float64
}

type todayRow struct {
	H           store.Habit
	Logged      bool
	Value       float64
	Target      float64
	Note        string
	Breakdown   []breakdownItem
	Progress    float64
	Done        bool
	Scheduled   bool
	BeforeStart bool // the day is before the habit was first tracked
	Streak      int
	Best        int
	Outreach    *store.OutreachDay // set for the career-linked habit
}

type todayView struct {
	page
	Greeting  string
	DateLabel string
	DateShort string
	Date      string
	Prev      string
	Next      string
	IsToday   bool
	Rows      []todayRow
	Score     score
	Tasks     []store.Task
	TasksDone int
	Companies []string
}

func (s *Server) today(w http.ResponseWriter, r *http.Request) {
	if s.needsSetup(w, r) {
		return
	}
	today := s.todayDate()
	date := s.dateParam(r, "date")
	hs, err := s.load()
	if err != nil {
		s.fail(w, err)
		return
	}
	isToday := date.Equal(today)
	since, err := s.store.OutreachLinkedSince()
	if err != nil {
		s.fail(w, err)
		return
	}
	tasks, err := s.store.TasksFor(stats.Key(date), isToday)
	if err != nil {
		s.fail(w, err)
		return
	}
	v := todayView{
		page:      page{Nav: "today", Title: "Today"},
		Greeting:  greeting(s.now()),
		DateLabel: date.Format("Monday, January 2, 2006"),
		DateShort: date.Format("Mon, Jan 2"),
		Date:      stats.Key(date),
		Prev:      stats.Key(date.AddDate(0, 0, -1)),
		IsToday:   isToday,
		Score:     dayScore(hs, date),
		Tasks:     tasks,
	}
	if !isToday {
		v.Next = stats.Key(date.AddDate(0, 0, 1))
	}
	for _, t := range tasks {
		if t.Done {
			v.TasksDone++
		}
	}
	for _, h := range hs {
		row := todayRow{
			H:           h.Habit,
			Target:      h.Target,
			Scheduled:   h.sched.ScheduledOn(date),
			BeforeStart: date.Before(h.sched.Start),
			Streak:      stats.CurrentStreak(h.sched, h.ent, today),
			Best:        stats.LongestStreak(h.sched, h.ent, today),
		}
		if e, ok := h.raw[v.Date]; ok {
			row.Logged, row.Value, row.Target, row.Note = true, e.Value, e.Target, e.Note
		}
		row.Progress, row.Done = h.ent.On(date)
		var bd map[string]float64
		if e, ok := h.raw[v.Date]; ok {
			bd = e.Breakdown
		}
		for _, k := range h.BreakdownKeys() {
			row.Breakdown = append(row.Breakdown, breakdownItem{Key: k, Value: bd[k]})
		}
		// Days before the career link was made are still logged by hand.
		if h.Linked() && v.Date >= since {
			od, err := s.store.OutreachOn(v.Date)
			if err != nil {
				s.fail(w, err)
				return
			}
			row.Outreach = &od
		}
		v.Rows = append(v.Rows, row)
	}
	if v.Companies, err = s.store.CompanyNames(); err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, http.StatusOK, "today", v)
}

func greeting(now time.Time) string {
	switch h := now.Hour(); {
	case h < 12:
		return "Good morning"
	case h < 17:
		return "Good afternoon"
	}
	return "Good evening"
}

type saveEntryRequest struct {
	HabitID   int64              `json:"habit_id"`
	Date      string             `json:"date"`
	Value     float64            `json:"value"`
	Breakdown map[string]float64 `json:"breakdown"`
	Note      string             `json:"note"`
}

type saveEntryResponse struct {
	Progress float64            `json:"progress"`
	Done     bool               `json:"done"`
	Value    float64            `json:"value"`
	Streak   int                `json:"streak"`
	Best     int                `json:"best"`
	Score    scoreJSON          `json:"score"`
	Outreach *store.OutreachDay `json:"outreach,omitempty"`
}

type scoreJSON struct {
	Completed int `json:"completed"`
	Total     int `json:"total"`
	Percent   int `json:"percent"`
}

func (s *Server) saveEntry(w http.ResponseWriter, r *http.Request) {
	var req saveEntryRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	date, err := stats.ParseDay(req.Date)
	if err != nil || date.After(s.todayDate()) {
		http.Error(w, "invalid date", http.StatusBadRequest)
		return
	}
	h, err := s.store.Habit(req.HabitID)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "unknown habit", http.StatusNotFound)
		return
	} else if err != nil {
		s.fail(w, err)
		return
	}

	e := store.Entry{HabitID: h.ID, Date: req.Date, Value: max(req.Value, 0), Note: truncate(strings.TrimSpace(req.Note), 500)}
	if keys := h.BreakdownKeys(); len(keys) > 0 && req.Breakdown != nil {
		e.Breakdown, e.Value = map[string]float64{}, 0
		for _, k := range keys {
			n := max(req.Breakdown[k], 0)
			e.Breakdown[k] = n
			e.Value += n
		}
	}
	if h.Kind == store.KindBinary {
		e.Value = min(e.Value, 1)
	}
	if err := s.store.SaveEntry(e); err != nil {
		s.fail(w, err)
		return
	}
	s.writeEntryState(w, h.ID, date)
}

// writeEntryState answers with a habit's state on date after a change, plus the day score.
func (s *Server) writeEntryState(w http.ResponseWriter, habitID int64, date time.Time) {
	today := s.todayDate()
	hs, err := s.load()
	if err != nil {
		s.fail(w, err)
		return
	}
	var resp saveEntryResponse
	for _, hd := range hs {
		if hd.ID != habitID {
			continue
		}
		resp.Progress, resp.Done = hd.ent.On(date)
		resp.Value = hd.ent[stats.Key(date)].Value
		resp.Streak = stats.CurrentStreak(hd.sched, hd.ent, today)
		resp.Best = stats.LongestStreak(hd.sched, hd.ent, today)
		if hd.Linked() {
			od, err := s.store.OutreachOn(stats.Key(date))
			if err != nil {
				s.fail(w, err)
				return
			}
			resp.Outreach = &od
		}
	}
	sc := dayScore(hs, date)
	resp.Score = scoreJSON{Completed: sc.Completed, Total: sc.Total, Percent: pct(sc.Progress)}
	writeJSON(w, resp)
}

// quickOutreach logs one outreach message from the Today page.
func (s *Server) quickOutreach(w http.ResponseWriter, r *http.Request) {
	var req struct {
		HabitID int64  `json:"habit_id"`
		Date    string `json:"date"`
		Type    string `json:"type"`
		Company string `json:"company"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	date, err := stats.ParseDay(req.Date)
	if err != nil || date.After(s.todayDate()) || !store.Valid(store.OutreachTypes, req.Type) {
		http.Error(w, "invalid date or type", http.StatusBadRequest)
		return
	}
	o := store.Outreach{Type: req.Type, Date: req.Date, Status: "sent"}
	if err := s.store.SaveOutreach(&o, truncate(strings.TrimSpace(req.Company), 120), "", s.todayKey()); err != nil {
		s.fail(w, err)
		return
	}
	s.writeEntryState(w, req.HabitID, date)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Date  string `json:"date"`
		Title string `json:"title"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	title := truncate(strings.TrimSpace(req.Title), 200)
	if _, err := stats.ParseDay(req.Date); err != nil || title == "" {
		http.Error(w, "date and title are required", http.StatusBadRequest)
		return
	}
	t, err := s.store.CreateTask(req.Date, title)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"id": t.ID, "title": t.Title})
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	var req struct {
		Done bool   `json:"done"`
		Date string `json:"date"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil || !ok {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if _, err := stats.ParseDay(req.Date); err != nil {
		http.Error(w, "invalid date", http.StatusBadRequest)
		return
	}
	if err := s.store.SetTaskDone(id, req.Done, req.Date); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := s.store.DeleteTask(id); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
