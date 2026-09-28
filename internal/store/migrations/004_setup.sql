-- First-run setup replaces the fixed starter habits. Databases that already
-- have habits were set up before this existed; they keep everything as it is,
-- including the "Go" wording used for opportunity relevance until now.
INSERT OR IGNORE INTO settings (key, value) SELECT 'setup_done', '1' WHERE EXISTS (SELECT 1 FROM habits);
INSERT OR IGNORE INTO settings (key, value) SELECT 'primary_skill', 'Go' WHERE EXISTS (SELECT 1 FROM habits);
