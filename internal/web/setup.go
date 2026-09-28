package web

import (
	"net/http"
	"strings"

	"tracker/internal/store"
)

type templateGroup struct {
	Name      string
	Templates []store.HabitTemplate
}

type welcomeView struct {
	page
	Groups []templateGroup
}

// welcome is the first-run setup: name, whether to track a job search, and
// which starter habits (if any) to begin with.
func (s *Server) welcome(w http.ResponseWriter, r *http.Request) {
	v := welcomeView{page: page{Nav: "", Title: "Welcome"}}
	for _, t := range store.HabitTemplates {
		if n := len(v.Groups); n == 0 || v.Groups[n-1].Name != t.Group {
			v.Groups = append(v.Groups, templateGroup{Name: t.Group})
		}
		g := &v.Groups[len(v.Groups)-1]
		g.Templates = append(g.Templates, t)
	}
	s.render(w, http.StatusOK, "welcome", v)
}

func (s *Server) finishWelcome(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	career := r.FormValue("career") == "1"
	settings := map[string]string{
		store.KeyDisplayName: truncate(strings.TrimSpace(r.FormValue("display_name")), 60),
		store.KeySkill:       truncate(strings.TrimSpace(r.FormValue("skill")), 40),
		store.KeyCareer:      map[bool]string{true: "1", false: "0"}[career],
	}
	for k, v := range settings {
		if err := s.store.SetSetting(k, v); err != nil {
			s.fail(w, err)
			return
		}
	}
	// Only add starter habits once, even if the form is submitted twice.
	if hs, err := s.store.Habits(true); err == nil && len(hs) == 0 {
		var keys []string
		for _, k := range r.Form["habit"] {
			if !career && k == "job-outreach" {
				continue // it counts Career records, which are hidden
			}
			keys = append(keys, k)
		}
		if err := s.store.CreateFromTemplates(keys, s.todayKey()); err != nil {
			s.fail(w, err)
			return
		}
	}
	if err := s.store.SetSetting(store.KeySetupDone, "1"); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/today", http.StatusSeeOther)
}
