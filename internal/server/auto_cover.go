package server

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"embytool/internal/config"
	"embytool/internal/emby"
	"embytool/internal/notifier"
)

const newImportCoverDelay = 5 * time.Minute

type autoCoverPending struct {
	scheduledAt time.Time
	dueAt       time.Time
	seq         uint64
}

type autoCoverSnapshot struct {
	libraryID   string
	scheduledAt time.Time
	dueAt       time.Time
}

type autoCoverScheduler struct {
	mu      sync.Mutex
	timers  map[string]*time.Timer
	pending map[string]autoCoverPending
	seq     map[string]uint64
	closed  bool
}

func newAutoCoverScheduler() *autoCoverScheduler {
	return &autoCoverScheduler{
		timers:  map[string]*time.Timer{},
		pending: map[string]autoCoverPending{},
		seq:     map[string]uint64{},
	}
}

func (s *autoCoverScheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for _, timer := range s.timers {
		timer.Stop()
	}
	s.timers = map[string]*time.Timer{}
	s.pending = map[string]autoCoverPending{}
}

func (s *autoCoverScheduler) Reconcile(enabled bool, selectedLibraries []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	selected := map[string]struct{}{}
	for _, libraryID := range selectedLibraries {
		libraryID = strings.TrimSpace(libraryID)
		if libraryID != "" {
			selected[libraryID] = struct{}{}
		}
	}

	for libraryID, timer := range s.timers {
		if enabled {
			if _, ok := selected[libraryID]; ok {
				continue
			}
		}
		timer.Stop()
		delete(s.timers, libraryID)
		delete(s.pending, libraryID)
	}
}

func (s *autoCoverScheduler) Pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.timers)
}

func (s *autoCoverScheduler) Snapshot() []autoCoverSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]autoCoverSnapshot, 0, len(s.pending))
	for libraryID, pending := range s.pending {
		out = append(out, autoCoverSnapshot{
			libraryID:   libraryID,
			scheduledAt: pending.scheduledAt,
			dueAt:       pending.dueAt,
		})
	}
	return out
}

func (s *autoCoverScheduler) Schedule(libraryID string, delay time.Duration, fire func()) bool {
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" || delay <= 0 || fire == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}

	s.seq[libraryID]++
	seq := s.seq[libraryID]
	now := time.Now()
	if timer, ok := s.timers[libraryID]; ok {
		timer.Stop()
	}
	s.pending[libraryID] = autoCoverPending{
		scheduledAt: now,
		dueAt:       now.Add(delay),
		seq:         seq,
	}
	s.timers[libraryID] = time.AfterFunc(delay, func() {
		s.mu.Lock()
		if s.closed || s.seq[libraryID] != seq {
			s.mu.Unlock()
			return
		}
		delete(s.timers, libraryID)
		delete(s.pending, libraryID)
		s.mu.Unlock()
		fire()
	})
	return true
}

func (s *Server) maybeScheduleAutoCover(event *notifier.Event, cfg config.Config) {
	if event == nil || !cfg.NewImportCoverEnabled {
		return
	}
	if cfg.EmbyServerURL == "" || cfg.EmbyAPIKey == "" {
		return
	}
	if event.Event != "library.new" {
		return
	}
	if event.ItemType != "MOV" && event.ItemType != "TV" {
		return
	}
	if len(cfg.SelectedLibraries) == 0 {
		return
	}

	resolveCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := emby.New(cfg.EmbyServerURL, cfg.EmbyAPIKey)
	libraryID := s.resolveLibraryIDForSelectedLibraries(resolveCtx, client, event, cfg.SelectedLibraries)
	if libraryID == "" {
		log.Printf("新入库封面联动：无法确定媒体库 item_id=%s path=%q", event.ItemID, event.ItemPath)
		return
	}

	delay := time.Duration(cfg.NewImportCoverWindow) * time.Second
	if delay <= 0 {
		delay = newImportCoverDelay
	}

	s.autoCover.Schedule(libraryID, delay, func() {
		s.generateAutoCoverForLibrary(libraryID)
	})
	log.Printf("新入库封面联动已排队: library_id=%s, window=%s", libraryID, delay)
}

func (s *Server) resolveLibraryIDForSelectedLibraries(ctx context.Context, client *emby.Client, event *notifier.Event, selected []string) string {
	selectedSet := map[string]struct{}{}
	for _, id := range selected {
		id = strings.TrimSpace(id)
		if id != "" {
			selectedSet[id] = struct{}{}
		}
	}
	if len(selectedSet) == 0 {
		return ""
	}

	libraries, err := client.GetLibrariesWithPaths(ctx)
	if err == nil {
		if libraryID := resolveLibraryIDByPath(ctx, client, event, libraries, selectedSet); libraryID != "" {
			return libraryID
		}
	}

	candidates := webhookLibraryCandidates(event)
	for _, candidate := range candidates {
		if libraryID := walkLibraryChain(ctx, client, candidate, selectedSet); libraryID != "" {
			return libraryID
		}
	}
	return ""
}

func resolveLibraryIDByPath(ctx context.Context, client *emby.Client, event *notifier.Event, libraries []map[string]any, selectedSet map[string]struct{}) string {
	itemPath := resolveWebhookItemPath(ctx, client, event)
	if itemPath == "" {
		return ""
	}

	bestID := ""
	bestName := ""
	bestSource := ""
	bestLength := -1
	for _, library := range libraries {
		libraryID := strings.TrimSpace(itemID(library))
		if _, ok := selectedSet[libraryID]; !ok {
			continue
		}
		for _, sourcePath := range librarySourcePaths(library) {
			if matchPathPrefix(itemPath, sourcePath) {
				matchLength := len(normalizeComparablePath(sourcePath))
				if matchLength > bestLength {
					bestLength = matchLength
					bestID = libraryID
					bestName = strings.TrimSpace(asString(library["Name"]))
					bestSource = sourcePath
				}
			}
		}
	}
	if bestID != "" {
		log.Printf("新入库封面联动：路径匹配媒体库 item_path=%q source=%q library=%s(%s)", itemPath, bestSource, bestName, bestID)
	}
	return bestID
}

func resolveWebhookItemPath(ctx context.Context, client *emby.Client, event *notifier.Event) string {
	if event == nil {
		return ""
	}
	if path := normalizeComparablePath(event.ItemPath); path != "" {
		return path
	}
	for _, candidate := range webhookLibraryCandidates(event) {
		item, _ := client.GetItem(ctx, candidate)
		if len(item) == 0 {
			continue
		}
		if path := normalizeComparablePath(asString(item["Path"])); path != "" {
			return path
		}
	}
	return ""
}

func walkLibraryChain(ctx context.Context, client *emby.Client, startID string, selectedSet map[string]struct{}) string {
	currentID := strings.TrimSpace(startID)
	for depth := 0; depth < 16 && currentID != ""; depth++ {
		if _, ok := selectedSet[currentID]; ok {
			return currentID
		}
		item, _ := client.GetItem(ctx, currentID)
		if len(item) == 0 {
			return ""
		}
		currentID = strings.TrimSpace(asString(item["ParentId"]))
	}
	return ""
}

func librarySourcePaths(library map[string]any) []string {
	for _, key := range []string{"Locations", "Paths"} {
		if values := stringSlice(library[key]); len(values) > 0 {
			return normalizePathList(values)
		}
	}
	if value := normalizeComparablePath(asString(library["Path"])); value != "" {
		return []string{value}
	}
	return nil
}

func normalizePathList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if normalized := normalizeComparablePath(value); normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}

func normalizeComparablePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = filepath.Clean(value)
	value = strings.ReplaceAll(value, `\`, `/`)
	value = strings.TrimRight(value, "/")
	if value == "." {
		return ""
	}
	return strings.ToLower(value)
}

func matchPathPrefix(itemPath, sourcePath string) bool {
	itemPath = normalizeComparablePath(itemPath)
	sourcePath = normalizeComparablePath(sourcePath)
	if itemPath == "" || sourcePath == "" {
		return false
	}
	if itemPath == sourcePath {
		return true
	}
	return strings.HasPrefix(itemPath, sourcePath+"/")
}

func stringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if value := strings.TrimSpace(asString(item)); value != "" {
				out = append(out, value)
			}
		}
		return out
	default:
		return nil
	}
}

func webhookLibraryCandidates(event *notifier.Event) []string {
	if event == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}

	add(event.ItemID)
	add(asString(event.JSONObject["LibraryId"]))
	add(asString(event.JSONObject["ParentId"]))
	add(asString(event.JSONObject["ParentLibraryItemId"]))
	if item, ok := event.JSONObject["Item"].(map[string]any); ok {
		add(asString(item["Id"]))
		add(asString(item["LibraryId"]))
		add(asString(item["ParentId"]))
		add(asString(item["ParentLibraryItemId"]))
	}
	return out
}

func (s *Server) generateAutoCoverForLibrary(libraryID string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("新入库联动生成封面时发生 panic: %v", r)
		}
	}()

	cfg := config.Load()
	if !cfg.NewImportCoverEnabled {
		return
	}
	if !containsString(cfg.SelectedLibraries, libraryID) {
		return
	}
	if cfg.EmbyServerURL == "" || cfg.EmbyAPIKey == "" {
		return
	}

	client := emby.New(cfg.EmbyServerURL, cfg.EmbyAPIKey)
	libraries, err := client.GetLibraries(context.Background())
	if err != nil {
		log.Printf("新入库联动：获取媒体库失败: %v", err)
		return
	}
	selected := filterLibrariesByIDs(libraries, []string{libraryID})
	if len(selected) == 0 {
		log.Printf("新入库联动：未找到媒体库 %s", libraryID)
		return
	}

	result := s.generateLibraryCover(context.Background(), client, selected[0], cfg)
	log.Printf("新入库联动 [%s] %s", asString(selected[0]["Name"]), result.Message)
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}
