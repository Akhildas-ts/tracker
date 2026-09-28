package web

import (
	"fmt"
	"math"
	"time"

	"tracker/internal/stats"
	"tracker/internal/store"
)

// habitReport is one habit's figures for a period.
type habitReport struct {
	H          store.Habit
	Cells      []cell // week view only
	Sum        stats.Summary
	Streak     int
	Best       int
	TotalLabel string
}

type cell struct {
	Class string
	Text  string
}

type periodTotals struct {
	Overall    float64
	DoneDays   int
	MissedDays int
	TasksDone  int
	TasksTotal int
}

func (s *Server) report(hs []*habitData, from, to, today time.Time) ([]habitReport, periodTotals, error) {
	var tot periodTotals
	var sums []stats.Summary
	rows := make([]habitReport, 0, len(hs))
	for _, h := range hs {
		sum := stats.Summarize(h.sched, h.ent, from, to, today)
		sums = append(sums, sum)
		tot.DoneDays += sum.Done
		tot.MissedDays += sum.Missed
		rows = append(rows, habitReport{
			H:          h.Habit,
			Sum:        sum,
			Streak:     stats.CurrentStreak(h.sched, h.ent, today),
			Best:       stats.LongestStreak(h.sched, h.ent, today),
			TotalLabel: totalLabel(h.Habit, sum.Total, breakdownTotals(h, from, to)),
		})
	}
	tot.Overall = stats.Overall(sums)
	planned, done, err := s.store.TaskCounts(stats.Key(from), stats.Key(to))
	if err != nil {
		return nil, tot, err
	}
	for d, n := range planned {
		tot.TasksTotal += n
		tot.TasksDone += done[d]
	}
	return rows, tot, nil
}

func breakdownTotals(h *habitData, from, to time.Time) map[string]float64 {
	out := map[string]float64{}
	for d, e := range h.raw {
		if d >= stats.Key(from) && d <= stats.Key(to) {
			for k, v := range e.Breakdown {
				out[k] += v
			}
		}
	}
	return out
}

type dayHead struct {
	Name  string
	Num   string
	Today bool
}

func weekCell(h *habitData, d, today time.Time) cell {
	if d.After(today) {
		return cell{Class: "future"}
	}
	e, ok := h.raw[stats.Key(d)]
	if !ok {
		switch {
		case !h.sched.ScheduledOn(d):
			return cell{Class: "rest", Text: "·"}
		case d.Equal(today):
			return cell{Class: "pending", Text: "·"}
		}
		return cell{Class: "unlogged", Text: "–"}
	}
	c := cell{Text: formatValue(h.Habit, e.Value)}
	switch p := stats.Progress(e.Value, e.Target); {
	case p >= 1:
		c.Class = "done"
	case p > 0:
		c.Class = "partial"
	default:
		c.Class = "miss"
	}
	return c
}

type heatCell struct {
	Day   int
	Level int // -1 = nothing scheduled, 0..4 = progress bucket
	Title string
	Today bool
	Blank bool // padding outside the month, or a future day
}

// heatmap lays out [first, last] as Monday-first weeks of day cells.
func heatmap(hs []*habitData, first, last, today time.Time) [][]heatCell {
	var weeks [][]heatCell
	var week []heatCell
	for d := stats.Monday(first); !d.After(last) || len(week) > 0; d = d.AddDate(0, 0, 1) {
		c := heatCell{Day: d.Day(), Today: d.Equal(today), Level: -1}
		if d.Month() != first.Month() || d.After(today) {
			c.Blank = true
		} else if sc := dayScore(hs, d); sc.Total > 0 {
			c.Level = int(math.Ceil(sc.Progress * 4))
			c.Title = fmt.Sprintf("%s: %d/%d done · %d%%", d.Format("Mon, Jan 2"), sc.Completed, sc.Total, pct(sc.Progress))
		}
		week = append(week, c)
		if len(week) == 7 {
			weeks = append(weeks, week)
			week = nil
		}
	}
	return weeks
}
