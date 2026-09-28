package stats

import (
	"math"
	"testing"
	"time"
)

func day(s string) time.Time {
	d, err := ParseDay(s)
	if err != nil {
		panic(err)
	}
	return d
}

// entries builds Entries from date → value pairs with the given target.
func entries(target float64, vals map[string]float64) Entries {
	e := Entries{}
	for k, v := range vals {
		e[k] = Entry{Value: v, Target: target}
	}
	return e
}

// 2026-09-21 is a Monday.
var daily = Habit{ScheduleDays: 127, Start: day("2026-09-01")}

func TestProgress(t *testing.T) {
	for _, tc := range []struct{ v, t, want float64 }{
		{10, 10, 1}, {8, 10, 0.8}, {5, 10, 0.5}, {0, 10, 0}, {15, 10, 1}, {-1, 10, 0}, {3, 0, 1},
	} {
		if got := Progress(tc.v, tc.t); got != tc.want {
			t.Errorf("Progress(%v, %v) = %v, want %v", tc.v, tc.t, got, tc.want)
		}
	}
}

func TestCurrentStreak(t *testing.T) {
	today := day("2026-09-28")
	tests := []struct {
		name string
		h    Habit
		e    Entries
		want int
	}{
		{"nothing logged", daily, Entries{}, 0},
		{"today done", daily, entries(1, map[string]float64{"2026-09-26": 1, "2026-09-27": 1, "2026-09-28": 1}), 3},
		{"today pending keeps streak", daily, entries(1, map[string]float64{"2026-09-26": 1, "2026-09-27": 1}), 2},
		{"today logged as missed still counts yesterday", daily, entries(1, map[string]float64{"2026-09-27": 1, "2026-09-28": 0}), 1},
		{"gap breaks streak", daily, entries(1, map[string]float64{"2026-09-25": 1, "2026-09-27": 1, "2026-09-28": 1}), 2},
		{"partial does not count", daily, entries(10, map[string]float64{"2026-09-27": 7, "2026-09-28": 10}), 1},
		{
			"unscheduled days are skipped",
			Habit{ScheduleDays: 1<<0 | 1<<2 | 1<<4, Start: day("2026-09-01")}, // Mon, Wed, Fri
			entries(1, map[string]float64{"2026-09-23": 1, "2026-09-25": 1, "2026-09-28": 1}),
			3,
		},
		{"stops at start date", Habit{ScheduleDays: 127, Start: day("2026-09-27")}, entries(1, map[string]float64{"2026-09-26": 1, "2026-09-27": 1, "2026-09-28": 1}), 2},
	}
	for _, tc := range tests {
		if got := CurrentStreak(tc.h, tc.e, today); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestLongestStreak(t *testing.T) {
	e := entries(1, map[string]float64{
		"2026-09-01": 1, "2026-09-02": 1, "2026-09-03": 1, "2026-09-04": 1,
		"2026-09-10": 1, "2026-09-11": 1,
		"2026-09-27": 1, "2026-09-28": 1,
	})
	if got := LongestStreak(daily, e, day("2026-09-28")); got != 4 {
		t.Errorf("got %d, want 4", got)
	}
}

func TestTargetSnapshot(t *testing.T) {
	// The target was 10 on the 27th and raised to 15 on the 28th: 10 still counts as done on the 27th.
	e := Entries{"2026-09-27": {Value: 10, Target: 10}, "2026-09-28": {Value: 10, Target: 15}}
	if got := CurrentStreak(daily, e, day("2026-09-28")); got != 1 {
		t.Errorf("got %d, want 1", got)
	}
}

func TestSummarize(t *testing.T) {
	today := day("2026-09-24") // Thursday
	e := entries(10, map[string]float64{
		"2026-09-21": 10, "2026-09-22": 5, "2026-09-24": 8,
		"2026-09-27": 10, // future, ignored
	})
	s := Summarize(daily, e, day("2026-09-21"), day("2026-09-27"), today)
	want := Summary{Scheduled: 4, Done: 1, Missed: 2, Progress: (1 + 0.5 + 0 + 0.8) / 4, Total: 23}
	if s.Scheduled != want.Scheduled || s.Done != want.Done || s.Missed != want.Missed || s.Total != want.Total ||
		math.Abs(s.Progress-want.Progress) > 1e-9 {
		t.Errorf("got %+v, want %+v", s, want)
	}
}

func TestSummarizeCountsUnscheduledTotals(t *testing.T) {
	gym := Habit{ScheduleDays: 1 << 0, Start: day("2026-09-01")} // Mondays only
	e := entries(1, map[string]float64{"2026-09-21": 1, "2026-09-22": 1})
	s := Summarize(gym, e, day("2026-09-21"), day("2026-09-27"), day("2026-09-27"))
	if s.Scheduled != 1 || s.Done != 1 || s.Total != 2 || s.Progress != 1 {
		t.Errorf("got %+v", s)
	}
}

func TestOverall(t *testing.T) {
	got := Overall([]Summary{{Scheduled: 7, Progress: 1}, {Scheduled: 3, Progress: 0}, {}})
	if math.Abs(got-0.7) > 1e-9 {
		t.Errorf("got %v, want 0.7", got)
	}
}
