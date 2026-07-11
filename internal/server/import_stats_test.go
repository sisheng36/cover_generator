package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"embytool/internal/config"
	"embytool/internal/emby"
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

func TestImportStatsStoreRecentImportsMoveExistingMediaToFront(t *testing.T) {
	path := filepath.Join(t.TempDir(), "import_stats.json")
	store, err := newImportStatsStore(path)
	if err != nil {
		t.Fatalf("newImportStatsStore() error = %v", err)
	}

	now := startOfImportDay(time.Now()).Add(9 * time.Hour)
	uniqueEvents := []*notifier.Event{
		{
			Event:    "library.new",
			ItemID:   "movie-1",
			ItemType: "MOV",
			ItemName: "电影一",
		},
		{
			Event:    "library.new",
			ItemID:   "movie-2",
			ItemType: "MOV",
			ItemName: "电影二",
		},
		{
			Event:      "library.new",
			ItemID:     "episode-1",
			ItemType:   "TV",
			SeriesID:   "series-1",
			SeriesName: "剧集一",
		},
	}
	for _, event := range uniqueEvents {
		if recorded, err := store.recordAt(event, now); err != nil || !recorded {
			t.Fatalf("recordAt(%+v) = (%v, %v), want (true, nil)", event, recorded, err)
		}
	}
	if recorded, err := store.recordAt(uniqueEvents[0], now); err != nil || recorded {
		t.Fatalf("duplicate movie recordAt() = (%v, %v), want (false, nil)", recorded, err)
	}
	if recorded, err := store.recordAt(&notifier.Event{
		Event:      "library.new",
		ItemID:     "episode-2",
		ItemType:   "TV",
		SeriesID:   "series-1",
		SeriesName: "剧集一",
	}, now); err != nil || !recorded {
		t.Fatalf("latest series episode recordAt() = (%v, %v), want (true, nil)", recorded, err)
	}

	overview := store.overviewAt(now)
	if len(overview.RecentItems) != 3 {
		t.Fatalf("recent item count = %d, want 3", len(overview.RecentItems))
	}
	if overview.RecentItems[0].ID != "TV\x00series-1" || overview.RecentItems[0].Name != "剧集一" {
		t.Fatalf("first recent item = %+v, want latest series", overview.RecentItems[0])
	}
	if overview.RecentItems[1].ID != "MOV\x00movie-1" || overview.RecentItems[2].ID != "MOV\x00movie-2" {
		t.Fatalf("recent item order = %+v, want series, movie-1, movie-2", overview.RecentItems)
	}

	entry, ok := store.recentImport("TV\x00series-1")
	if !ok {
		t.Fatal("recentImport() did not find the series")
	}
	if entry.ItemID != "series-1" || entry.FallbackItemID != "episode-2" {
		t.Fatalf("stored series entry = %+v, want latest episode fallback", entry)
	}

	reloaded, err := newImportStatsStore(path)
	if err != nil {
		t.Fatalf("reload import stats error = %v", err)
	}
	reloadedOverview := reloaded.overviewAt(now)
	if len(reloadedOverview.RecentItems) != 3 || reloadedOverview.RecentItems[0].ID != "TV\x00series-1" {
		t.Fatalf("persisted recent items = %+v", reloadedOverview.RecentItems)
	}
}

func TestRecentImportPosterPathFallsBackToLatestEpisode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Items/series-name":
			http.NotFound(w, r)
		case "/Items/episode-2":
			writeJSON(w, http.StatusOK, map[string]any{
				"Id":                    "episode-2",
				"Type":                  "Episode",
				"SeriesId":              "series-1",
				"SeriesPrimaryImageTag": "series-poster",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	path := recentImportPosterPath(context.Background(), emby.New(server.URL, "api-key"), recentImport{
		ItemID:         "series-name",
		FallbackItemID: "episode-2",
		Type:           "TV",
	})
	want := "/emby/Items/series-1/Images/Primary?tag=series-poster&maxWidth=320&maxHeight=480&quality=90"
	if path != want {
		t.Fatalf("recentImportPosterPath() = %q, want %q", path, want)
	}
}

func TestHandleRecentImportPosterProxiesPrimaryImage(t *testing.T) {
	const posterBody = "poster-bytes"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Items/movie-1":
			writeJSON(w, http.StatusOK, map[string]any{
				"Id":        "movie-1",
				"Type":      "Movie",
				"ImageTags": map[string]string{"Primary": "poster-tag"},
			})
		case "/emby/Items/movie-1/Images/Primary":
			if r.Header.Get("X-Emby-Token") != "api-key" {
				http.Error(w, "missing Emby token", http.StatusUnauthorized)
				return
			}
			query := r.URL.Query()
			if query.Get("tag") != "poster-tag" || query.Get("maxWidth") != "320" || query.Get("maxHeight") != "480" || query.Get("quality") != "90" {
				http.Error(w, "unexpected image query", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte(posterBody))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	entry := recentImport{
		Key:    "MOV\x00movie-1",
		ItemID: "movie-1",
		Name:   "电影一",
		Type:   "MOV",
	}
	s := &Server{
		cfg: config.Config{
			EmbyServerURL: upstream.URL,
			EmbyAPIKey:    "api-key",
		},
		importStats: &importStatsStore{
			days:   map[string]int{},
			seen:   map[string]string{},
			recent: []recentImport{entry},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/recent-imports/poster?id="+url.QueryEscape(entry.Key), nil)
	response := httptest.NewRecorder()
	s.handleRecentImportPoster(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("poster response status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("poster content type = %q, want image/jpeg", response.Header().Get("Content-Type"))
	}
	if response.Header().Get("Cache-Control") != "private, max-age=300" {
		t.Fatalf("poster cache control = %q", response.Header().Get("Cache-Control"))
	}
	if response.Body.String() != posterBody {
		t.Fatalf("poster body = %q, want %q", response.Body.String(), posterBody)
	}
}

func TestPrependRecentImportLimitsPosterWallToThirtyTwoItems(t *testing.T) {
	items := make([]recentImport, 0, recentImportLimit)
	for i := 0; i < recentImportLimit+1; i++ {
		itemID := "movie-" + strconv.Itoa(i)
		item := recentImport{
			Key:    "MOV\x00" + itemID,
			ItemID: itemID,
			Name:   "movie",
			Type:   "MOV",
		}
		items = prependRecentImport(items, item)
	}
	if len(items) != recentImportLimit {
		t.Fatalf("recent item count = %d, want %d", len(items), recentImportLimit)
	}
	if items[0].ItemID != "movie-32" {
		t.Fatalf("first recent item = %s, want newest item", items[0].ItemID)
	}
	if items[len(items)-1].ItemID != "movie-1" {
		t.Fatalf("last recent item = %s, want oldest retained item", items[len(items)-1].ItemID)
	}
}
