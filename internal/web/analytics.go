package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"tracker/internal/stats"
	"tracker/internal/store"
)

// bar and chart feed the "bars" template: a single-series column chart.
type bar struct {
	Label  string
	Tip    string
	Height int // 0..100, percent of the chart height
	Muted  bool
}

type chart struct {
	Label string // accessible description
	Bars  []bar
	Dense bool
}

func barHeight(v, max float64) int {
	if max <= 0 {
		return 0
	}
	return pct(min(v/max, 1))
}

type trendRow struct {
	H       store.Habit
	Chart   chart
	Average float64 // over the weeks the habit was tracked
}

type careerWeek struct {
	Label string
	M     store.CareerMetrics
}

type totalCard struct {
	H     store.Habit
	Total string
	Sub   string
}

type analyticsView struct {
	page
	Period    string // "week" or "month"
	Label     string
	PrevURL   string
	NextURL   string
	WeekURL   string
	MonthURL  string
	Start     string // week start, or "YYYY-MM" for a month
	Country   string
	Countries []string

	Rows []habitReport
	periodTotals

	Days      []dayHead    // week only
	DayScores []*score     // week only
	Weeks     [][]heatCell // month only

	ScoreChart    chart
	Totals        []totalCard
	Trends        []trendRow
	Career        store.CareerMetrics
	OutreachChart chart
	CareerWeeks   []careerWeek
}

func (s *Server) analytics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	today := s.todayDate()
	v := analyticsView{page: page{Nav: "analytics", Title: "Analytics"}, Period: "week", Country: q.Get("country")}
	if q.Get("period") == "month" {
		v.Period = "month"
	}

	var from, to time.Time
	link := func(period, key, val string) string {
		u := url.Values{"period": {period}}
		if val != "" {
			u.Set(key, val)
		}
		if v.Country != "" {
			u.Set("country", v.Country)
		}
		return "/analytics?" + u.Encode()
	}
	if v.Period == "week" {
		from = stats.Monday(s.dateParam(r, "start"))
		to = from.AddDate(0, 0, 6)
		v.Start = stats.Key(from)
		v.Label = from.Format("Jan 2") + " – " + to.Format("Jan 2, 2006")
		v.PrevURL = link("week", "start", stats.Key(from.AddDate(0, 0, -7)))
		if next := from.AddDate(0, 0, 7); !next.After(today) {
			v.NextURL = link("week", "start", stats.Key(next))
		}
	} else {
		from = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
		if m, err := time.Parse("2006-01", q.Get("m")); err == nil && !m.After(from) {
			from = m
		}
		to = from.AddDate(0, 1, -1)
		v.Start = from.Format("2006-01")
		v.Label = from.Format("January 2006")
		v.PrevURL = link("month", "m", from.AddDate(0, -1, 0).Format("2006-01"))
		if next := from.AddDate(0, 1, 0); !next.After(today) {
			v.NextURL = link("month", "m", next.Format("2006-01"))
		}
	}
	v.WeekURL = link("week", "", "")
	v.MonthURL = link("month", "", "")

	hs, err := s.load()
	if err != nil {
		s.fail(w, err)
		return
	}
	if v.Rows, v.periodTotals, err = s.report(hs, from, to, today); err != nil {
		s.fail(w, err)
		return
	}

	// Period detail: a habit × day grid for a week, a heatmap for a month.
	if v.Period == "week" {
		for i := 0; i < 7; i++ {
			d := from.AddDate(0, 0, i)
			v.Days = append(v.Days, dayHead{Name: weekdayNames[i], Num: d.Format("2"), Today: d.Equal(today)})
			if d.After(today) {
				v.DayScores = append(v.DayScores, nil)
			} else {
				sc := dayScore(hs, d)
				v.DayScores = append(v.DayScores, &sc)
			}
		}
		for i, h := range hs {
			for j := 0; j < 7; j++ {
				v.Rows[i].Cells = append(v.Rows[i].Cells, weekCell(h, from.AddDate(0, 0, j), today))
			}
		}
	} else {
		v.Weeks = heatmap(hs, from, to, today)
	}

	// Daily completion chart.
	v.ScoreChart = chart{Label: "Daily habit completion, " + v.Label, Dense: v.Period == "month"}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		b := bar{Label: d.Format("2")}
		if v.Period == "week" {
			b.Label = weekdayNames[(int(d.Weekday())+6)%7]
		}
		if d.After(today) {
			b.Muted, b.Tip = true, d.Format("Mon, Jan 2")+" · upcoming"
		} else {
			sc := dayScore(hs, d)
			b.Height = pct(sc.Progress)
			b.Tip = fmt.Sprintf("%s · %d%% · %d/%d habits", d.Format("Mon, Jan 2"), pct(sc.Progress), sc.Completed, sc.Total)
		}
		v.ScoreChart.Bars = append(v.ScoreChart.Bars, b)
	}

	// Activity totals per habit.
	for i, row := range v.Rows {
		sub := fmt.Sprintf("%d/%d days on target", row.Sum.Done, row.Sum.Scheduled)
		if keys := row.H.BreakdownKeys(); len(keys) > 0 {
			bd := breakdownTotals(hs[i], from, to)
			parts := make([]string, len(keys))
			for j, k := range keys {
				parts[j] = fmt.Sprintf("%s %s", k, num(bd[k]))
			}
			sub = strings.Join(parts, " · ") + " · " + sub
		}
		v.Totals = append(v.Totals, totalCard{H: row.H, Total: totalLabel(row.H, row.Sum.Total, nil), Sub: sub})
	}

	// Habit trends: completion per week for the 8 weeks ending with this period.
	lastWeek := stats.Monday(minTime(to, today))
	for _, h := range hs {
		tr := trendRow{H: h.Habit, Chart: chart{Label: h.Name + " weekly completion, last 8 weeks"}}
		var avg float64
		var tracked int
		for i := 7; i >= 0; i-- {
			ws := lastWeek.AddDate(0, 0, -7*i)
			sum := stats.Summarize(h.sched, h.ent, ws, ws.AddDate(0, 0, 6), today)
			b := bar{Label: ws.Format("1/2"), Height: pct(sum.Progress),
				Tip: fmt.Sprintf("Week of %s · %d%% · %d/%d days", ws.Format("Jan 2"), pct(sum.Progress), sum.Done, sum.Scheduled)}
			if sum.Scheduled == 0 {
				b.Muted, b.Tip = true, "Week of "+ws.Format("Jan 2")+" · not tracked"
			}
			if sum.Scheduled > 0 {
				tracked++
				avg += sum.Progress
			}
			tr.Chart.Bars = append(tr.Chart.Bars, b)
		}
		if tracked > 0 {
			tr.Average = avg / float64(tracked)
		}
		v.Trends = append(v.Trends, tr)
	}

	// Career, optionally for one country.
	if v.Countries, err = s.store.Countries(); err != nil {
		s.fail(w, err)
		return
	}
	if v.Career, err = s.store.CareerMetrics(stats.Key(from), stats.Key(to), v.Country); err != nil {
		s.fail(w, err)
		return
	}
	daily, err := s.store.DailyOutreach(stats.Key(from), stats.Key(to), v.Country)
	if err != nil {
		s.fail(w, err)
		return
	}
	target := s.outreachTarget()
	peak := target
	for _, n := range daily {
		peak = max(peak, float64(n))
	}
	v.OutreachChart = chart{Label: "Job outreach per day, " + v.Label, Dense: v.Period == "month"}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		n := daily[stats.Key(d)]
		b := bar{Label: d.Format("2"), Height: barHeight(float64(n), peak)}
		if v.Period == "week" {
			b.Label = weekdayNames[(int(d.Weekday())+6)%7]
		}
		b.Tip = fmt.Sprintf("%s · %d sent", d.Format("Mon, Jan 2"), n)
		if target > 0 {
			b.Tip += fmt.Sprintf(" of %s", num(target))
		}
		if d.After(today) {
			b.Muted, b.Tip = true, d.Format("Mon, Jan 2")+" · upcoming"
		}
		v.OutreachChart.Bars = append(v.OutreachChart.Bars, b)
	}
	for i := 7; i >= 0; i-- {
		ws := lastWeek.AddDate(0, 0, -7*i)
		m, err := s.store.CareerMetrics(stats.Key(ws), stats.Key(ws.AddDate(0, 0, 6)), v.Country)
		if err != nil {
			s.fail(w, err)
			return
		}
		v.CareerWeeks = append(v.CareerWeeks, careerWeek{Label: ws.Format("Jan 2"), M: m})
	}
	s.render(w, http.StatusOK, "analytics", v)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
