package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"embytool/internal/notifier"
)

const (
	importStatsFile         = "/data/import_stats.json"
	overviewWindowDays      = 30
	importSeenRetentionDays = 31
	importStatsFileVersion  = 1
)

type dailyImportCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type importOverview struct {
	Days            []dailyImportCount `json:"days"`
	TodayCount      int                `json:"today_count"`
	Last7DaysCount  int                `json:"last_7_days_count"`
	Last30DaysCount int                `json:"last_30_days_count"`
	PeakCount       int                `json:"peak_count"`
	PeakDate        string             `json:"peak_date,omitempty"`
}

type importStatsDisk struct {
	Version int               `json:"version"`
	Days    map[string]int    `json:"days"`
	Seen    map[string]string `json:"seen"`
}

type importStatsStore struct {
	mu   sync.RWMutex
	path string
	days map[string]int
	seen map[string]string
}

func newImportStatsStore(path string) (*importStatsStore, error) {
	store := &importStatsStore{
		path: strings.TrimSpace(path),
		days: map[string]int{},
		seen: map[string]string{},
	}
	if store.path == "" {
		return store, errors.New("empty import stats path")
	}

	data, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return store, fmt.Errorf("read import stats: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return store, nil
	}

	var disk importStatsDisk
	if err := json.Unmarshal(data, &disk); err != nil {
		return store, fmt.Errorf("decode import stats: %w", err)
	}
	for day, count := range disk.Days {
		if _, ok := parseImportDay(day); ok && count > 0 {
			store.days[day] = count
		}
	}
	for key, day := range disk.Seen {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if _, ok := parseImportDay(day); ok {
			store.seen[key] = day
		}
	}
	store.pruneLocked(time.Now())
	return store, nil
}

func (s *importStatsStore) Record(event *notifier.Event) (bool, error) {
	return s.recordAt(event, time.Now())
}

func (s *importStatsStore) recordAt(event *notifier.Event, now time.Time) (bool, error) {
	key := importEventKey(event)
	if key == "" {
		return false, nil
	}

	day := importDayKey(now)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneLocked(now)
	if _, exists := s.seen[key]; exists {
		return false, nil
	}

	previousCount := s.days[day]
	s.days[day] = previousCount + 1
	s.seen[key] = day
	if err := s.saveLocked(); err != nil {
		if previousCount == 0 {
			delete(s.days, day)
		} else {
			s.days[day] = previousCount
		}
		delete(s.seen, key)
		return false, err
	}
	return true, nil
}

func (s *importStatsStore) Overview() importOverview {
	return s.overviewAt(time.Now())
}

func (s *importStatsStore) overviewAt(now time.Time) importOverview {
	dayStart := startOfImportDay(now)
	windowStart := dayStart.AddDate(0, 0, -(overviewWindowDays - 1))
	days := make([]dailyImportCount, 0, overviewWindowDays)
	overview := importOverview{Days: days}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for offset := 0; offset < overviewWindowDays; offset++ {
		day := windowStart.AddDate(0, 0, offset)
		key := importDayKey(day)
		count := s.days[key]
		overview.Days = append(overview.Days, dailyImportCount{
			Date:  key,
			Count: count,
		})
		overview.Last30DaysCount += count
		if offset >= overviewWindowDays-7 {
			overview.Last7DaysCount += count
		}
		if offset == overviewWindowDays-1 {
			overview.TodayCount = count
		}
		if count > overview.PeakCount {
			overview.PeakCount = count
			overview.PeakDate = key
		}
	}
	return overview
}

func (s *importStatsStore) pruneLocked(now time.Time) {
	dayStart := startOfImportDay(now)
	daysCutoff := importDayKey(dayStart.AddDate(0, 0, -(overviewWindowDays - 1)))
	seenCutoff := importDayKey(dayStart.AddDate(0, 0, -(importSeenRetentionDays - 1)))

	for day := range s.days {
		if _, ok := parseImportDay(day); !ok || day < daysCutoff {
			delete(s.days, day)
		}
	}
	for key, day := range s.seen {
		if _, ok := parseImportDay(day); !ok || day < seenCutoff {
			delete(s.seen, key)
		}
	}
}

func (s *importStatsStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(importStatsDisk{
		Version: importStatsFileVersion,
		Days:    s.days,
		Seen:    s.seen,
	}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	file, err := os.CreateTemp(filepath.Dir(s.path), ".import-stats-*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)

	if err := file.Chmod(0o644); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, s.path)
}

func importEventKey(event *notifier.Event) string {
	if event == nil || event.Event != "library.new" {
		return ""
	}

	itemID := strings.TrimSpace(event.ItemID)
	if itemID == "" {
		itemID = strings.TrimSpace(event.ItemPath)
	}
	if itemID == "" {
		return ""
	}
	return strings.TrimSpace(event.ServerName) + "\x00" + itemID
}

func importDayKey(value time.Time) string {
	return value.In(time.Local).Format("2006-01-02")
}

func startOfImportDay(value time.Time) time.Time {
	local := value.In(time.Local)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
}

func parseImportDay(value string) (time.Time, bool) {
	parsed, err := time.ParseInLocation("2006-01-02", value, time.Local)
	return parsed, err == nil
}
