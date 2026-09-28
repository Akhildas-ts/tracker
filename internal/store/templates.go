package store

// HabitTemplate is a ready-made habit offered during first-run setup.
type HabitTemplate struct {
	Key   string
	Group string
	Habit Habit
}

// HabitTemplates are suggestions only: everything can be edited, archived or
// deleted afterwards, and users can add their own habits.
var HabitTemplates = []HabitTemplate{
	{"no-smoking", "Health", Habit{Name: "No Smoking", Emoji: "🚭", Kind: KindBinary, Target: 1, Step: 1, Description: "Smoke-free today"}},
	{"workout", "Health", Habit{Name: "Gym", Emoji: "🏋️", Kind: KindBinary, Target: 1, Step: 1, Description: "Worked out today"}},
	{"walk", "Health", Habit{Name: "Walk", Emoji: "🚶", Kind: KindCount, Target: 8000, Unit: "steps", Step: 1000}},
	{"water", "Health", Habit{Name: "Drink water", Emoji: "💧", Kind: KindCount, Target: 8, Unit: "glasses", Step: 1}},
	{"sleep", "Health", Habit{Name: "Sleep on time", Emoji: "😴", Kind: KindBinary, Target: 1, Step: 1}},
	{"meditation", "Health", Habit{Name: "Meditation", Emoji: "🧘", Kind: KindDuration, Target: 10, Unit: "min", Step: 5}},

	{"learning", "Growth", Habit{Name: "Learning", Emoji: "📚", Kind: KindDuration, Target: 60, Unit: "min", Step: 15}},
	{"reading", "Growth", Habit{Name: "Reading", Emoji: "📖", Kind: KindDuration, Target: 30, Unit: "min", Step: 10}},
	{"communication", "Growth", Habit{Name: "Communication", Emoji: "🗣️", Kind: KindDuration, Target: 30, Unit: "min", Step: 10}},
	{"journal", "Growth", Habit{Name: "Journaling", Emoji: "✍️", Kind: KindBinary, Target: 1, Step: 1}},

	{"leetcode", "Job search", Habit{Name: "LeetCode", Emoji: "🧠", Kind: KindCount, Target: 2, Unit: "problems", Step: 1, Breakdown: "easy,medium,hard"}},
	{"job-outreach", "Job search", Habit{Name: "Job Outreach", Emoji: "📧", Kind: KindCount, Target: 10, Unit: "sent", Step: 1,
		Source: SourceOutreach, Description: "Counted automatically from Career: outreach messages + applications submitted"}},
	{"opportunity-analysis", "Job search", Habit{Name: "Opportunity Analysis", Emoji: "🌍", Kind: KindDuration, Target: 30, Unit: "min", Step: 15}},
}

// CreateFromTemplates adds the chosen templates as habits starting on today.
// Unknown keys are ignored.
func (s *Store) CreateFromTemplates(keys []string, today string) error {
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	for _, t := range HabitTemplates {
		if !want[t.Key] {
			continue
		}
		h := t.Habit
		h.ScheduleDays, h.StartDate = EveryDay, today
		if err := s.CreateHabit(&h); err != nil {
			return err
		}
	}
	return s.ResyncOutreach()
}

// jobSearchStarter is the job-search starter set used by SeedDefaults.
var jobSearchStarter = []string{"no-smoking", "learning", "communication", "workout", "leetcode", "job-outreach", "opportunity-analysis"}

// SeedDefaults creates the job-search starter habits on an empty database and
// marks setup as done. Used by tests and demos; real users pick on /welcome.
func (s *Store) SeedDefaults(today string) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM habits`).Scan(&n); err != nil || n > 0 {
		return err
	}
	if err := s.CreateFromTemplates(jobSearchStarter, today); err != nil {
		return err
	}
	return s.SetSetting(KeySetupDone, "1")
}
