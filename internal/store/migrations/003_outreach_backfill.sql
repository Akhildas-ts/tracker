-- Job Outreach is computed from career records for every day except days that
-- already had a value typed in by hand before the career link existed.
-- 002 protected everything before the upgrade day, which meant outreach
-- recorded for earlier days (backfilled history) never counted on those days.
-- Narrow the cutoff to just after the last hand-logged day; with none, every
-- day is computed from Career.
UPDATE settings SET value = COALESCE(
  (SELECT date(MAX(e.date), '+1 day')
     FROM habit_entries e JOIN habits h ON h.id = e.habit_id
    WHERE h.source = 'career_outreach' AND e.value > 0 AND e.date < settings.value),
  '0000-01-01')
WHERE key = 'outreach_linked_since';
