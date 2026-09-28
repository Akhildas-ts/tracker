-- Habit definitions. Archived rather than deleted so history stays intact.
CREATE TABLE habits (
  id            INTEGER PRIMARY KEY,
  name          TEXT    NOT NULL,
  emoji         TEXT,
  kind          TEXT    NOT NULL CHECK (kind IN ('binary', 'count', 'duration')),
  target        REAL    NOT NULL DEFAULT 1,   -- 1 for binary, minutes for duration, count otherwise
  unit          TEXT,
  step          REAL    NOT NULL DEFAULT 1,   -- +/- increment on the Today screen
  schedule_days INTEGER NOT NULL DEFAULT 127, -- weekday bitmask, bit 0 = Monday ... bit 6 = Sunday
  breakdown     TEXT,                         -- optional comma-separated sub-counters, e.g. 'easy,medium,hard'
  reminder_time TEXT,                         -- 'HH:MM'; stored for future reminders
  description   TEXT,
  sort_order    INTEGER NOT NULL DEFAULT 0,
  start_date    TEXT    NOT NULL,             -- 'YYYY-MM-DD'; stats ignore days before this
  archived_at   TEXT,
  created_at    TEXT    NOT NULL
);

-- One row per habit per day. Every statistic is derived from this table.
CREATE TABLE habit_entries (
  id         INTEGER PRIMARY KEY,
  habit_id   INTEGER NOT NULL REFERENCES habits (id),
  date       TEXT    NOT NULL,
  value      REAL    NOT NULL,
  target     REAL    NOT NULL, -- the habit's target when this day was logged
  breakdown  TEXT,             -- JSON object of sub-counter values
  note       TEXT,
  updated_at TEXT    NOT NULL,
  UNIQUE (habit_id, date)
);
CREATE INDEX idx_habit_entries_date ON habit_entries (date);

-- One-off daily tasks (to-dos), separate from recurring habits.
CREATE TABLE tasks (
  id         INTEGER PRIMARY KEY,
  date       TEXT    NOT NULL,
  title      TEXT    NOT NULL,
  done_at    TEXT,
  created_at TEXT    NOT NULL
);
CREATE INDEX idx_tasks_date ON tasks (date);
