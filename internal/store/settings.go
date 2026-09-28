package store

import (
	"database/sql"
	"errors"
)

const (
	keyOutreachSince = "outreach_linked_since"
	KeyDisplayName   = "display_name"
	KeySetupDone     = "setup_done"     // "1" once first-run setup is finished
	KeyCareer        = "career_enabled" // "0" hides job-search tracking; anything else shows it
	KeySkill         = "primary_skill"  // e.g. "Go"; used to label opportunity relevance
)

func setting(q querier, key string) (string, error) {
	var v string
	err := q.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// Setting returns a stored setting, or "" if it has never been set.
func (s *Store) Setting(key string) (string, error) { return setting(s.db, key) }

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// OutreachLinkedSince is the first day Job Outreach was computed from career records.
func (s *Store) OutreachLinkedSince() (string, error) { return setting(s.db, keyOutreachSince) }
