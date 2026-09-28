// Package stats derives progress, streaks and summaries from daily entries.
// It has no database or HTTP dependencies.
//
// Days are calendar dates represented as UTC midnight, so adding days is
// never affected by daylight-saving changes.
package stats

import "time"

const Layout = "2006-01-02"

func ParseDay(s string) (time.Time, error) { return time.Parse(Layout, s) }

// Today returns the local calendar date of now.
func Today(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func Key(d time.Time) string { return d.Format(Layout) }

// Monday returns the Monday of d's week.
func Monday(d time.Time) time.Time { return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7)) }

// WeekdayBit returns d's bit in a schedule mask where bit 0 is Monday.
func WeekdayBit(d time.Time) int { return 1 << ((int(d.Weekday()) + 6) % 7) }

type Habit struct {
	ScheduleDays int
	Start        time.Time
}

func (h Habit) ScheduledOn(d time.Time) bool {
	return !d.Before(h.Start) && h.ScheduleDays&WeekdayBit(d) != 0
}

// Entry is one logged day. Target is the habit's target at the time.
type Entry struct {
	Value  float64
	Target float64
}

// Entries maps a date key (see Key) to that day's entry.
type Entries map[string]Entry

// Progress returns value/target clamped to [0, 1].
func Progress(value, target float64) float64 {
	if target <= 0 {
		return 1
	}
	p := value / target
	switch {
	case p < 0:
		return 0
	case p > 1:
		return 1
	}
	return p
}

// On returns the progress for day d; an unlogged day has zero progress.
func (e Entries) On(d time.Time) (progress float64, done bool) {
	en, ok := e[Key(d)]
	if !ok {
		return 0, false
	}
	p := Progress(en.Value, en.Target)
	return p, p >= 1
}

// CurrentStreak counts consecutive scheduled days completed up to today.
// If today is not done yet it does not break the streak: counting starts
// from yesterday instead. Unscheduled days are skipped.
func CurrentStreak(h Habit, e Entries, today time.Time) int {
	d := today
	if _, done := e.On(today); !done {
		d = d.AddDate(0, 0, -1)
	}
	n := 0
	for ; !d.Before(h.Start); d = d.AddDate(0, 0, -1) {
		if !h.ScheduledOn(d) {
			continue
		}
		if _, done := e.On(d); !done {
			break
		}
		n++
	}
	return n
}

// LongestStreak returns the longest run of completed scheduled days up to today.
func LongestStreak(h Habit, e Entries, today time.Time) int {
	best, run := 0, 0
	for d := h.Start; !d.After(today); d = d.AddDate(0, 0, 1) {
		if !h.ScheduledOn(d) {
			continue
		}
		if _, done := e.On(d); done {
			run++
			best = max(best, run)
		} else {
			run = 0
		}
	}
	return best
}

type Summary struct {
	Scheduled int     // scheduled days in the range, up to and including today
	Done      int     // scheduled days that met the target
	Missed    int     // scheduled days before today that did not meet the target
	Progress  float64 // average daily progress over scheduled days, 0..1
	Total     float64 // sum of logged values in the range (including unscheduled days)
}

// Summarize aggregates a habit over [from, to]; days after today are ignored.
func Summarize(h Habit, e Entries, from, to, today time.Time) Summary {
	var s Summary
	var sum float64
	if to.After(today) {
		to = today
	}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if d.Before(h.Start) {
			continue
		}
		if en, ok := e[Key(d)]; ok {
			s.Total += en.Value
		}
		if !h.ScheduledOn(d) {
			continue
		}
		s.Scheduled++
		p, done := e.On(d)
		sum += p
		if done {
			s.Done++
		} else if d.Before(today) {
			s.Missed++
		}
	}
	if s.Scheduled > 0 {
		s.Progress = sum / float64(s.Scheduled)
	}
	return s
}

// Overall combines several summaries into one progress value, weighting
// each habit by its number of scheduled days.
func Overall(ss []Summary) float64 {
	var sum float64
	var n int
	for _, s := range ss {
		sum += s.Progress * float64(s.Scheduled)
		n += s.Scheduled
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}
