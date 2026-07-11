package server

import (
	"path/filepath"
	"testing"
	"time"

	"embytool/internal/notifier"
)

func TestImportStatsStorePersistsAndDeduplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "import_stats.json")
	store, err := newImportStatsStore(path)
	if err != nil {
		t.Fatalf("newImportStatsStore() error = %v", err)
	}

	now := startOfImportDay(time.Now()).Add(10 * time.Hour)
	event := &notifier.Event{
		Event:      "library.new",
		ServerName: "emby-a",
		ItemID:     "movie-1",
	}
	recorded, err := store.recordAt(event, now)
	if err != nil {
		t.Fatalf("recordAt() error = %v", err)
	}
	if !recorded {
		t.Fatal("recordAt() recorded = false, want true")
	}

	recorded, err = store.recordAt(event, now)
	if err != nil {
		t.Fatalf("second recordAt() error = %v", err)
	}
	if recorded {
		t.Fatal("second recordAt() recorded = true, want false for duplicate")
	}

	recorded, err = store.recordAt(&notifier.Event{
		Event:      "library.new",
		ServerName: "emby-a",
		ItemID:     "episode-2",
	}, now)
	if err != nil {
		t.Fatalf("record second item error = %v", err)
	}
	if !recorded {
		t.Fatal("record second item recorded = false, want true")
	}

	overview := store.overviewAt(now)
	if overview.TodayCount != 2 || overview.Last7DaysCount != 2 || overview.Last30DaysCount != 2 {
		t.Fatalf("unexpected overview totals: %+v", overview)
	}
	if overview.PeakCount != 2 || overview.PeakDate != importDayKey(now) {
		t.Fatalf("unexpected peak: %+v", overview)
	}

	reloaded, err := newImportStatsStore(path)
	if err != nil {
		t.Fatalf("reload import stats error = %v", err)
	}
	reloadedOverview := reloaded.overviewAt(now)
	if reloadedOverview.TodayCount != 2 || reloadedOverview.Last30DaysCount != 2 {
		t.Fatalf("persisted overview = %+v, want two records", reloadedOverview)
	}
}

func TestImportStatsStoreBuildsContinuousThirtyDayWindow(t *testing.T) {
	now := startOfImportDay(time.Now()).Add(8 * time.Hour)
	oldDay := importDayKey(now.AddDate(0, 0, -overviewWindowDays))
	firstDay := importDayKey(now.AddDate(0, 0, -(overviewWindowDays - 1)))
	today := importDayKey(now)
	store := &importStatsStore{
		days: map[string]int{
			oldDay:   99,
			firstDay: 3,
			today:    4,
		},
		seen: map[string]string{},
	}

	overview := store.overviewAt(now)
	if len(overview.Days) != overviewWindowDays {
		t.Fatalf("days length = %d, want %d", len(overview.Days), overviewWindowDays)
	}
	if overview.Days[0].Date != firstDay {
		t.Fatalf("first overview day = %s, want %s", overview.Days[0].Date, firstDay)
	}
	if overview.Days[len(overview.Days)-1].Date != today {
		t.Fatalf("last overview day = %s, want %s", overview.Days[len(overview.Days)-1].Date, today)
	}
	if overview.Last30DaysCount != 7 {
		t.Fatalf("last 30 days count = %d, want 7", overview.Last30DaysCount)
	}
	if overview.TodayCount != 4 {
		t.Fatalf("today count = %d, want 4", overview.TodayCount)
	}
	if overview.PeakCount != 4 || overview.PeakDate != today {
		t.Fatalf("unexpected peak: %+v", overview)
	}
}
