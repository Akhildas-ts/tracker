package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"tracker/internal/store"
)

type backupFile struct {
	Name string
	Size string
}

type settingsView struct {
	page
	Name          string
	DBPath        string
	BackupDir     string
	Backups       []backupFile
	LinkedSince   string
	OutreachHabit store.Habit
	Message       string
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	v := settingsView{page: page{Nav: "settings", Title: "Settings"}, Name: s.profileName(),
		DBPath: s.opts.DBPath, BackupDir: s.opts.BackupDir}
	switch r.URL.Query().Get("saved") {
	case "name":
		v.Message = "Saved."
	case "backup":
		v.Message = "Backup created."
	}
	since, err := s.store.OutreachLinkedSince()
	if err != nil {
		s.fail(w, err)
		return
	}
	if since > "0001" {
		v.LinkedSince = shortDate(since)
	}
	if hs, err := s.store.Habits(false); err == nil {
		for _, h := range hs {
			if h.Linked() {
				v.OutreachHabit = h
			}
		}
	}
	if s.opts.BackupDir != "" {
		files, _ := filepath.Glob(filepath.Join(s.opts.BackupDir, "tracker-*.db"))
		sort.Sort(sort.Reverse(sort.StringSlice(files)))
		for i, f := range files {
			if i == 10 {
				break
			}
			if st, err := os.Stat(f); err == nil {
				v.Backups = append(v.Backups, backupFile{Name: filepath.Base(f), Size: humanSize(st.Size())})
			}
		}
	}
	s.render(w, http.StatusOK, "settings", v)
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	career := "0"
	if r.FormValue("career") == "1" {
		career = "1"
	}
	for k, v := range map[string]string{
		store.KeyDisplayName: truncate(strings.TrimSpace(r.FormValue("display_name")), 60),
		store.KeySkill:       truncate(strings.TrimSpace(r.FormValue("skill")), 40),
		store.KeyCareer:      career,
	} {
		if err := s.store.SetSetting(k, v); err != nil {
			s.fail(w, err)
			return
		}
	}
	http.Redirect(w, r, "/settings?saved=name", http.StatusSeeOther)
}

func (s *Server) backupNow(w http.ResponseWriter, r *http.Request) {
	if s.opts.BackupDir == "" {
		http.Error(w, "backups are not configured", http.StatusBadRequest)
		return
	}
	stamp := s.now().Format("2006-01-02-150405")
	if _, err := s.store.Backup(s.opts.BackupDir, stamp, 30); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/settings?saved=backup", http.StatusSeeOther)
}

func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	data, err := s.store.Export()
	if err != nil {
		s.fail(w, err)
		return
	}
	name := "tracker-export-" + s.now().Format("2006-01-02") + ".json"
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(map[string]any{"exported_at": s.now().Format(time.RFC3339), "tables": data})
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return strings.TrimSuffix(num(float64(n*10/(1<<20))/10), ".0") + " MB"
	case n >= 1<<10:
		return num(float64(n/(1<<10))) + " KB"
	}
	return num(float64(n)) + " B"
}
