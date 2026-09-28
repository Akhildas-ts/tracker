package web

import (
	"fmt"
	"html"
	"html/template"
	"math"
	"strconv"
	"strings"
	"time"

	"tracker/internal/stats"
	"tracker/internal/store"
)

var funcs = template.FuncMap{
	"pct":      pct,
	"num":      num,
	"dur":      duration,
	"value":    formatValue,
	"target":   targetLabel,
	"schedule": scheduleLabel,
	"options":  optionTags,
	"initial":  initial,
	"short":    shortDate,
	"since":    daysSince,

	"statusLabel":   func(v string) string { return store.Label(store.StatusOptions, v) },
	"typeLabel":     func(v string) string { return store.Label(store.OutreachTypes, v) },
	"replyLabel":    func(v string) string { return store.Label(store.OutreachStatuses, v) },
	"matchLabel":    func(v string) string { return store.Label(store.MatchOptions, v) },
	"goLabel":       func(v string) string { return store.Label(store.GoRelevanceOptions, v) },
	"remoteLabel":   func(v string) string { return store.Label(store.RemoteOptions, v) },
	"statusOptions": func() []store.Option { return store.StatusOptions },
	"emojiPicks": func() []string {
		return []string{"✅", "🏃", "🏋️", "🚶", "🧘", "💧", "😴", "🥗", "🚭", "📚", "📖", "✍️", "🗣️", "🧠", "💻", "🎸", "🌍", "📧", "💰", "🧹"}
	},
}

// optionTags renders <option> elements with the selected value marked.
func optionTags(opts []store.Option, selected string) template.HTML {
	var b strings.Builder
	for _, o := range opts {
		sel := ""
		if o.Value == selected {
			sel = " selected"
		}
		fmt.Fprintf(&b, `<option value="%s"%s>%s</option>`, html.EscapeString(o.Value), sel, html.EscapeString(o.Label))
	}
	return template.HTML(b.String())
}

func initial(name string) string {
	for _, r := range strings.TrimSpace(name) {
		return strings.ToUpper(string(r))
	}
	return "·"
}

// shortDate turns "2026-09-21" into "Sep 21" ("" stays "").
func shortDate(d string) string {
	t, err := stats.ParseDay(d)
	if err != nil {
		return d
	}
	return t.Format("Jan 2")
}

// daysSince returns whole days from date d to today (0 for invalid dates).
func daysSince(d string) int {
	t, err := stats.ParseDay(d)
	if err != nil {
		return 0
	}
	return int(stats.Today(time.Now()).Sub(t).Hours() / 24)
}

var weekdayNames = []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

func pct(p float64) int { return int(math.Round(p * 100)) }

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// duration formats minutes as "45m", "1h" or "1h 10m".
func duration(minutes float64) string {
	m := int(math.Round(minutes))
	h, r := m/60, m%60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", r)
	case r == 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, r)
}

func formatValue(h store.Habit, v float64) string {
	switch h.Kind {
	case store.KindDuration:
		return duration(v)
	case store.KindBinary:
		if v >= 1 {
			return "✓"
		}
		return "✗"
	}
	return num(v)
}

func targetLabel(h store.Habit) string {
	switch h.Kind {
	case store.KindBinary:
		return "Yes / No"
	case store.KindDuration:
		return duration(h.Target) + " / day"
	}
	return strings.TrimSpace(num(h.Target)+" "+h.Unit) + " / day"
}

func scheduleLabel(mask int) string {
	switch mask {
	case store.EveryDay:
		return "Every day"
	case 0b0011111:
		return "Weekdays"
	case 0b1100000:
		return "Weekends"
	}
	var days []string
	for i, n := range weekdayNames {
		if mask&(1<<i) != 0 {
			days = append(days, n)
		}
	}
	return strings.Join(days, ", ")
}

// totalLabel describes a habit's logged total over a period; a nil
// breakdown leaves out the sub-counters.
func totalLabel(h store.Habit, total float64, breakdown map[string]float64) string {
	switch h.Kind {
	case store.KindBinary:
		if total == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%s days", num(total))
	case store.KindDuration:
		return duration(total)
	}
	s := strings.TrimSpace(num(total) + " " + h.Unit)
	if keys := h.BreakdownKeys(); len(keys) > 0 && breakdown != nil {
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = fmt.Sprintf("%s %s", k, num(breakdown[k]))
		}
		s += " (" + strings.Join(parts, " · ") + ")"
	}
	return s
}
