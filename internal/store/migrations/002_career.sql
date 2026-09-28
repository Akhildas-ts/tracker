-- Career tracking. Adds tables only; Phase 1 data is untouched except for
-- linking the Job Outreach habit to career activity.

-- 'manual' habits are logged on the Today page; 'career_outreach' habits are
-- computed from outreach messages and submitted applications.
ALTER TABLE habits ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE companies (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL COLLATE NOCASE UNIQUE,
  country    TEXT,
  website    TEXT,
  notes      TEXT,
  added_on   TEXT NOT NULL, -- day the company was researched/added
  created_at TEXT NOT NULL
);

CREATE TABLE contacts (
  id          INTEGER PRIMARY KEY,
  company_id  INTEGER NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
  name        TEXT NOT NULL COLLATE NOCASE,
  role        TEXT,
  email       TEXT,
  profile_url TEXT,
  notes       TEXT,
  created_at  TEXT NOT NULL,
  UNIQUE (company_id, name)
);

CREATE TABLE opportunities (
  id            INTEGER PRIMARY KEY,
  company_id    INTEGER NOT NULL REFERENCES companies (id),
  title         TEXT NOT NULL,
  country       TEXT,
  location      TEXT,
  url           TEXT,
  go_relevance  TEXT NOT NULL DEFAULT 'unknown' CHECK (go_relevance IN ('core', 'partial', 'none', 'unknown')),
  remote        TEXT NOT NULL DEFAULT 'unknown' CHECK (remote IN ('yes', 'hybrid', 'no', 'unknown')),
  relocation    TEXT NOT NULL DEFAULT 'unknown' CHECK (relocation IN ('yes', 'no', 'unknown')),
  visa          TEXT NOT NULL DEFAULT 'unknown' CHECK (visa IN ('yes', 'no', 'unknown')),
  experience    TEXT,
  skills        TEXT,
  profile_match TEXT NOT NULL DEFAULT 'unset'
                CHECK (profile_match IN ('strong', 'possible', 'needs_improvement', 'not_match', 'unset')),
  researched_on TEXT NOT NULL,
  notes         TEXT,
  created_at    TEXT NOT NULL
);
CREATE INDEX idx_opportunities_company ON opportunities (company_id);
CREATE INDEX idx_opportunities_researched ON opportunities (researched_on);

CREATE TABLE applications (
  id             INTEGER PRIMARY KEY,
  opportunity_id INTEGER NOT NULL REFERENCES opportunities (id),
  applied_on     TEXT, -- set once the application is submitted
  source         TEXT,
  status         TEXT NOT NULL CHECK (status IN ('saved', 'researching', 'ready', 'applied', 'follow_up',
                                                 'response', 'interview', 'offer', 'rejected', 'closed')),
  notes          TEXT,
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL
);
CREATE INDEX idx_applications_opportunity ON applications (opportunity_id);
CREATE INDEX idx_applications_applied ON applications (applied_on);

-- Every status change with the day it happened, so period counts
-- (interviews this week, rejections this month) reflect when things occurred.
CREATE TABLE application_status_log (
  id             INTEGER PRIMARY KEY,
  application_id INTEGER NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
  status         TEXT NOT NULL,
  date           TEXT NOT NULL,
  created_at     TEXT NOT NULL
);
CREATE INDEX idx_status_log_date ON application_status_log (date);

CREATE TABLE outreach (
  id             INTEGER PRIMARY KEY,
  company_id     INTEGER REFERENCES companies (id),
  opportunity_id INTEGER REFERENCES opportunities (id) ON DELETE SET NULL,
  contact_id     INTEGER REFERENCES contacts (id) ON DELETE SET NULL,
  type           TEXT NOT NULL CHECK (type IN ('cold_email', 'recruiter_email', 'linkedin', 'referral', 'follow_up', 'other')),
  date           TEXT NOT NULL,
  status         TEXT NOT NULL DEFAULT 'sent' CHECK (status IN ('sent', 'replied', 'no_response', 'closed')),
  response       TEXT,
  response_on    TEXT,
  notes          TEXT,
  created_at     TEXT NOT NULL
);
CREATE INDEX idx_outreach_date ON outreach (date);
CREATE INDEX idx_outreach_company ON outreach (company_id);

-- Link the existing Job Outreach habit. Days before the link date keep
-- whatever was logged manually. If a value was already logged by hand today,
-- the link starts tomorrow so that value is not overwritten.
UPDATE habits SET source = 'career_outreach' WHERE name = 'Job Outreach' AND kind = 'count';
INSERT INTO settings (key, value) VALUES ('outreach_linked_since',
  CASE WHEN EXISTS (SELECT 1 FROM habit_entries e JOIN habits h ON h.id = e.habit_id
                    WHERE h.source = 'career_outreach' AND e.date >= date('now', 'localtime') AND e.value > 0)
       THEN date('now', 'localtime', '+1 day')
       ELSE date('now', 'localtime') END);
