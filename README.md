# Daily Habit & Career Tracker

A fast, local web app that answers two questions every day: *How am I doing today?* and *Am I consistently
working toward finding a job abroad?* It tracks daily habits and tasks, logs job-search activity (companies,
opportunities, applications, outreach, contacts), and shows weekly and monthly progress. It runs entirely on
your own machine, and your data never leaves it.

## Run it

You need [Go 1.24+](https://go.dev/dl/). Nothing else is required: the SQLite driver is pure Go, so there's
no C compiler, Node or Docker.

```sh
git clone <this repo>
cd tracker
go run ./cmd/tracker
```

Then open **http://127.0.0.1:8080**.

To build a binary instead:

```sh
go build -o tracker ./cmd/tracker
./tracker
```

## Where your data lives

All data is stored in one SQLite file:

```
~/.habit-tracker/tracker.db
```

- Every change is written to disk immediately. Stopping the app (Ctrl+C), closing the terminal or
  restarting the laptop loses nothing.
- The file is **outside the repo**, so it's never committed, and it survives deleting or re-cloning the repo.
- A backup copy is saved once a day on startup to `~/.habit-tracker/backups/`. The newest 30 are kept.
  **Settings → Back up now** makes one on demand, and **Export all data** downloads everything as JSON.
  To restore a backup, stop the app and copy it over `tracker.db`.

Options (flags or environment variables):

| Flag    | Env var        | Default                      |
|---------|----------------|------------------------------|
| `-db`   | `TRACKER_DB`   | `~/.habit-tracker/tracker.db` |
| `-addr` | `TRACKER_ADDR` | `127.0.0.1:8080`             |

## Features

- **Dashboard**: today's habit progress and streaks, what's still left to do, open tasks, this week's
  job-search numbers, pending applications and recent career activity.
- **Today**: log every habit in seconds. Toggles are one tap, +/− steppers handle minutes and counts, and
  an optional note sits under each habit. Everything saves instantly. Use ← → to fix earlier days.
  Job Outreach has quick **+ Email / + Recruiter / + LinkedIn** buttons.
- **Tasks**: one-off to-dos for the day. Unfinished tasks from earlier days carry over to today.
- **Career**
  - *Overview*: this week's and this month's activity, the application pipeline, pending applications and recent activity.
  - *Applications*: search, filter by status or country, and change a status right in the list. Every change is dated.
  - *Opportunities*: roles you've researched, with Go relevance, remote, relocation, visa sponsorship,
    experience, skills and your own profile-match rating. Researching a role doesn't count as applying.
  - *Outreach*: cold and recruiter emails, LinkedIn messages, referral requests and follow-ups, with replies.
  - *Companies*: every company with its contacts (recruiters, hiring managers), opportunities,
    applications and outreach.
- **Analytics**: Week or Month view with daily completion, a day-by-day grid or calendar heatmap, per-habit
  completion and streaks, activity totals (learning hours, gym sessions, LeetCode by difficulty…),
  8-week habit trends, and career numbers with an optional country filter. Countries are never ranked or
  recommended.
- **Habits**: add, edit, reorder and archive habits. Habits can be Yes/No, duration or count types, with a
  daily target, scheduled weekdays, optional sub-counters (e.g. LeetCode easy/medium/hard), a reminder
  time and notes.
- **Settings**: display name, where your data lives, manual backup and JSON export.

A fresh database starts with these habits: No Smoking, Learning, Communication, Gym, LeetCode,
Job Outreach and Opportunity Analysis. Edit or archive any of them on the Habits page.

## How numbers are calculated

All statistics are computed from the stored daily records:

- **Progress** for a day is `value / target`, capped at 100%. For example, 7 of 10 emails is 70%.
  A Yes/No habit is 0% or 100%.
- A day is **done** when progress reaches 100%.
- **Daily score** is the average progress of that day's scheduled habits, so partial work counts.
- **Streaks** count consecutive *scheduled* days that are done. A habit scheduled Mon/Wed/Fri isn't broken
  by a Tuesday, and today doesn't break a streak until the day is over.
- Changing a target applies from today onwards. Past days keep the target they were logged with.
- Archiving a habit hides it but keeps its history.

### Job Outreach and career numbers

- **Job Outreach** is never typed in by hand. Its daily value is every outreach message sent that day plus
  every application submitted that day (4 emails + 3 applications + 2 LinkedIn messages = 9/10). It is
  recalculated whenever career records change and checked again on every start.
- An application counts once it's submitted (Applied or later), on its application date. Saved, Researching
  and Ready to Apply don't count.
- Weekly and monthly career numbers count activity on the day it happened: outreach by its date,
  applications by application date, and responses, interviews, follow-ups and rejections by the date the
  status changed. **Companies researched** counts companies by the date they were added. **Pending** is a
  current snapshot of submitted applications with no final outcome yet (Applied, Follow-up, Response, Interview).
- The country filter uses the opportunity's country, or the company's country if the opportunity has none.

## Project layout

```
cmd/tracker/        entry point: flags, database, backups, HTTP server
internal/stats/     pure habit calculations (progress, streaks, summaries)
internal/store/     SQLite access, career metrics, Job Outreach sync, embedded SQL migrations
internal/web/       handlers, HTML templates, CSS and a little plain JavaScript
```

Run tests with `go test ./...`. They cover the streak and progress logic, the career data layer and
Job Outreach sync, and a full end-to-end flow through the web server. To change the schema, add a new numbered file in
`internal/store/migrations/`. It's applied automatically on the next start.
# tracker
